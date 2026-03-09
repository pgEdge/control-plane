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
	ServiceImage      *ServiceImage
	Credentials       *database.ServiceUser
	DatabaseNetworkID string
	// Database connection info
	DatabaseHost string
	DatabasePort int
	// Service port configuration
	Port *int
	// SwarmConfigID is the Docker Swarm config ID to mount into the container.
	// When set, the config is mounted at the service-type-specific config path.
	// Used by services that require a config file (e.g. RAG server).
	SwarmConfigID string
}

// serviceHealthCheckPath returns the HTTP health check path for a service type.
func serviceHealthCheckPath(serviceType string) string {
	if serviceType == "rag" {
		return "/v1/health"
	}
	return "/health"
}

// serviceHealthCheckTest returns the Docker health check Test command for a
// service type. Services whose images include curl use the standard curl check.
// The RAG server image (RHEL minimal) ships without curl/wget, so it falls back
// to a bash /dev/tcp TCP connectivity check against port 8080.
func serviceHealthCheckTest(serviceType string) []string {
	if serviceType == "rag" {
		// No curl/wget in the RAG server image — use bash built-in /dev/tcp.
		return []string{"CMD-SHELL", "exec 3<>/dev/tcp/127.0.0.1/8080"}
	}
	return []string{"CMD-SHELL", fmt.Sprintf("curl -f http://localhost:8080%s || exit 1", serviceHealthCheckPath(serviceType))}
}

// serviceConfigMountPath returns the path inside the container where the
// Swarm config should be mounted for a given service type.
func serviceConfigMountPath(serviceType string) string {
	if serviceType == "rag" {
		return "/etc/pgedge/pgedge-rag-server.yaml"
	}
	return ""
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

	containerSpec := &swarm.ContainerSpec{
		Image:    image,
		Labels:   labels,
		Hostname: opts.Hostname,
		Env:      env,
		Healthcheck: &container.HealthConfig{
			Test:        serviceHealthCheckTest(opts.ServiceSpec.ServiceType),
			StartPeriod: time.Second * 30,
			Interval:    time.Second * 10,
			Timeout:     time.Second * 5,
			Retries:     3,
		},
		Mounts: []mount.Mount{}, // No persistent volumes for services in Phase 1
	}

	// Mount a Swarm config as a file inside the container when provided.
	if opts.SwarmConfigID != "" {
		mountPath := serviceConfigMountPath(opts.ServiceSpec.ServiceType)
		if mountPath != "" {
			containerSpec.Configs = []*swarm.ConfigReference{
				{
					ConfigID:   opts.SwarmConfigID,
					ConfigName: fmt.Sprintf("rag-config-%s", opts.ServiceInstanceID),
					File: &swarm.ConfigReferenceFileTarget{
						Name: mountPath,
						UID:  "0",
						GID:  "0",
						Mode: 0o444,
					},
				},
			}
		}
	}

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: containerSpec,
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

	switch opts.ServiceSpec.ServiceType {
	case "rag":
		env = append(env, buildRAGEnvVars(opts.ServiceSpec.Config)...)
	default:
		// MCP and other services: PGEDGE_-prefixed LLM vars
		env = append(env, buildMCPEnvVars(opts.ServiceSpec.Config)...)
	}

	return env
}

// buildMCPEnvVars returns PGEDGE_-prefixed LLM env vars for the MCP service.
func buildMCPEnvVars(config map[string]any) []string {
	var env []string
	if provider, ok := config["llm_provider"].(string); ok {
		env = append(env, fmt.Sprintf("PGEDGE_LLM_PROVIDER=%s", provider))
	}
	if model, ok := config["llm_model"].(string); ok {
		env = append(env, fmt.Sprintf("PGEDGE_LLM_MODEL=%s", model))
	}
	if provider, ok := config["llm_provider"].(string); ok {
		switch provider {
		case "anthropic":
			if key, ok := config["anthropic_api_key"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_ANTHROPIC_API_KEY=%s", key))
			}
		case "openai":
			if key, ok := config["openai_api_key"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_OPENAI_API_KEY=%s", key))
			}
		case "ollama":
			if url, ok := config["ollama_url"].(string); ok {
				env = append(env, fmt.Sprintf("PGEDGE_OLLAMA_URL=%s", url))
			}
		}
	}
	return env
}

// buildRAGEnvVars returns the standard API key env vars expected by the RAG server.
// The RAG server reads its full config from a YAML file (delivered via Swarm config);
// only API keys are injected as environment variables.
//
// Keys may appear at the top level of config or inside individual pipeline entries.
// All unique keys across all locations are collected and set once.
func buildRAGEnvVars(config map[string]any) []string {
	// Gather all config maps to scan: top-level + each pipeline entry.
	sources := []map[string]any{config}
	if pipelines, ok := config["pipelines"].([]any); ok {
		for _, raw := range pipelines {
			if p, ok := raw.(map[string]any); ok {
				sources = append(sources, p)
			}
		}
	}

	type keyDef struct{ field, envVar string }
	keys := []keyDef{
		{"openai_api_key", "OPENAI_API_KEY"},
		{"anthropic_api_key", "ANTHROPIC_API_KEY"},
		{"voyage_api_key", "VOYAGE_API_KEY"},
	}

	var env []string
	for _, k := range keys {
		for _, src := range sources {
			if val, ok := src[k.field].(string); ok && val != "" {
				env = append(env, fmt.Sprintf("%s=%s", k.envVar, val))
				break // first non-empty value wins; skip remaining sources
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
		PublishMode: swarm.PortConfigPublishModeIngress,
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
