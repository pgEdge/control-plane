package swarm

import (
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// ServiceContainerSpecOptions contains all parameters needed to build a service container spec.
type ServiceContainerSpecOptions struct {
	ServiceSpec       *database.ServiceSpec
	ServiceInstanceID string
	DatabaseID        string
	DatabaseName      string
	HostID            string
	ServiceName       string
	Hostname          string
	CohortMemberID    string
	ServiceImages     *ServiceImages
	Credentials       *database.ServiceUser
	DatabaseNetworkID string
	// Database connection info
	DatabaseHost string
	DatabasePort int
	// Service port configuration
	Port *int
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

	// Build environment variables for database connection and LLM config
	env := buildServiceEnvVars(opts)

	// Get container image (already resolved in ServiceImages)
	image := opts.ServiceImages.Image

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

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:    image,
				Labels:   labels,
				Hostname: opts.Hostname,
				Env:      env,
				Healthcheck: &container.HealthConfig{
					Test:        []string{"CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"},
					StartPeriod: time.Second * 30,
					Interval:    time.Second * 10,
					Timeout:     time.Second * 5,
					Retries:     3,
				},
				Mounts: []mount.Mount{}, // No persistent volumes for services in Phase 1
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

// buildServiceEnvVars constructs environment variables for the service container.
func buildServiceEnvVars(opts *ServiceContainerSpecOptions) []string {
	env := []string{
		// Database connection
		fmt.Sprintf("PGHOST=%s", opts.DatabaseHost),
		fmt.Sprintf("PGPORT=%d", opts.DatabasePort),
		fmt.Sprintf("PGDATABASE=%s", opts.DatabaseName),
		"PGSSLMODE=prefer",

		// Service metadata
		fmt.Sprintf("PGEDGE_SERVICE_ID=%s", opts.ServiceSpec.ServiceID),
		fmt.Sprintf("PGEDGE_DATABASE_ID=%s", opts.DatabaseID),
	}

	// Add credentials if provided
	if opts.Credentials != nil {
		env = append(env,
			fmt.Sprintf("PGUSER=%s", opts.Credentials.Username),
			fmt.Sprintf("PGPASSWORD=%s", opts.Credentials.Password),
		)
	}

	// LLM configuration from serviceSpec.Config
	if provider, ok := opts.ServiceSpec.Config["llm_provider"].(string); ok {
		env = append(env, fmt.Sprintf("PGEDGE_LLM_PROVIDER=%s", provider))
	}
	if model, ok := opts.ServiceSpec.Config["llm_model"].(string); ok {
		env = append(env, fmt.Sprintf("PGEDGE_LLM_MODEL=%s", model))
	}

	// Provider-specific API keys
	if provider, ok := opts.ServiceSpec.Config["llm_provider"].(string); ok {
		switch provider {
		case "anthropic":
			if key, ok := opts.ServiceSpec.Config["anthropic_api_key"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_ANTHROPIC_API_KEY=%s", key))
			}
		case "openai":
			if key, ok := opts.ServiceSpec.Config["openai_api_key"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_OPENAI_API_KEY=%s", key))
			}
		case "ollama":
			if url, ok := opts.ServiceSpec.Config["ollama_url"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_OLLAMA_URL=%s", url))
			}
		}
	}

	return env
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
