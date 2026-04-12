package swarm

import (
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

// Shared health check timing for all service container types.
const (
	serviceHealthCheckStartPeriod = 30 * time.Second
	serviceHealthCheckInterval    = 10 * time.Second
	serviceHealthCheckTimeout     = 5 * time.Second
	serviceHealthCheckRetries     = 3
)

func buildPostgRESTEnvVars() []string {
	return []string{
		"PGRST_SERVER_HOST=0.0.0.0",
		"PGRST_SERVER_PORT=8080",
		"PGRST_ADMIN_SERVER_PORT=3001",
	}
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
	Credentials        *database.ServiceUser
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

	// Build port configuration (expose 8080 for HTTP API)
	ports := buildServicePortConfig(opts.Port)

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
	case "rag":
		user = fmt.Sprintf("%d", ragContainerUID)
		command = []string{"/app/pgedge-rag-server"}
		args = []string{"-config", "/app/data/pgedge-rag-server.yaml"}
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
// Exposes port 8080 for the HTTP API.
// If port is nil, no port is published.
// If port is non-nil and > 0, publish on that specific port.
// If port is non-nil and == 0, let Docker assign a random port.
func buildServicePortConfig(port *int) []swarm.PortConfig {
	if port == nil {
		// Do not expose any port if not specified
		return nil
	}

	config := swarm.PortConfig{
		PublishMode: swarm.PortConfigPublishModeHost,
		TargetPort:  8080,
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
