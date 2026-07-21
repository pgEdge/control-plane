package swarm

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
)

// mcpContainerUID is the UID of the MCP container user.
const mcpContainerUID = 1001

// ragContainerUID is the UID of the RAG server container user.
const ragContainerUID = 1001

// postgrestContainerUID is the UID of the PostgREST container user.
// See: https://github.com/PostgREST/postgrest/blob/main/Dockerfile (USER 1000)
const postgrestContainerUID = 1000

// lakekeeperListenPort is the port Lakekeeper listens on inside the container.
const lakekeeperListenPort = 8181

// Shared health check timing for all service container types.
const (
	serviceHealthCheckStartPeriod = 30 * time.Second
	serviceHealthCheckInterval    = 10 * time.Second
	serviceHealthCheckTimeout     = 5 * time.Second
	serviceHealthCheckRetries     = 3
)

// lakekeeperHealthCheckStartPeriod is a longer start grace used only for the
// Lakekeeper serve container. With LAKEKEEPER__DEBUG__MIGRATE_BEFORE_SERVE=true,
// serve runs the catalog schema migration in-process before it binds its HTTP
// listener, so on a first deploy the health check must tolerate that migration
// time without marking the task unhealthy and triggering a restart loop. This
// only extends the grace before a failing check counts; serve still becomes
// healthy the instant its first check passes, and WaitForService's overall
// 5-minute budget still bounds a genuinely stuck serve.
const lakekeeperHealthCheckStartPeriod = 2 * time.Minute

func buildPostgRESTEnvVars() []string {
	// Connection details (hosts, credentials) are embedded in the db-uri inside
	// postgrest.conf by PostgRESTConfigResource — they must not appear as env vars.
	return []string{
		"PGRST_SERVER_HOST=0.0.0.0",
		"PGRST_SERVER_PORT=8080",
		"PGRST_ADMIN_SERVER_PORT=8081",
	}
}

// serviceConfigHash returns a short hex digest of a service's configuration.
// It is embedded in the container spec as PGEDGE_CONFIG_VERSION so that Docker
// Swarm detects a TaskTemplate change and restarts the container whenever the
// configuration or API keys change. This is required for services whose config
// lives in a bind-mounted file (config.yaml / pipelines): editing the file is
// invisible to Swarm, so without a TaskTemplate change the container is never
// restarted and the new config is never re-read.
func serviceConfigHash(config map[string]any) string {
	b, _ := json.Marshal(config)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:8])
}

// ServiceContainerSpecOptions contains all parameters needed to build a service container spec.
type ServiceContainerSpecOptions struct {
	ServiceSpec        *database.ServiceSpec
	ServiceInstanceID  string
	DatabaseID         string
	DatabaseName       string
	HostID             string
	ServiceName        string
	Hostname           string
	CohortMemberID     string
	ServiceImage       *ServiceImage
	DatabaseNetworkID  string
	DatabaseHosts      []database.ServiceHostEntry // Ordered Postgres host:port entries
	TargetSessionAttrs string                      // libpq target_session_attrs
	// Service port configuration
	Port *int
	// DataPath is the host-side directory path for the bind mount
	DataPath string
	// KeysPath is the host-side directory containing API key files.
	// When non-empty, it is bind-mounted read-only into the container at /app/keys.
	KeysPath string
	// KBDirPath is the host-side directory containing the KB SQLite file.
	// When non-empty, it is bind-mounted read-only into the container at /app/kb.
	KBDirPath string
}

// ServiceContainerSpec builds a Docker Swarm service spec for a service instance.
func ServiceContainerSpec(opts *ServiceContainerSpecOptions) (swarm.ServiceSpec, error) {
	// Build labels for service discovery
	labels := map[string]string{
		"pgedge.component":           "service",
		"pgedge.service.instance.id": opts.ServiceInstanceID,
		"pgedge.service.id":          opts.ServiceSpec.ServiceID,
		"pgedge.database.id":         opts.DatabaseID,
		"pgedge.host.id":             opts.HostID,
	}

	// Extract swarm orchestrator options (matches Postgres pattern in spec.go).
	// ExtraVolumes and DriverOpts are rejected at the API validation layer
	// (validateServiceOrchestratorOpts).
	var swarmOpts *database.SwarmOpts
	if opts.ServiceSpec.OrchestratorOpts != nil {
		swarmOpts = opts.ServiceSpec.OrchestratorOpts.Swarm
	}

	// Merge user-provided extra labels
	if swarmOpts != nil {
		for k, v := range swarmOpts.ExtraLabels {
			labels[k] = v
		}
	}

	// Build networks - attach to both bridge and database overlay networks
	// Bridge network provides:
	// - Control Plane access to service health/API endpoints (port 8080)
	// - External accessibility for end-users via published ports
	// Database overlay network provides:
	// - Connectivity to Postgres instances
	// - Network isolation per database
	networks := []swarm.NetworkAttachmentConfig{
		{
			Target: "bridge",
		},
		{
			Target: opts.DatabaseNetworkID,
		},
	}

	// Append user-requested extra networks (e.g. Traefik, reverse proxy).
	if swarmOpts != nil {
		for _, net := range swarmOpts.ExtraNetworks {
			networks = append(networks, swarm.NetworkAttachmentConfig{
				Target:  net.ID,
				Aliases: net.Aliases,
			})
		}
	}

	// Get container image (already resolved in ServiceImage)
	image := opts.ServiceImage.Tag

	// Determine target port: most services use 8080, Lakekeeper uses 8181.
	containerPort := 8080
	if opts.ServiceSpec.ServiceType == "coldfront" {
		containerPort = lakekeeperListenPort
	}

	// Build port configuration
	ports := buildServicePortConfig(opts.Port, containerPort)

	// Build resource limits
	var resources *swarm.ResourceRequirements
	if opts.ServiceSpec.CPUs != nil || opts.ServiceSpec.MemoryBytes != nil {
		resources = &swarm.ResourceRequirements{
			Limits: &swarm.Limit{},
		}
		if opts.ServiceSpec.CPUs != nil {
			resources.Limits.NanoCPUs = int64(*opts.ServiceSpec.CPUs * 1e9)
		}
		if opts.ServiceSpec.MemoryBytes != nil {
			resources.Limits.MemoryBytes = int64(*opts.ServiceSpec.MemoryBytes)
		}
	}

	// Build the container-spec fields that vary by service type.
	var (
		command     []string
		args        []string
		env         []string
		user        string
		healthcheck *container.HealthConfig
		mounts      []mount.Mount
	)

	switch opts.ServiceSpec.ServiceType {
	case "postgrest":
		user = fmt.Sprintf("%d", postgrestContainerUID)
		command = []string{"postgrest"}
		args = []string{"/app/data/postgrest.conf"}
		env = buildPostgRESTEnvVars()
		// postgrest --ready exits 0/1; no curl in the static binary image.
		healthcheck = &container.HealthConfig{
			Test:        []string{"CMD", "postgrest", "--ready"},
			StartPeriod: serviceHealthCheckStartPeriod,
			Interval:    serviceHealthCheckInterval,
			Timeout:     serviceHealthCheckTimeout,
			Retries:     serviceHealthCheckRetries,
		}
		mounts = []mount.Mount{
			docker.BuildMount(opts.DataPath, "/app/data", true),
		}
	case "mcp":
		user = fmt.Sprintf("%d", mcpContainerUID)
		// Override the default container entrypoint to specify config path on bind mount.
		command = []string{"/app/pgedge-postgres-mcp"}
		args = []string{"-config", "/app/data/config.yaml"}
		// Embed a hash of the service config so that Docker Swarm detects a
		// TaskTemplate change and restarts the container when the config changes.
		// The MCP config (config.yaml) lives on a bind mount, so edits to it are
		// invisible to Swarm. SIGHUP reloads the database client connections but
		// does NOT re-initialize the knowledgebase, so KB config changes (path,
		// provider, model, key) only take effect on a restart. Without this, a
		// changed kb_database_host_path silently keeps using the old KB file.
		// Connection details (hosts, target_session_attrs) are intentionally not
		// part of the config map, so failover-driven reconnects still use SIGHUP
		// without forcing a restart.
		env = []string{"PGEDGE_CONFIG_VERSION=" + serviceConfigHash(opts.ServiceSpec.Config)}
		healthcheck = &container.HealthConfig{
			Test:        []string{"CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"},
			StartPeriod: serviceHealthCheckStartPeriod,
			Interval:    serviceHealthCheckInterval,
			Timeout:     serviceHealthCheckTimeout,
			Retries:     serviceHealthCheckRetries,
		}
		mounts = []mount.Mount{
			docker.BuildMount(opts.DataPath, "/app/data", false),
		}
		if opts.KBDirPath != "" {
			mounts = append(mounts, docker.BuildMount(opts.KBDirPath, "/app/kb", true))
		}
	case "rag":
		user = fmt.Sprintf("%d", ragContainerUID)
		command = []string{"/app/pgedge-rag-server"}
		args = []string{"-config", "/app/data/pgedge-rag-server.yaml"}
		// Embed a hash of the service config so that Docker Swarm detects a
		// TaskTemplate change and restarts the container when pipelines or API
		// keys change. Without this, bind-mount updates are invisible to Swarm.
		env = []string{"PGEDGE_CONFIG_VERSION=" + serviceConfigHash(opts.ServiceSpec.Config)}
		// No curl in the RHEL minimal image — use a TCP probe instead.
		healthcheck = &container.HealthConfig{
			Test:        []string{"CMD-SHELL", "exec 3<>/dev/tcp/127.0.0.1/8080"},
			StartPeriod: serviceHealthCheckStartPeriod,
			Interval:    serviceHealthCheckInterval,
			Timeout:     serviceHealthCheckTimeout,
			Retries:     serviceHealthCheckRetries,
		}
		mounts = []mount.Mount{
			docker.BuildMount(opts.DataPath, "/app/data", false),
		}
		if opts.KeysPath != "" {
			mounts = append(mounts, docker.BuildMount(opts.KeysPath, "/app/keys", true))
		}
	case "coldfront":
		// Lakekeeper is an Apache Iceberg REST catalog backed by an external
		// Postgres instance. Connection details are supplied by the caller via
		// ServiceSpec.Config. The LAKEKEEPER__ env vars are the idiomatic
		// configuration mechanism for this service.
		// Both catalog_db_url and pg_encryption_key are validated at spec time
		// (validateLakekeeperServiceConfig / generateLakekeeperInstanceResources),
		// so they will be non-empty here during normal operation.
		catalogDBURL, _ := opts.ServiceSpec.Config["catalog_db_url"].(string)
		pgEncryptionKey, _ := opts.ServiceSpec.Config["pg_encryption_key"].(string)
		// The lakekeeper image ENTRYPOINT is the lakekeeper binary itself, so
		// "serve" must be an ARG appended to it (Swarm ContainerSpec.Command
		// would REPLACE the entrypoint → exec "serve" not found).
		args = []string{"serve"}
		env = []string{
			"LAKEKEEPER__PG_DATABASE_URL_READ=" + catalogDBURL,
			"LAKEKEEPER__PG_DATABASE_URL_WRITE=" + catalogDBURL,
			"LAKEKEEPER__PG_ENCRYPTION_KEY=" + pgEncryptionKey,
			fmt.Sprintf("LAKEKEEPER__LISTEN_PORT=%d", lakekeeperListenPort),
			// Migrate the Iceberg catalog schema in-process on startup rather than
			// via a separate one-shot container. serve runs the migration, then
			// begins serving — so nothing else needs to join the database overlay
			// and the overlay no longer has to be attachable.
			"LAKEKEEPER__DEBUG__MIGRATE_BEFORE_SERVE=true",
		}
		healthcheck = &container.HealthConfig{
			// "healthcheck" is a SUBCOMMAND of the lakekeeper binary, not a
			// standalone executable, and the image is distroless (no shell), so
			// the healthcheck must invoke the binary by its absolute path.
			Test: []string{"CMD", "/home/nonroot/lakekeeper", "healthcheck"},
			// serve runs the catalog migration in-process before it binds its
			// listener (MIGRATE_BEFORE_SERVE), so it needs a longer start grace
			// than the other services to avoid being marked unhealthy mid-migration.
			StartPeriod: lakekeeperHealthCheckStartPeriod,
			Interval:    serviceHealthCheckInterval,
			Timeout:     serviceHealthCheckTimeout,
			Retries:     serviceHealthCheckRetries,
		}
		if opts.DataPath != "" {
			mounts = []mount.Mount{
				docker.BuildMount(opts.DataPath, "/app/data", false),
			}
		}
	default:
		return swarm.ServiceSpec{}, fmt.Errorf("unsupported service type: %q", opts.ServiceSpec.ServiceType)
	}

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:       image,
				Labels:      labels,
				Hostname:    opts.Hostname,
				User:        user,
				Command:     command,
				Args:        args,
				Env:         env,
				Healthcheck: healthcheck,
				Mounts:      mounts,
			},
			Networks: networks,
			Placement: &swarm.Placement{
				Constraints: []string{
					"node.id==" + opts.CohortMemberID,
				},
			},
			Resources: resources,
		},
		EndpointSpec: &swarm.EndpointSpec{
			Mode:  swarm.ResolutionModeVIP,
			Ports: ports,
		},
		Annotations: swarm.Annotations{
			Name:   opts.ServiceName,
			Labels: labels,
		},
	}, nil
}

// buildServicePortConfig builds port configuration for service containers.
// targetPort is the port the service listens on inside the container (typically 8080,
// but 8181 for Lakekeeper).
// If port is nil, no port is published.
// If port is non-nil and > 0, publish on that specific host port.
// If port is non-nil and == 0, let Docker assign a random port.
func buildServicePortConfig(port *int, targetPort int) []swarm.PortConfig {
	if port == nil {
		// Do not expose any port if not specified
		return nil
	}

	config := swarm.PortConfig{
		PublishMode: swarm.PortConfigPublishModeHost,
		TargetPort:  uint32(targetPort),
		Name:        "http",
		Protocol:    swarm.PortConfigProtocolTCP,
	}

	if *port > 0 {
		config.PublishedPort = uint32(*port)
	} else if *port == 0 {
		// Port 0 means random port assigned
		config.PublishedPort = 0
	}

	return []swarm.PortConfig{config}
}
