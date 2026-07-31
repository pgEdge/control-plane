package activities

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/google/uuid"
	"github.com/samber/do"
	"gopkg.in/yaml.v3"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// coldFrontArchiverBinary and coldFrontPartitionerBinary are the two binaries
// whose empty-partition_config exit is benign (see runColdFrontBinary). Both
// read coldfront.partition_config (filtered by mode) and both log.Fatalf the
// same "no tables in coldfront.partition_config" message when it is empty.
const (
	coldFrontArchiverBinary    = "archiver"
	coldFrontPartitionerBinary = "partitioner"
	coldFrontCompactorBinary   = "compactor"
)

// coldFrontConfigPath returns the in-container path for one activity run's
// config. It is unique per run, not merely per binary: the three tiering jobs
// are independent activities on the same host whose default crons collide (the
// hourly archiver and 6-hourly partitioner both fire on the hour, the archiver
// and compactor both at 02:00), and since the partitioner's config deliberately
// omits the cold-tier blocks the per-binary configs now DIFFER. A shared path
// would let one binary read another's config — silently reinstating the
// partitioner's tiered-mode rejection — or read a half-written file. A run can
// also overlap the next fire of its own schedule, so per-binary alone is not
// enough.
func coldFrontConfigPath(binary string) string {
	return fmt.Sprintf("/tmp/coldfront-%s-%s.yaml", binary, uuid.NewString())
}

// coldFrontEmptyPartitionConfigMarker is the substring the archiver and the
// partitioner emit (via log.Fatalf, exit 1) when coldfront.partition_config has
// no rows for their mode. Matched lower-cased. It MUST track the upstream
// wording in cmd/{archiver,partitioner}/main.go.
const coldFrontEmptyPartitionConfigMarker = "no tables in coldfront.partition_config"

// coldFrontBinDir is where the tiering binaries (archiver/partitioner/compactor)
// live in the data-node image. The pgedge-coldfront package installs them to
// /usr/bin (RPM %{_bindir}), matching CP's other externally-installed binaries
// (pgbackrest/patroni default to /usr/bin too).
const coldFrontBinDir = "/usr/bin"

// buildConfigWriteCommand writes the base64-encoded config to configPath inside
// the container. This is the only step that needs a shell, for the decode
// pipeline; base64 keeps the config itself out of the shell string entirely.
func buildConfigWriteCommand(encodedConfig, configPath string) []string {
	return []string{
		"sh", "-c",
		fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", encodedConfig, configPath),
	}
}

// buildConfigRemoveCommand deletes the rendered config, which holds live
// object-store credentials.
func buildConfigRemoveCommand(configPath string) []string {
	return []string{"rm", "-f", configPath}
}

// buildTieringArgv builds the argv for a single-pass binary run. No shell: the
// execer hands argv straight to docker exec, so nothing here needs quoting.
func buildTieringArgv(configPath, binary string) []string {
	return []string{coldFrontBinDir + "/" + binary, "--config", configPath}
}

// buildCompactorArgv builds the argv for one compactor run. The compactor is
// per-table: it requires --table alongside --config and exits 2 with a usage
// message without it, so there is no config-only invocation that can succeed.
// Passing the table as its own argv element rather than through a shell is what
// makes any identifier PostgreSQL accepts safe to compact.
func buildCompactorArgv(configPath, table string) []string {
	return []string{
		coldFrontBinDir + "/" + coldFrontCompactorBinary,
		"--config", configPath,
		"--table", table,
	}
}

// buildTieredTableListCommand builds the argv that lists the Iceberg-backed
// tables to compact. coldfront.tiered_views is the right source rather than
// coldfront.partition_config: a row appears in tiered_views only once a table
// actually has an Iceberg table (at first cutover, or via
// coldfront.create_iceberg_table). A table registered in partition_config but
// not yet archived has no Iceberg table at all, and asking the catalog for it
// would fail the daily job.
//
// DISTINCT because tiered_views is keyed (schema_name, relname) while the
// Iceberg name flattens to the relname, so two same-named tables in different
// schemas would otherwise be compacted twice.
//
// pager=off because docker exec attaches a TTY, which makes psql's stdout a
// terminal and turns its pager on; with stdin attached and never fed, a paged
// list would block until the activity context expired.
func buildTieredTableListCommand(dbName, dsnUser string) []string {
	if dsnUser == "" {
		dsnUser = "coldfront"
	}
	const query = "SELECT DISTINCT relname FROM coldfront.tiered_views ORDER BY relname"
	return []string{
		"psql", "-tAX", "-P", "pager=off", "-v", "ON_ERROR_STOP=1",
		"-d", fmt.Sprintf("host=localhost port=5432 user=%s dbname=%s sslmode=disable", dsnUser, dbName),
		"-c", query,
	}
}

// parseTieredTableList extracts table names from the enumeration query output.
func parseTieredTableList(output string) []string {
	var tables []string
	for _, line := range strings.Split(output, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			tables = append(tables, name)
		}
	}
	return tables
}

// runColdFrontCompactor writes the config once, enumerates the Iceberg-backed
// tables, and runs the compactor once per table. A database with no such tables
// is the normal state of a freshly created ColdFront database, so an empty list
// is success and no compactor runs at all — mirroring how an empty
// partition_config is benign for the archiver and partitioner.
//
// Per-table failures are collected rather than aborting the pass: one unusable
// table would otherwise permanently block compaction of every table after it,
// with no partial progress. The joined error still fails the task.
func runColdFrontCompactor(
	ctx context.Context,
	execer tieringExecer,
	containerID, encodedConfig, configPath, dbName, dsnUser string,
) error {
	if err := runColdFrontStep(
		ctx, execer, containerID, "write config",
		buildConfigWriteCommand(encodedConfig, configPath),
	); err != nil {
		return err
	}
	defer removeColdFrontConfig(ctx, execer, containerID, configPath)

	output, err := runColdFrontStepOutput(
		ctx, execer, containerID, "list tiered tables",
		buildTieredTableListCommand(dbName, dsnUser),
	)
	if err != nil {
		return err
	}

	var errs []error
	for _, table := range parseTieredTableList(output) {
		if runErr := runColdFrontBinary(
			ctx, execer, containerID, coldFrontCompactorBinary,
			buildCompactorArgv(configPath, table),
		); runErr != nil {
			errs = append(errs, fmt.Errorf("table %s: %w", table, runErr))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("coldfront %s: %w",
			coldFrontCompactorBinary, errors.Join(errs...))
	}
	return nil
}

// runColdFrontStep runs a supporting command and discards its output.
func runColdFrontStep(
	ctx context.Context, execer tieringExecer, containerID, step string, cmd []string,
) error {
	_, err := runColdFrontStepOutput(ctx, execer, containerID, step, cmd)
	return err
}

// runColdFrontStepOutput runs a supporting command and returns its output. These
// are the compactor's scaffolding steps, not a tiering binary, so the
// benign-empty classification does not apply.
func runColdFrontStepOutput(
	ctx context.Context, execer tieringExecer, containerID, step string, cmd []string,
) (string, error) {
	exitCode, output, err := execer.Exec(ctx, containerID, cmd)
	if err != nil {
		return output, fmt.Errorf("coldfront %s: failed to %s: %w\noutput:\n%s",
			coldFrontCompactorBinary, step, err, output)
	}
	if exitCode != 0 {
		return output, fmt.Errorf("coldfront %s: failed to %s (exit %d)\noutput:\n%s",
			coldFrontCompactorBinary, step, exitCode, output)
	}
	return output, nil
}

// removeColdFrontConfig deletes the rendered config on a best-effort basis; a
// failure to clean up must not fail an otherwise successful pass.
func removeColdFrontConfig(
	ctx context.Context, execer tieringExecer, containerID, configPath string,
) {
	_, _, _ = execer.Exec(ctx, containerID, buildConfigRemoveCommand(configPath))
}

// coldFrontStorageConfig holds the parsed object-store coordinates extracted
// from a lakekeeper ServiceSpec.Config. The Credential field MUST NOT be logged.
type coldFrontStorageConfig struct {
	Provider   string
	Warehouse  string
	Bucket     string
	Region     string
	Endpoint   string
	PathPrefix string
	Credential map[string]string
}

// parseColdFrontStorageConfig extracts storage config from a lakekeeper
// ServiceSpec.Config map. Returns nil (no error) if the provider key is
// absent — callers treat that as "no storage configured yet".
func parseColdFrontStorageConfig(config map[string]any) (*coldFrontStorageConfig, error) {
	get := func(key string) string {
		v, _ := config[key].(string)
		return strings.TrimSpace(v)
	}

	provider := get("provider")
	if provider == "" {
		return nil, nil
	}
	switch provider {
	case "aws", "azure", "gcs":
	default:
		return nil, fmt.Errorf("coldfront: unsupported provider %q", provider)
	}

	credRaw := get("credential")
	var cred map[string]string
	if credRaw != "" {
		if err := json.Unmarshal([]byte(credRaw), &cred); err != nil {
			return nil, fmt.Errorf("coldfront: credential is not valid JSON")
		}
	}

	return &coldFrontStorageConfig{
		Provider:   provider,
		Warehouse:  get("warehouse"),
		Bucket:     get("bucket"),
		Region:     get("region"),
		Endpoint:   get("endpoint"),
		PathPrefix: get("path_prefix"),
		Credential: cred,
	}, nil
}

// buildColdFrontConfigYAML renders the YAML configuration for the archiver,
// partitioner, or compactor binary. The table list is intentionally omitted —
// the binaries resolve which tables to process from the DB registry
// (coldfront.partition_config). Credentials are written to the YAML but the
// caller must ensure the file is ephemeral and the content is never logged.
//
// dsnUser is the connect-as user the binary should authenticate as against the
// node's local Postgres; it falls back to "coldfront" when empty so the DSN is
// always well-formed.
func buildColdFrontConfigYAML(
	cfg coldFrontStorageConfig,
	dbName, lakekeeperEndpoint, dsnUser, binary string,
) ([]byte, error) {
	if dsnUser == "" {
		dsnUser = "coldfront"
	}
	m := map[string]any{
		"postgres": map[string]any{
			"dsn": fmt.Sprintf("host=localhost port=5432 user=%s dbname=%s sslmode=disable", dsnUser, dbName),
		},
	}

	// ColdFront selects tiered-vs-partition-only mode from the CONFIG, not per
	// table: any iceberg/s3 block makes it validate every table loaded from
	// coldfront.partition_config in tiered mode and demand a hot_period. The
	// partitioner is the no-Iceberg tool and only ever loads PartitionOnly rows,
	// which have no hot_period, so handing it a cold-tier config makes it reject
	// exactly the tables it exists to process.
	if binary == coldFrontPartitionerBinary {
		return yaml.Marshal(m)
	}

	m["iceberg"] = map[string]any{
		"warehouse":           cfg.Warehouse,
		"lakekeeper_endpoint": lakekeeperEndpoint,
		"namespace":           "default",
	}

	switch cfg.Provider {
	case "aws", "gcs":
		keyID := cfg.Credential["access_key_id"]
		secret := cfg.Credential["secret_access_key"]
		if cfg.Provider == "gcs" {
			keyID = cfg.Credential["hmac_access_id"]
			secret = cfg.Credential["hmac_secret"]
		}
		// Emit exactly the keys the ColdFront binaries parse (coldfront
		// internal/config.S3Config + cmd/compactor/config): access_key /
		// secret_key / region / endpoint. access_key_id / secret_access_key /
		// bucket are NOT parsed — the credential VALUES carry AWS's standard
		// field names, but the YAML keys must be access_key / secret_key or the
		// binaries see no static creds. The bucket comes from the Lakekeeper
		// warehouse, never the binary's own s3 config.
		s3cfg := map[string]any{
			"access_key": keyID,
			"secret_key": secret,
			"region":     cfg.Region,
		}
		if cfg.Endpoint != "" {
			s3cfg["endpoint"] = cfg.Endpoint
		}
		m["s3"] = s3cfg
	case "azure":
		m["azure"] = map[string]any{
			"connection_string": cfg.Credential["connection_string"],
		}
	}

	return yaml.Marshal(m)
}

// isBenignEmptyPartitionConfig reports whether the given binary's output indicates
// that no tables have been registered yet. This is a normal, non-error
// condition when a database has just been created and nothing has been marked
// for tiering (or when a database uses only the OTHER binary's mode — a
// tiered-only database has no partition-only tables for the partitioner, and
// vice versa, so each binary legitimately finds its side of
// coldfront.partition_config empty).
//
// The classification covers the ARCHIVER and the PARTITIONER: both log.Fatalf
// the same "no tables in coldfront.partition_config" message on an empty
// config. The COMPACTOR is deliberately excluded — it takes an explicit
// --table, never emits this message, and masking its failures would hide real
// problems.
//
// FRAGILE INTERIM: both binaries emit this via log.Fatalf, which exits with
// code 1 — the SAME exit code as a genuine fatal error. There is therefore no
// distinct exit code to key on today, so a substring match on the binary's log
// text is the only available signal. A robust fix needs a ColdFront upstream
// change to emit a dedicated benign exit code (tracked as a cross-team
// follow-up); until then, changes to the upstream log wording will silently
// break this detection (kept in coldFrontEmptyPartitionConfigMarker to make
// that coupling explicit).
func isBenignEmptyPartitionConfig(binary, output string) bool {
	switch binary {
	case coldFrontArchiverBinary, coldFrontPartitionerBinary:
	default:
		return false
	}
	return strings.Contains(strings.ToLower(output), coldFrontEmptyPartitionConfigMarker)
}

// tieringExecer is the minimal exec surface the tiering activity needs: run a
// command in a container and report its exit code, combined output, and any
// transport-level error. It is satisfied by *docker.Docker via
// dockerTieringExecer and lets the exit-code + benign-classification behaviour
// be unit-tested with a fake.
type tieringExecer interface {
	Exec(ctx context.Context, containerID string, cmd []string) (exitCode int, output string, err error)
}

// dockerTieringExecer adapts *docker.Docker to the tieringExecer interface.
type dockerTieringExecer struct {
	docker *docker.Docker
}

func (d dockerTieringExecer) Exec(ctx context.Context, containerID string, cmd []string) (int, string, error) {
	var buf bytes.Buffer
	// docker.Docker.Exec returns a non-nil error wrapping "command failed with
	// exit code N" for a non-zero exit. We normalise that to an explicit exit
	// code so the classification logic does not depend on error-string parsing.
	err := d.docker.Exec(ctx, &buf, containerID, cmd)
	output := buf.String()
	if err != nil {
		// A non-zero exit is reported as an error by docker.Docker.Exec. We
		// cannot recover the precise code from the wrapped error, so we report
		// a sentinel non-zero (1) which is sufficient for classification: the
		// only exit code we treat specially (benign archiver-empty) is itself
		// exit 1 and is distinguished by output text, not by the code.
		return 1, output, err
	}
	return 0, output, nil
}

// runColdFrontBinary executes the tiering binary via the supplied execer and
// classifies the result. It returns nil when the run succeeded OR when it was a
// benign archiver-empty run; it returns a non-nil error for a genuine failure.
// The caller (the workflow) maps a nil result to task success and a non-nil
// result to task failure, so this function encodes the full success/fail/benign
// decision. Credentials in the config are never included in the returned error.
func runColdFrontBinary(ctx context.Context, execer tieringExecer, containerID, binary string, cmd []string) error {
	exitCode, output, execErr := execer.Exec(ctx, containerID, cmd)
	if exitCode == 0 && execErr == nil {
		return nil
	}
	if isBenignEmptyPartitionConfig(binary, output) {
		// No tables registered yet: nothing to tier. Recorded as success.
		return nil
	}
	if execErr != nil {
		return fmt.Errorf("coldfront %s exited with error: %w\noutput:\n%s", binary, execErr, output)
	}
	return fmt.Errorf("coldfront %s exited with code %d\noutput:\n%s", binary, exitCode, output)
}

// RunColdFrontBinaryInput holds the parameters for a single tiering binary run.
type RunColdFrontBinaryInput struct {
	DatabaseID    string         `json:"database_id"`
	NodeName      string         `json:"node_name"`
	InstanceID    string         `json:"instance_id"`
	ServiceConfig map[string]any `json:"service_config"`
	DatabaseName  string         `json:"database_name"`
	Binary        string         `json:"binary"`
}

type RunColdFrontBinaryOutput struct{}

// ExecuteRunColdFrontBinary dispatches the RunColdFrontBinary activity to the
// given host's workflow queue.
func (a *Activities) ExecuteRunColdFrontBinary(
	ctx workflow.Context,
	hostID string,
	input *RunColdFrontBinaryInput,
) workflow.Future[*RunColdFrontBinaryOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*RunColdFrontBinaryOutput](ctx, options, a.RunColdFrontBinary, input)
}

// RunColdFrontBinary executes a single-pass ColdFront tiering binary
// (archiver, partitioner, or compactor) inside the primary node's Postgres
// container via docker exec. The binary's config is written to a temporary
// file inside the container using base64 to avoid shell injection. Exit codes
// are captured: a "no tables in coldfront.partition_config" non-zero exit from
// the archiver or partitioner is treated as benign (nothing to tier yet).
func (a *Activities) RunColdFrontBinary(ctx context.Context, input *RunColdFrontBinaryInput) (*RunColdFrontBinaryOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
		"binary", input.Binary,
	)
	logger.Info("running coldfront tiering binary")

	storageCfg, err := parseColdFrontStorageConfig(input.ServiceConfig)
	if err != nil {
		return nil, fmt.Errorf("coldfront %s: invalid storage config: %w", input.Binary, err)
	}
	if storageCfg == nil {
		logger.Warn("no storage provider configured; skipping coldfront run")
		return &RunColdFrontBinaryOutput{}, nil
	}

	dockerClient, err := do.Invoke[*docker.Docker](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("coldfront %s: failed to get docker client: %w", input.Binary, err)
	}

	// The lakekeeper endpoint is supplied in the service config (baked into the
	// scheduled-job args at reconciliation time as http://<serviceName>:<port>).
	lakekeeperEndpoint := ""
	if ep, ok := input.ServiceConfig["lakekeeper_endpoint"].(string); ok && ep != "" {
		lakekeeperEndpoint = ep
	}

	// The connect-as user is likewise baked into the service config at
	// reconciliation time (from spec.ConnectAsUsername). buildColdFrontConfigYAML
	// falls back to "coldfront" when it is absent.
	dsnUser := ""
	if u, ok := input.ServiceConfig["local_pg_dsn_user"].(string); ok {
		dsnUser = u
	}
	if dsnUser == "" {
		// The orchestrator always injects the connect-as user, so an empty value
		// signals a misconfiguration. buildColdFrontConfigYAML still falls back to
		// "coldfront" to stay functional, but surface it rather than silently
		// reverting to the hardcode this fix removed.
		logger.Warn("no connect-as user in tiering config; falling back to coldfront DSN user")
	}

	configYAML, err := buildColdFrontConfigYAML(
		*storageCfg, input.DatabaseName, lakekeeperEndpoint, dsnUser, input.Binary,
	)
	if err != nil {
		return nil, fmt.Errorf("coldfront %s: failed to render config: %w", input.Binary, err)
	}

	// Locate the primary's Postgres container on this host via instance ID label.
	pgContainers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", input.InstanceID)),
			filters.Arg("label", "pgedge.component=postgres"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("coldfront %s: failed to list containers for instance %s: %w",
			input.Binary, input.InstanceID, err)
	}
	if len(pgContainers) == 0 {
		return nil, fmt.Errorf("coldfront %s: no postgres container found for instance %s",
			input.Binary, input.InstanceID)
	}
	pgContainer := pgContainers[0]

	// Write the config file into the container using base64 to avoid any shell
	// quoting or injection issues, then run the binary. The path is unique per
	// run because the per-binary configs differ and the jobs can overlap.
	encoded := base64.StdEncoding.EncodeToString(configYAML)
	configPath := coldFrontConfigPath(input.Binary)
	execer := dockerTieringExecer{docker: dockerClient}

	// The compactor is per-table, so it needs the table list resolved first and
	// one invocation per table. The archiver and partitioner resolve their own
	// tables from coldfront.partition_config and run in a single pass.
	if input.Binary == coldFrontCompactorBinary {
		if err := runColdFrontCompactor(
			ctx, execer, pgContainer.ID, encoded, configPath, input.DatabaseName, dsnUser,
		); err != nil {
			return nil, err
		}
		logger.Info("coldfront tiering binary completed successfully")
		return &RunColdFrontBinaryOutput{}, nil
	}

	if err := runColdFrontStep(
		ctx, execer, pgContainer.ID, "write config",
		buildConfigWriteCommand(encoded, configPath),
	); err != nil {
		return nil, err
	}
	defer removeColdFrontConfig(ctx, execer, pgContainer.ID, configPath)

	if err := runColdFrontBinary(
		ctx, execer, pgContainer.ID, input.Binary,
		buildTieringArgv(configPath, input.Binary),
	); err != nil {
		return nil, err
	}

	logger.Info("coldfront tiering binary completed successfully")
	return &RunColdFrontBinaryOutput{}, nil
}
