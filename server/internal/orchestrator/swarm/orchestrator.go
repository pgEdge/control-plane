package swarm

import (
	"context"
	"fmt"
	"path"

	"github.com/docker/docker/api/types/swarm"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/host"
)

var _ host.Orchestrator = (*Orchestrator)(nil)
var _ database.Orchestrator = (*Orchestrator)(nil)

var defaultVersion = host.MustPgEdgeVersion("17", "4")

// TODO: What should this look like?
var allVersions = []*host.PgEdgeVersion{
	defaultVersion,
	host.MustPgEdgeVersion("16", "4"),
	host.MustPgEdgeVersion("15", "4"),
}

type Orchestrator struct {
	cfg    config.Config
	docker *docker.Docker
}

func NewOrchestrator(cfg config.Config, d *docker.Docker) *Orchestrator {
	return &Orchestrator{docker: d}
}

func (o *Orchestrator) PopulateHost(ctx context.Context, h *host.Host) error {
	info, err := o.docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get docker info: %w", err)
	}
	h.CPUs = info.NCPU
	h.MemBytes = uint64(info.MemTotal)
	h.Cohort = &host.Cohort{
		Type:             host.CohortTypeSwarm,
		CohortID:         info.Swarm.Cluster.ID,
		MemberID:         info.Swarm.NodeID,
		ControlAvailable: info.Swarm.ControlAvailable,
	}
	h.DefaultPgEdgeVersion = defaultVersion
	h.SupportedPgEdgeVersions = allVersions

	return nil
}

func (o *Orchestrator) PopulateHostStatus(ctx context.Context, status *host.HostStatus) error {
	info, err := o.docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get docker info: %w", err)
	}
	status.Components["docker"] = common.ComponentStatus{
		Name:    "docker",
		Healthy: info.Swarm.LocalNodeState == swarm.LocalNodeStateActive,
		Error:   info.Swarm.Error,
		Details: map[string]any{
			"containers":              info.Containers,
			"containers_running":      info.ContainersRunning,
			"containers_stopped":      info.ContainersStopped,
			"containers_paused":       info.ContainersPaused,
			"swarm.local_node_state":  info.Swarm.LocalNodeState,
			"swarm.control_available": info.Swarm.ControlAvailable,
			"swarm.error":             info.Swarm.Error,
		},
	}
	return nil
}

func (o *Orchestrator) PopulateInstanceSpec(ctx context.Context, spec *database.InstanceSpec) error {
	// version, err := host.NewPgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
	// if err != nil {
	// 	return fmt.Errorf("failed to parse version from instance spec: %w", err)
	// }
	if spec.StorageClass == "" {
		spec.StorageClass = "local"
	}
	_, err := GetImages(o.cfg, spec.PgEdgeVersion)
	if err != nil {
		return err
	}
	return nil
}

type Images struct {
	PgEdgeImage string
}

func GetImages(cfg config.Config, version *host.PgEdgeVersion) (*Images, error) {
	// TODO: Real implementation
	var tag string
	switch version.PostgresVersion.Major() {
	case 17:
		tag = "pgedge:pg17_4.0.10-3"
	case 16:
		tag = "pgedge:pg16_4.0.10-3"
	case 15:
		tag = "pgedge:pg15_4.0.10-3"
	default:
		return nil, fmt.Errorf("unsupported postgres version: %q", version.PostgresVersion)
	}

	return &Images{
		PgEdgeImage: path.Join(cfg.DockerSwarm.ImageRepositoryHost, tag),
	}, nil
}
