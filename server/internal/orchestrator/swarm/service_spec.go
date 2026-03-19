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
	// Service port configuration
	Port *int
	// DataPath is the host-side directory path for the bind mount
	DataPath string
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

	// Merge user-provided extra labels (matches Postgres ExtraLabels behavior)
	if opts.ServiceSpec.OrchestratorOpts != nil && opts.ServiceSpec.OrchestratorOpts.Swarm != nil {
		for k, v := range opts.ServiceSpec.OrchestratorOpts.Swarm.ExtraLabels {
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

	// Build bind mount for config/auth files
	mounts := []mount.Mount{
		docker.BuildMount(opts.DataPath, "/app/data", false),
	}

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:    image,
				Labels:   labels,
				Hostname: opts.Hostname,
				User:     fmt.Sprintf("%d", mcpContainerUID),
				// override the default container entrypoint so we can specify path to config on bind mount
				Command: []string{"/app/pgedge-postgres-mcp"},
				Args:    []string{"-config", "/app/data/config.yaml"},
				Healthcheck: &container.HealthConfig{
					Test:        []string{"CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"},
					StartPeriod: time.Second * 30,
					Interval:    time.Second * 10,
					Timeout:     time.Second * 5,
					Retries:     3,
				},
				Mounts: mounts,
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
