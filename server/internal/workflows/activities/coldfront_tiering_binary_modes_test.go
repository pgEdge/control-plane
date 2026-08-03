package activities

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func modesStorageConfig() coldFrontStorageConfig {
	return coldFrontStorageConfig{
		Provider:  "aws",
		Warehouse: "wh",
		Bucket:    "bkt",
		Region:    "us-east-2",
		Credential: map[string]string{
			"access_key_id":     "AKID",
			"secret_access_key": "SECRET",
		},
	}
}

// The scheduler passes the binary name as a bare literal
// (scheduler/scheduled_job_executor.go), and the cold-tier mode switch is an
// exact string match on it. Renaming a constant's VALUE without touching the
// scheduler would silently hand the partitioner a tiered config again — a loud
// failure would only follow if the binary PATH also broke, which it would not.
func TestColdFrontBinaryConstantsMatchSchedulerLiterals(t *testing.T) {
	assert.Equal(t, "archiver", coldFrontArchiverBinary)
	assert.Equal(t, "partitioner", coldFrontPartitionerBinary)
	assert.Equal(t, "compactor", coldFrontCompactorBinary)
}

// The partitioner is the no-Iceberg tool: it filters coldfront.partition_config
// to PartitionOnly rows, which by definition carry no hot_period. ColdFront
// decides tiered-vs-partition-only at CONFIG level, so any iceberg/s3 block in
// the YAML makes it validate every loaded table in tiered mode and reject
// partition-only tables with "hot_period is required in tiered mode".
func TestBuildColdFrontConfigOmitsColdTierForPartitioner(t *testing.T) {
	// The literal, not the constant: this must keep working for the value the
	// scheduler actually passes.
	out, err := buildColdFrontConfigYAML(
		modesStorageConfig(), "mydb", "http://svc:8181/catalog", "app_owner",
		"partitioner",
	)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, yaml.Unmarshal(out, &m))

	assert.NotContains(t, m, "iceberg",
		"partitioner config must not carry an iceberg block")
	assert.NotContains(t, m, "s3",
		"partitioner config must not carry an s3 block")
	assert.NotContains(t, m, "azure",
		"partitioner config must not carry an azure block")
	require.Contains(t, m, "postgres", "partitioner still needs its DSN")
}

func TestBuildColdFrontConfigKeepsColdTierForTieringBinaries(t *testing.T) {
	for _, binary := range []string{coldFrontArchiverBinary, coldFrontCompactorBinary} {
		t.Run(binary, func(t *testing.T) {
			out, err := buildColdFrontConfigYAML(
				modesStorageConfig(), "mydb", "http://svc:8181/catalog", "app_owner", binary,
			)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, yaml.Unmarshal(out, &m))
			assert.Contains(t, m, "iceberg", "%s needs the iceberg block", binary)
			assert.Contains(t, m, "s3", "%s needs the s3 block", binary)
			assert.Contains(t, m, "postgres")
		})
	}
}

// The three tiering jobs run as independent activities on the same host and
// their default crons collide (archiver hourly and partitioner 6-hourly both
// fire on the hour; archiver and compactor both at 02:00). Since F7 the
// per-binary configs DIFFER, so a shared path lets one binary read another's
// config — reinstating F7 intermittently — or read a half-written file.
func TestColdFrontConfigPathIsUniquePerRun(t *testing.T) {
	a1 := coldFrontConfigPath(coldFrontArchiverBinary)
	a2 := coldFrontConfigPath(coldFrontArchiverBinary)
	p1 := coldFrontConfigPath(coldFrontPartitionerBinary)

	assert.NotEqual(t, a1, a2, "two runs of the same binary must not share a path")
	assert.NotEqual(t, a1, p1, "two binaries must not share a path")
	for _, p := range []string{a1, a2, p1} {
		assert.True(t, strings.HasPrefix(p, "/tmp/coldfront-"), "unexpected path %q", p)
		assert.True(t, strings.HasSuffix(p, ".yaml"), "unexpected path %q", p)
	}
}

// The binary run needs no shell: dockerTieringExecer passes argv straight to
// docker exec. Keeping the table name out of a shell string is what makes an
// arbitrary PG identifier safe to compact.
func TestBuildCompactorArgvUsesNoShell(t *testing.T) {
	argv := buildCompactorArgv("/tmp/cf.yaml", "my-Table$weird")
	assert.Equal(t, []string{
		coldFrontBinDir + "/" + coldFrontCompactorBinary,
		"--config", "/tmp/cf.yaml",
		"--table", "my-Table$weird",
	}, argv)
	for _, arg := range argv {
		assert.NotEqual(t, "sh", arg, "compactor run must not go through a shell")
	}
}

func TestBuildTieringArgvUsesNoShell(t *testing.T) {
	argv := buildTieringArgv("/tmp/cf.yaml", coldFrontArchiverBinary)
	assert.Equal(t, []string{
		coldFrontBinDir + "/" + coldFrontArchiverBinary,
		"--config", "/tmp/cf.yaml",
	}, argv)
}

// The enumeration query is the whole basis for which tables get compacted, so
// its source table and connection are worth pinning explicitly.
func TestBuildTieredTableListCommand(t *testing.T) {
	cmd := buildTieredTableListCommand("mydb", "app_owner")
	joined := strings.Join(cmd, " ")

	assert.Equal(t, "psql", cmd[0])
	// tiered_views, not partition_config: only tables with an Iceberg table have
	// anything to compact. A table registered but never archived is absent from
	// tiered_views, and asking the catalog for it would fail the daily job.
	assert.Contains(t, joined, "coldfront.tiered_views")
	assert.NotContains(t, joined, "partition_config")
	// Distinct: tiered_views is keyed (schema_name, relname) but the Iceberg name
	// flattens to the relname, so two schemas would otherwise compact twice.
	assert.Contains(t, joined, "DISTINCT")
	assert.Contains(t, joined, "dbname=mydb")
	assert.Contains(t, joined, "user=app_owner")
	assert.Contains(t, joined, "-tAX")
	// docker exec attaches a TTY, so psql's stdout is a terminal and its pager is
	// on by default; with stdin attached and never fed, a paged list would block.
	assert.Contains(t, joined, "pager=off")
}

func TestBuildTieredTableListCommandFallsBackToColdfrontUser(t *testing.T) {
	cmd := buildTieredTableListCommand("mydb", "")
	assert.Contains(t, strings.Join(cmd, " "), "user=coldfront")
}

func TestParseTieredTableList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"whitespace only", "\n  \n", nil},
		{"single", "events\n", []string{"events"}},
		{"several", "events\norders\nreadings\n", []string{"events", "orders", "readings"}},
		{"trims padding and CR", "  events  \n\torders\t\nreadings\r\n",
			[]string{"events", "orders", "readings"}},
		// No allowlist: the name goes into argv, never a shell, so any identifier
		// PostgreSQL accepts is compactable.
		{"keeps quoted-identifier names", "my-table\nMixedCase\n",
			[]string{"my-table", "MixedCase"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseTieredTableList(tc.output))
		})
	}
}

// recordingExecer records every command and replays scripted responses in order.
// A non-zero exit is paired with a non-nil error to match dockerTieringExecer,
// which always reports both.
type recordingExecer struct {
	calls     [][]string
	responses []execResponse
}

type execResponse struct {
	exitCode int
	output   string
	err      error
}

func (r *recordingExecer) RunInContainer(_ context.Context, _ string, cmd []string) (int, string, error) {
	r.calls = append(r.calls, cmd)
	if len(r.calls) <= len(r.responses) {
		resp := r.responses[len(r.calls)-1]
		return resp.exitCode, resp.output, resp.err
	}
	return 0, "", nil
}

func TestRunColdFrontCompactorWritesConfigOnceAndRunsPerTable(t *testing.T) {
	execer := &recordingExecer{responses: []execResponse{
		{},                           // config write
		{output: "events\norders\n"}, // enumeration
	}}

	err := runColdFrontCompactor(
		context.Background(), execer, "container-1",
		"BASE64", "/tmp/cf.yaml", "mydb", "app_owner",
	)
	require.NoError(t, err)

	// write + enumerate + one run per table + cleanup.
	require.Len(t, execer.calls, 5)
	assert.Contains(t, strings.Join(execer.calls[0], " "), "base64 -d",
		"first call must write the config")
	assert.Equal(t, "psql", execer.calls[1][0])
	assert.Equal(t, []string{
		coldFrontBinDir + "/" + coldFrontCompactorBinary,
		"--config", "/tmp/cf.yaml", "--table", "events",
	}, execer.calls[2])
	assert.Equal(t, []string{
		coldFrontBinDir + "/" + coldFrontCompactorBinary,
		"--config", "/tmp/cf.yaml", "--table", "orders",
	}, execer.calls[3])
	assert.Contains(t, strings.Join(execer.calls[4], " "), "rm -f",
		"the config carries live credentials and must be removed")
}

// A database with no Iceberg-backed tables has nothing to compact. That is the
// normal state of a freshly created ColdFront database and must not fail the
// daily job, mirroring the archiver/partitioner empty-config handling.
func TestRunColdFrontCompactorNoTablesIsBenign(t *testing.T) {
	execer := &recordingExecer{responses: []execResponse{
		{},             // config write
		{output: "\n"}, // enumeration: nothing
	}}

	err := runColdFrontCompactor(
		context.Background(), execer, "container-1",
		"BASE64", "/tmp/cf.yaml", "mydb", "app_owner",
	)
	require.NoError(t, err)
	for _, call := range execer.calls {
		assert.NotContains(t, strings.Join(call, " "), "--table",
			"no compactor run when there are no tables")
	}
}

// One unusable table must not block compaction of every table sorted after it,
// which would otherwise be permanent and silent for the rest of the set.
func TestRunColdFrontCompactorContinuesAfterPerTableFailure(t *testing.T) {
	execer := &recordingExecer{responses: []execResponse{
		{},                          // config write
		{output: "aaa\nbbb\nccc\n"}, // enumeration
		{exitCode: 1, output: "NoSuchTable", // aaa fails
			err: errors.New("command failed with exit code 1")},
	}}

	err := runColdFrontCompactor(
		context.Background(), execer, "container-1",
		"BASE64", "/tmp/cf.yaml", "mydb", "app_owner",
	)
	require.Error(t, err, "the task must still fail so the alert survives")
	assert.Contains(t, err.Error(), "aaa")

	var ran []string
	for _, call := range execer.calls {
		for i, arg := range call {
			if arg == "--table" && i+1 < len(call) {
				ran = append(ran, call[i+1])
			}
		}
	}
	assert.Equal(t, []string{"aaa", "bbb", "ccc"}, ran,
		"every table must be attempted despite an earlier failure")
}

func TestRunColdFrontCompactorSurfacesEnumerationFailure(t *testing.T) {
	execer := &recordingExecer{responses: []execResponse{
		{}, // config write
		{exitCode: 1, output: `FATAL: database "mydb" does not exist`,
			err: errors.New("command failed with exit code 1")},
	}}

	err := runColdFrontCompactor(
		context.Background(), execer, "container-1",
		"BASE64", "/tmp/cf.yaml", "mydb", "app_owner",
	)
	require.Error(t, err)
	for _, call := range execer.calls {
		assert.NotContains(t, strings.Join(call, " "), "--table",
			"must not run the compactor if enumeration failed")
	}
}

func TestRunColdFrontCompactorSurfacesConfigWriteFailure(t *testing.T) {
	execer := &recordingExecer{responses: []execResponse{
		{exitCode: 1, output: "no space left on device",
			err: errors.New("command failed with exit code 1")},
	}}

	err := runColdFrontCompactor(
		context.Background(), execer, "container-1",
		"BASE64", "/tmp/cf.yaml", "mydb", "app_owner",
	)
	require.Error(t, err)
	assert.Len(t, execer.calls, 1, "must not enumerate if the config never landed")
}
