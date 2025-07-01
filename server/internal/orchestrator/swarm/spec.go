package swarm

import (
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
)

type Paths struct {
	Configs      string
	Certificates string
	Data         string
}

type HostOptions struct {
	// BridgeNetworkID   string
	ServiceName       string
	DatabaseNetworkID string
	Paths             Paths
	Images            *Images
	CohortMemberID    string
}

func DatabaseServiceSpec(
	instance *database.InstanceSpec,
	options *HostOptions,
) (swarm.ServiceSpec, error) {
	labels := map[string]string{
		"pgedge.host.id":     instance.HostID,
		"pgedge.database.id": instance.DatabaseID,
		"pgedge.instance.id": instance.InstanceID,
		"pgedge.node.name":   instance.NodeName,
		"pgedge.component":   "postgres",
	}
	if instance.TenantID != nil {
		labels["pgedge.tenant.id"] = *instance.TenantID
	}

	mounts := []mount.Mount{
		docker.BuildMount(options.Paths.Configs, "/opt/pgedge/configs", true),
		// We're using a mount for the certificates instead of
		// a secret because secrets can't be rotated without
		// restarting the container.
		docker.BuildMount(options.Paths.Certificates, "/opt/pgedge/certificates", true),
		docker.BuildMount(options.Paths.Data, "/opt/pgedge/data", false),
	}

	if instance.OrchestratorOpts != nil && instance.OrchestratorOpts.Swarm != nil {
		for _, vol := range instance.OrchestratorOpts.Swarm.ExtraVolumes {
			mounts = append(mounts, docker.BuildMount(vol.HostPath, vol.DestinationPath, false))
		}
	}

	networks := []swarm.NetworkAttachmentConfig{
		{
			Target: "bridge", // always attached
		},
		{
			Target: options.DatabaseNetworkID, // database-specific
		},
	}
	if instance.OrchestratorOpts != nil && instance.OrchestratorOpts.Swarm != nil {
		for _, net := range instance.OrchestratorOpts.Swarm.ExtraNetworks {
			networks = append(networks, swarm.NetworkAttachmentConfig{
				Target:  net.ID,
				Aliases: net.Aliases,
			})
		}
	}

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:    options.Images.PgEdgeImage,
				Labels:   labels,
				Args:     []string{"/opt/pgedge/configs/patroni.yaml"},
				Hostname: instance.Hostname(),
				Env: []string{
					"PATRONICTL_CONFIG_FILE=/opt/pgedge/configs/patroni.yaml",
				},
				Healthcheck: &container.HealthConfig{
					Test:        []string{"CMD-SHELL", "curl -Ssf http://localhost:8888/liveness"},
					StartPeriod: time.Second * 5,
					Interval:    time.Second * 5,
					Timeout:     time.Second * 3,
					Retries:     10,
				},
				Mounts: mounts,
			},
			Networks: networks,
			Placement: &swarm.Placement{
				Constraints: []string{
					"node.id==" + options.CohortMemberID,
				},
			},
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{
					MemoryBytes: int64(instance.MemoryBytes),
					NanoCPUs:    int64(instance.CPUs * 1e9),
				},
			},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Mode: swarm.ResolutionModeVIP,
			Ports: []swarm.PortConfig{
				{
					// TODO: This could get complicated. In the DE use case, we
					// don't want to expose the port directly, and instead go
					// through traefik. In EE, there could be cases where we
					// want both traefik and direct access. For example, if you
					// wanted to make a heterogeneous cluster where some
					// instances use systemd, you would need to expose a port
					// regardless of whether you're using traefik.
					// For now, since we're not running traefik yet, we'll just
					// expose the port directly.
					PublishMode:   swarm.PortConfigPublishModeHost,
					TargetPort:    uint32(5432),
					PublishedPort: uint32(instance.Port),
					Name:          "postgres",
					Protocol:      swarm.PortConfigProtocolTCP,
				},
			},
		},
		Annotations: swarm.Annotations{
			Name:   options.ServiceName,
			Labels: labels,
		},
	}, nil
}
