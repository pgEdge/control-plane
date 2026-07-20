package activities

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/samber/do"
	"gopkg.in/yaml.v3"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// coldFrontArchiverBinary is the name of the archiver binary. The benign-empty
// classification is scoped to this binary only (see runColdFrontBinary).
const coldFrontArchiverBinary = "archiver"

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
func buildColdFrontConfigYAML(cfg coldFrontStorageConfig, dbName, lakekeeperEndpoint, dsnUser string) ([]byte, error) {
	if dsnUser == "" {
		dsnUser = "coldfront"
	}
	m := map[string]any{
		"postgres": map[string]any{
			"dsn": fmt.Sprintf("host=localhost port=5432 user=%s dbname=%s sslmode=disable", dsnUser, dbName),
		},
		"iceberg": map[string]any{
			"warehouse":           cfg.Warehouse,
			"lakekeeper_endpoint": lakekeeperEndpoint,
			"namespace":           "default",
		},
	}

	switch cfg.Provider {
	case "aws", "gcs":
		keyID := cfg.Credential["access_key_id"]
		secret := cfg.Credential["secret_access_key"]
		if cfg.Provider == "gcs" {
			keyID = cfg.Credential["hmac_access_id"]
			secret = cfg.Credential["hmac_secret"]
		}
		s3cfg := map[string]any{
			"access_key_id":     keyID,
			"secret_access_key": secret,
			"bucket":            cfg.Bucket,
			"region":            cfg.Region,
		}
		if cfg.Endpoint != "" {
			s3cfg["endpoint"] = cfg.Endpoint
		}
		if cfg.PathPrefix != "" {
			s3cfg["path_prefix"] = cfg.PathPrefix
		}
		m["s3"] = s3cfg
	case "azure":
		m["azure"] = map[string]any{
			"connection_string": cfg.Credential["connection_string"],
		}
	}

	return yaml.Marshal(m)
}

// isBenignArchiverEmpty reports whether the given binary's output indicates
// that no tables have been registered yet. This is a normal, non-error
// condition when a database has just been created and nothing has been marked
// for tiering.
//
// This classification is deliberately scoped to the ARCHIVER only: the
// partitioner and compactor must NEVER have failures masked this way.
//
// FRAGILE INTERIM: the archiver logs "no tables configured" via log.Fatalf,
// which exits with code 1 — the SAME exit code as a genuine fatal error. There
// is therefore no distinct exit code to key on today, so a substring match on
// the binary's log text is the only available signal. A robust fix needs a
// ColdFront upstream change to emit a dedicated benign exit code (tracked as a
// cross-team follow-up); until then, changes to the archiver's log wording will
// silently break this detection.
func isBenignArchiverEmpty(binary, output string) bool {
	if binary != coldFrontArchiverBinary {
		return false
	}
	return strings.Contains(strings.ToLower(output), "no tables configured")
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
	if isBenignArchiverEmpty(binary, output) {
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
// are captured: a "no tables configured" non-zero exit from the archiver is
// treated as benign (nothing to tier yet).
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

	configYAML, err := buildColdFrontConfigYAML(*storageCfg, input.DatabaseName, lakekeeperEndpoint, dsnUser)
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
	// quoting or injection issues, then run the binary.
	encoded := base64.StdEncoding.EncodeToString(configYAML)
	configPath := "/tmp/coldfront-config.yaml"
	binaryPath := "/usr/local/bin/" + input.Binary
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s && %s --config %s",
			encoded, configPath, binaryPath, configPath),
	}

	if err := runColdFrontBinary(ctx, dockerTieringExecer{docker: dockerClient}, pgContainer.ID, input.Binary, cmd); err != nil {
		return nil, err
	}

	logger.Info("coldfront tiering binary completed successfully")
	return &RunColdFrontBinaryOutput{}, nil
}
