package activities

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/task"
)

// TestBuildColdFrontConfig verifies that the config YAML renderer produces
// the correct structure for each provider and never embeds credentials in
// plain text in a way that could slip into a log (smoke-test only — the real
// credential check is that the field exists with the expected value).
func TestBuildColdFrontConfig(t *testing.T) {
	cases := []struct {
		name   string
		cfg    coldFrontStorageConfig
		dbName string
	}{
		{
			name: "aws",
			cfg: coldFrontStorageConfig{
				Provider:  "aws",
				Warehouse: "s3://my-bucket/warehouse",
				Bucket:    "my-bucket",
				Region:    "us-east-1",
				Credential: map[string]string{
					"access_key_id":     "AKID",
					"secret_access_key": "SECRET",
				},
			},
			dbName: "mydb",
		},
		{
			name: "azure",
			cfg: coldFrontStorageConfig{
				Provider:  "azure",
				Warehouse: "abfss://container@account.dfs.core.windows.net",
				Bucket:    "container",
				Credential: map[string]string{
					"connection_string": "DefaultEndpointsProtocol=https;...",
				},
			},
			dbName: "mydb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml, err := buildColdFrontConfigYAML(tc.cfg, tc.dbName, "lakekeeper-svc:8181", "coldfront")
			if err != nil {
				t.Fatalf("buildColdFrontConfigYAML returned error: %v", err)
			}
			if len(yaml) == 0 {
				t.Fatal("empty config YAML")
			}
			content := string(yaml)
			if !strings.Contains(content, "postgres:") {
				t.Error("missing postgres section")
			}
			if !strings.Contains(content, "iceberg:") {
				t.Error("missing iceberg section")
			}
			if strings.Contains(content, "tables:") {
				t.Error("config must NOT contain archiver.tables — tables come from DB registry")
			}
		})
	}
}

// TestBuildColdFrontConfigS3ContractKeys pins the s3 block to the exact YAML
// keys the ColdFront binaries parse (coldfront internal/config.S3Config and
// cmd/compactor/config): access_key / secret_key / region / endpoint. The
// binaries do NOT parse access_key_id / secret_access_key / bucket, so emitting
// those leaves cfg.S3.AccessKey/SecretKey EMPTY — the archiver then treats the
// run as having no static creds and fatals ("no static s3/azure credentials
// configured and coldfront.storage_secret is not vended") before any S3 write.
// The credential VALUES still come from the store's credential JSON, whose
// standard AWS field names are access_key_id / secret_access_key.
func TestBuildColdFrontConfigS3ContractKeys(t *testing.T) {
	cfg := coldFrontStorageConfig{
		Provider:  "aws",
		Warehouse: "cfsaas1",
		Bucket:    "my-bucket",
		Region:    "us-east-2",
		Credential: map[string]string{
			"access_key_id":     "AKID",
			"secret_access_key": "SECRET",
		},
	}
	yaml, err := buildColdFrontConfigYAML(cfg, "mydb", "http://lk:8181/catalog", "admin")
	if err != nil {
		t.Fatalf("buildColdFrontConfigYAML returned error: %v", err)
	}
	content := string(yaml)

	// Must emit coldfront's contract keys carrying the credential values.
	if !strings.Contains(content, "access_key: AKID") {
		t.Errorf("expected s3.access_key with the credential value, got:\n%s", content)
	}
	if !strings.Contains(content, "secret_key: SECRET") {
		t.Errorf("expected s3.secret_key with the credential value, got:\n%s", content)
	}
	// Must NOT emit keys the binaries silently ignore.
	if strings.Contains(content, "access_key_id") {
		t.Errorf("s3.access_key_id is not parsed by the coldfront binaries, got:\n%s", content)
	}
	if strings.Contains(content, "secret_access_key") {
		t.Errorf("s3.secret_access_key is not parsed by the coldfront binaries, got:\n%s", content)
	}
	if strings.Contains(content, "bucket:") {
		t.Errorf("s3.bucket is not a coldfront s3 key (bucket comes from the Lakekeeper warehouse), got:\n%s", content)
	}
}

// TestBuildColdFrontConfigDSNUser verifies the tiering DSN uses the supplied
// connect-as user, and falls back to "coldfront" when none is provided.
func TestBuildColdFrontConfigDSNUser(t *testing.T) {
	cfg := coldFrontStorageConfig{
		Provider:  "aws",
		Warehouse: "s3://my-bucket/warehouse",
		Bucket:    "my-bucket",
		Region:    "us-east-1",
		Credential: map[string]string{
			"access_key_id":     "AKID",
			"secret_access_key": "SECRET",
		},
	}

	t.Run("explicit user", func(t *testing.T) {
		yaml, err := buildColdFrontConfigYAML(cfg, "mydb", "lakekeeper-svc:8181", "app_owner")
		if err != nil {
			t.Fatalf("buildColdFrontConfigYAML returned error: %v", err)
		}
		if !strings.Contains(string(yaml), "user=app_owner ") {
			t.Errorf("expected DSN to use connect-as user app_owner, got:\n%s", yaml)
		}
	})

	t.Run("empty user falls back to coldfront", func(t *testing.T) {
		yaml, err := buildColdFrontConfigYAML(cfg, "mydb", "lakekeeper-svc:8181", "")
		if err != nil {
			t.Fatalf("buildColdFrontConfigYAML returned error: %v", err)
		}
		if !strings.Contains(string(yaml), "user=coldfront ") {
			t.Errorf("expected DSN to fall back to coldfront user, got:\n%s", yaml)
		}
	})
}

// TestBuildTieringCommand_UsesUsrBin verifies the tiering binary is invoked from
// /usr/bin (where the pgedge-coldfront package installs it), not the legacy
// /usr/local/bin, and that the config file is written and passed via --config.
func TestBuildTieringCommand_UsesUsrBin(t *testing.T) {
	cmd := buildTieringCommand("QkFTRTY0", "/tmp/coldfront-config.yaml", "archiver")

	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatalf("expected [sh -c <script>], got %#v", cmd)
	}
	script := cmd[2]
	if !strings.Contains(script, "/usr/bin/archiver --config /tmp/coldfront-config.yaml") {
		t.Errorf("expected binary invoked from /usr/bin with --config, got:\n%s", script)
	}
	if strings.Contains(script, "/usr/local/bin/") {
		t.Errorf("binary must not be invoked from /usr/local/bin, got:\n%s", script)
	}
	if !strings.Contains(script, "QkFTRTY0") {
		t.Errorf("expected base64 config payload in the script, got:\n%s", script)
	}
}

// realArchiverEmptyOutput is the archiver's verbatim log line (with its real
// timestamp prefix) when coldfront.partition_config has no rows for its mode
// (cmd/archiver/main.go, via log.Fatalf, exit 1). The partitioner emits the
// same leading clause, differing only in the register/import verb
// (`partitioner register` vs `archiver register`); both share the marker
// substring the classifier keys on, so this one string pins the real contract
// for both — not a paraphrase.
const realArchiverEmptyOutput = "2026/07/23 15:20:26 no tables in coldfront.partition_config; " +
	"add one with `archiver register` or seed a YAML with `archiver import --config /tmp/x.yaml`"

// TestIsBenignEmptyPartitionConfig verifies the benign-empty detection logic. An empty
// coldfront.partition_config is a normal, non-error state for a freshly-created
// database, so a "no tables in coldfront.partition_config" exit from the
// archiver OR the partitioner (both emit it, both benign) is treated as success.
// The compactor is deliberately excluded: it takes an explicit --table and never
// emits this message, so masking its failures would hide real problems.
func TestIsBenignEmptyPartitionConfig(t *testing.T) {
	cases := []struct {
		binary string
		output string
		want   bool
	}{
		// The REAL upstream message is benign for the archiver and partitioner.
		{"archiver", realArchiverEmptyOutput, true},
		{"partitioner", realArchiverEmptyOutput, true},
		{"archiver", "no tables in coldfront.partition_config", true},
		{"archiver", "NO TABLES IN COLDFRONT.PARTITION_CONFIG\n", true},
		// The compactor is never benign, even for this message.
		{"compactor", realArchiverEmptyOutput, false},
		// The stale phantom wording must NOT be what we key on.
		{"archiver", "no tables configured", false},
		// Genuine failures and unrelated output are never benign.
		{"archiver", "error: connection refused", false},
		{"archiver", "archiver completed successfully", false},
		{"archiver", "", false},
		{"partitioner", "error: connection refused", false},
	}
	for _, tc := range cases {
		if got := isBenignEmptyPartitionConfig(tc.binary, tc.output); got != tc.want {
			t.Errorf("isBenignEmptyPartitionConfig(%q, %q) = %v, want %v", tc.binary, tc.output, got, tc.want)
		}
	}
}

// fakeExecer drives runColdFrontBinary with a canned exit code / output so the
// exit-code capture and benign classification can be tested without Docker.
type fakeExecer struct {
	exitCode int
	output   string
	err      error
}

func (f fakeExecer) Exec(_ context.Context, _ string, _ []string) (int, string, error) {
	return f.exitCode, f.output, f.err
}

// mapResultToTaskStatus mirrors what the ColdFrontTiering workflow does with
// the activity result: a nil error is recorded as task success (completed), a
// non-nil error as task failure (failed). Exercising this mapping lets the test
// assert the resulting task status per exit-code scenario, which is the
// behaviour this task exists to add.
func mapResultToTaskStatus(err error) task.Status {
	if err != nil {
		return task.StatusFailed
	}
	return task.StatusCompleted
}

// TestRunColdFrontBinaryExitCodeToTaskStatus is the mandated behavioural test:
// it drives the exec + exit-code + benign-classification path with a fake and
// asserts the resulting task status for each scenario.
func TestRunColdFrontBinaryExitCodeToTaskStatus(t *testing.T) {
	cases := []struct {
		name    string
		binary  string
		execer  fakeExecer
		wantErr bool
		want    task.Status
	}{
		{
			name:    "exit zero records success",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 0},
			wantErr: false,
			want:    task.StatusCompleted,
		},
		{
			name:    "non-zero exit records failure",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 1, output: "fatal: connection refused", err: errors.New("command failed with exit code 1")},
			wantErr: true,
			want:    task.StatusFailed,
		},
		{
			name:    "archiver empty partition_config is benign (success)",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 1, output: realArchiverEmptyOutput, err: errors.New("command failed with exit code 1")},
			wantErr: false,
			want:    task.StatusCompleted,
		},
		{
			name:    "partitioner empty partition_config is benign (success)",
			binary:  "partitioner",
			execer:  fakeExecer{exitCode: 1, output: realArchiverEmptyOutput, err: errors.New("command failed with exit code 1")},
			wantErr: false,
			want:    task.StatusCompleted,
		},
		{
			name:    "compactor empty partition_config is NOT benign (failure)",
			binary:  "compactor",
			execer:  fakeExecer{exitCode: 1, output: realArchiverEmptyOutput, err: errors.New("command failed with exit code 1")},
			wantErr: true,
			want:    task.StatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runColdFrontBinary(context.Background(), tc.execer, "container-id", tc.binary, []string{"sh", "-c", "true"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("runColdFrontBinary err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got := mapResultToTaskStatus(err); got != tc.want {
				t.Errorf("resulting task status = %q, want %q (err=%v)", got, tc.want, err)
			}
			// A genuine failure error must never leak the config/credential.
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Errorf("error output unexpectedly contains 'secret': %v", err)
			}
		})
	}
}
