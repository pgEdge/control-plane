package swarm

import (
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
)

type Paths struct {
	Configs      string
	Certificates string
	Data         string
}

type HostOptions struct {
	// BridgeNetworkID   string
	DatabaseNetworkID string
	Paths             Paths
}

func DatabaseServiceSpec(
	host *host.Host,
	cfg config.Config,
	instance *database.InstanceSpec,
	options *HostOptions,
) (swarm.ServiceSpec, error) {
	images, err := GetImages(cfg, instance.PgEdgeVersion)
	if err != nil {
		return swarm.ServiceSpec{}, fmt.Errorf("could not find matching image: %w", err)
	}

	labels := map[string]string{
		"pgedge.host.id":     instance.HostID.String(),
		"pgedge.database.id": instance.DatabaseID.String(),
		"pgedge.instance.id": instance.InstanceID.String(),
		"pgedge.node.name":   instance.NodeName,
		"pgedge.component":   "postgres",
	}
	if instance.TenantID != nil {
		labels["pgedge.tenant.id"] = instance.TenantID.String()
	}
	if instance.ReplicaName != "" {
		labels["pgedge.replica.name"] = instance.ReplicaName
	}

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:    images.PgEdgeImage,
				Labels:   labels,
				Args:     []string{"/opt/pgedge/configs/patroni.yaml"},
				Hostname: instance.DatabaseID.String() + "_postgres-" + instance.NodeName + instance.ReplicaName,
				Env: []string{
					"PATRONICTL_CONFIG_FILE=/opt/pgedge/configs/patroni.yaml",
				},
				Healthcheck: &container.HealthConfig{
					Test:        []string{"CMD-SHELL", "curl -Ssf http://localhost:8888/health"},
					StartPeriod: time.Second * 5,
					Interval:    time.Second * 5,
					Timeout:     time.Second * 3,
					Retries:     10,
				},
				Mounts: []mount.Mount{
					{
						Type:     mount.TypeBind,
						Source:   options.Paths.Configs,
						Target:   "/opt/pgedge/configs",
						ReadOnly: true,
					},
					{
						// We're using a mount for the certificates instead of
						// a secret because secrets can't be rotated without
						// restarting the container.
						Type:     mount.TypeBind,
						Source:   options.Paths.Certificates,
						Target:   "/opt/pgedge/certificates",
						ReadOnly: true,
					},
					{
						Type:   mount.TypeBind,
						Source: options.Paths.Data,
						Target: "/opt/pgedge/data",
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{
					Target: "bridge",
				},
				{
					Target: options.DatabaseNetworkID,
					Aliases: []string{
						"postgres-" + instance.NodeName + instance.ReplicaName,
					},
				},
			},
			Placement: &swarm.Placement{
				Constraints: []string{
					"node.id==" + host.Cohort.MemberID,
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
			Name:   instance.DatabaseID.String() + "_postgres-" + instance.NodeName + instance.ReplicaName,
			Labels: labels,
		},
	}, nil
}
