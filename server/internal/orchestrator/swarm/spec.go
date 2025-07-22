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
	ServiceName       string
	InstanceHostname  string
	DatabaseNetworkID string
	Paths             Paths
	Images            *Images
	CohortMemberID    string
}

func DatabaseServiceSpec(
	instance *database.InstanceSpec,
	options *HostOptions,
) (swarm.ServiceSpec, error) {
	var swarmOpts *database.SwarmOpts
	if instance.OrchestratorOpts != nil {
		swarmOpts = instance.OrchestratorOpts.Swarm
	}
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

	networks := []swarm.NetworkAttachmentConfig{
		{
			Target: "bridge", // always attached
		},
		{
			Target: options.DatabaseNetworkID, // database-specific
		},
	}
	if swarmOpts != nil {
		for k, v := range swarmOpts.ExtraLabels {
			labels[k] = v
		}

		for _, vol := range swarmOpts.ExtraVolumes {
			mounts = append(mounts, docker.BuildMount(vol.HostPath, vol.DestinationPath, false))
		}

		for _, net := range instance.OrchestratorOpts.Swarm.ExtraNetworks {
			networks = append(networks, swarm.NetworkAttachmentConfig{
				Target:  net.ID,
				Aliases: net.Aliases,
			})
		}
	}

	ports := buildPostgresPortConfig(instance.Port)

	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:    options.Images.PgEdgeImage,
				Labels:   labels,
				Args:     []string{"/opt/pgedge/configs/patroni.yaml"},
				Hostname: options.InstanceHostname,
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
			Mode:  swarm.ResolutionModeVIP,
			Ports: ports,
		},
		Annotations: swarm.Annotations{
			Name:   options.ServiceName,
			Labels: labels,
		},
	}, nil
}

func buildPostgresPortConfig(port *int) []swarm.PortConfig {
	if port == nil {
		// Do not expose any port if not specified
		return nil
	}

	config := swarm.PortConfig{
		PublishMode: swarm.PortConfigPublishModeHost,
		TargetPort:  5432,
		Name:        "postgres",
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
