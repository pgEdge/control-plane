package swarm

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"path"
	"path/filepath"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var defaultVersion = host.MustPgEdgeVersion("17", "4")

// TODO: What should this look like?
var allVersions = []*host.PgEdgeVersion{
	defaultVersion,
	host.MustPgEdgeVersion("16", "4"),
	host.MustPgEdgeVersion("15", "4"),
}

type Orchestrator struct {
	cfg                config.Config
	docker             *docker.Docker
	dbNetworkAllocator Allocator
	bridgeNetwork      *docker.NetworkInfo
	cpus               int
	memBytes           uint64
	swarmID            string
	swarmNodeID        string
	controlAvailable   bool
}

func NewOrchestrator(ctx context.Context, cfg config.Config, d *docker.Docker) (*Orchestrator, error) {
	info, err := d.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker info: %w", err)
	}
	// Using the default bridge network to avoid this issue:
	// https://github.com/moby/moby/issues/37087
	bridge, err := d.NetworkInspect(ctx, "bridge", network.InspectOptions{
		Scope: "local",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the default bridge network: %w", err)
	}
	bridgeInfo, err := docker.ExtractNetworkInfo(bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bridge network info: %w", err)
	}
	dbNetworkPrefix, err := netip.ParsePrefix(cfg.DockerSwarm.DatabaseNetworksCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database network CIDR: %w", err)
	}

	return &Orchestrator{
		cfg:    cfg,
		docker: d,
		dbNetworkAllocator: Allocator{
			Prefix: dbNetworkPrefix,
			Bits:   cfg.DockerSwarm.DatabaseNetworksSubnetBits,
		},
		bridgeNetwork:    bridgeInfo,
		cpus:             info.NCPU,
		memBytes:         uint64(info.MemTotal),
		swarmID:          info.Swarm.Cluster.ID,
		swarmNodeID:      info.Swarm.NodeID,
		controlAvailable: info.Swarm.ControlAvailable,
	}, nil
}

func (o *Orchestrator) PopulateHost(ctx context.Context, h *host.Host) error {
	h.CPUs = o.cpus
	h.MemBytes = o.memBytes
	h.Cohort = &host.Cohort{
		Type:             host.CohortTypeSwarm,
		CohortID:         o.swarmID,
		MemberID:         o.swarmNodeID,
		ControlAvailable: o.controlAvailable,
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

func (o *Orchestrator) GenerateInstanceResources(spec *database.InstanceSpec) (*database.InstanceResources, error) {
	// instanceDir := filepath.Join(o.cfg.DataDir, "databases", spec.DatabaseID.String(), spec.InstanceID.String())
	images, err := GetImages(o.cfg, spec.PgEdgeVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get images: %w", err)
	}

	instanceHostname := fmt.Sprintf("postgres-%s-%s", spec.NodeName, spec.InstanceID)

	// If there's more than one instance from the same swarm cluster, each
	// instance will output this same network. They'll get deduplicated when we
	// add them to the state.
	databaseNetwork := &Network{
		CohortID:  o.swarmID,
		Scope:     "swarm",
		Driver:    "overlay",
		Name:      fmt.Sprintf("%s-database", spec.DatabaseID),
		Allocator: o.dbNetworkAllocator,
	}

	// directory resources
	instanceDir := &filesystem.DirResource{
		ID:     spec.InstanceID.String() + "-instance",
		HostID: spec.HostID,
		Path:   filepath.Join(o.cfg.DataDir, "instances", spec.InstanceID.String()),
	}
	dataDir := &filesystem.DirResource{
		ID:       spec.InstanceID.String() + "-data",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "data",
		OwnerUID: o.cfg.DatabaseOwnerUID,
		OwnerGID: o.cfg.DatabaseOwnerUID,
	}
	configsDir := &filesystem.DirResource{
		ID:       spec.InstanceID.String() + "-configs",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "configs",
		OwnerUID: o.cfg.DatabaseOwnerUID,
		OwnerGID: o.cfg.DatabaseOwnerUID,
	}
	certificatesDir := &filesystem.DirResource{
		ID:       spec.InstanceID.String() + "-certificates",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "certificates",
		OwnerUID: o.cfg.DatabaseOwnerUID,
		OwnerGID: o.cfg.DatabaseOwnerUID,
	}

	// patroni resources - used to clean up etcd on deletion
	patroniCluster := &PatroniCluster{
		ClusterID:        o.cfg.ClusterID,
		DatabaseID:       spec.DatabaseID,
		NodeName:         spec.NodeName,
		PatroniNamespace: patroni.Namespace(spec.DatabaseID, spec.NodeName),
	}
	patroniMember := &PatroniMember{
		ClusterID:  o.cfg.ClusterID,
		NodeName:   spec.NodeName,
		InstanceID: spec.InstanceID,
	}

	// file resources
	etcdCreds := &EtcdCreds{
		InstanceID: spec.InstanceID,
		HostID:     spec.HostID,
		DatabaseID: spec.DatabaseID,
		NodeName:   spec.NodeName,
		ParentID:   certificatesDir.ID,
		OwnerUID:   o.cfg.DatabaseOwnerUID,
		OwnerGID:   o.cfg.DatabaseOwnerUID,
	}
	postgresCerts := &PostgresCerts{
		InstanceID:       spec.InstanceID,
		HostID:           spec.HostID,
		ParentID:         certificatesDir.ID,
		InstanceHostname: instanceHostname,
		OwnerUID:         o.cfg.DatabaseOwnerUID,
		OwnerGID:         o.cfg.DatabaseOwnerUID,
	}
	patroniConfig := &PatroniConfig{
		Spec:                spec,
		HostCPUs:            float64(o.cpus),
		HostMemoryBytes:     o.memBytes,
		DatabaseNetworkName: databaseNetwork.Name,
		BridgeNetworkInfo:   o.bridgeNetwork,
		ParentID:            configsDir.ID,
		OwnerUID:            o.cfg.DatabaseOwnerUID,
		OwnerGID:            o.cfg.DatabaseOwnerUID,
		InstanceHostname:    instanceHostname,
	}

	// data := &DataDir{
	// 	HostID:    spec.HostID,
	// 	Path:      filepath.Join(instanceDir, "data"),
	// 	SizeBytes: spec.StorageSizeBytes,
	// }

	// cfgs := &InstanceConfigs{
	// 	HostID:           spec.HostID,
	// 	InstanceID:       spec.InstanceID,
	// 	DatabaseID:       spec.DatabaseID,
	// 	InstanceHostname: instanceHostname,
	// 	ClusterSize:      spec.ClusterSize,
	// 	CertificatesDir:  filepath.Join(instanceDir, "certificates"),
	// 	ConfigsDir:       filepath.Join(instanceDir, "configs"),
	// }

	serviceName := fmt.Sprintf("postgres-%s-%s", spec.NodeName, spec.InstanceID)
	serviceSpec := &PostgresServiceSpecResource{
		Instance:            spec,
		CohortMemberID:      o.swarmNodeID,
		Images:              images,
		ServiceName:         serviceName,
		DatabaseNetworkName: databaseNetwork.Name,
		DataDirID:           dataDir.ID,
		ConfigsDirID:        configsDir.ID,
		CertificatesDirID:   certificatesDir.ID,
	}
	service := &PostgresService{
		Instance:    spec,
		CohortID:    o.swarmID,
		ServiceName: serviceName,
	}

	instance := &database.InstanceResource{
		Spec:             spec,
		InstanceHostname: instanceHostname,
		OrchestratorDependencies: []resource.Identifier{
			service.Identifier(),
		},
	}

	orchestratorResources := []resource.Resource{
		databaseNetwork,
		patroniCluster,
		patroniMember,
		instanceDir,
		dataDir,
		configsDir,
		certificatesDir,
		etcdCreds,
		postgresCerts,
		patroniConfig,
		serviceSpec,
		service,
	}

	if spec.BackupConfig != nil && spec.BackupConfig.Provider == database.BackupProviderPgBackRest {
		orchestratorResources = append(orchestratorResources,
			&PgBackRestConfig{
				InstanceID:   spec.InstanceID,
				HostID:       spec.HostID,
				DatabaseID:   spec.DatabaseID,
				NodeName:     spec.NodeName,
				Repositories: spec.BackupConfig.Repositories,
				ParentID:     configsDir.ID,
				Type:         PgBackRestConfigTypeBackup,
				OwnerUID:     o.cfg.DatabaseOwnerUID,
				OwnerGID:     o.cfg.DatabaseOwnerUID,
			},
			&PgBackRestStanza{
				NodeName: spec.NodeName,
			},
		)
	}
	if spec.RestoreConfig != nil && spec.RestoreConfig.Provider == database.BackupProviderPgBackRest {
		orchestratorResources = append(orchestratorResources, &PgBackRestConfig{
			InstanceID:   spec.InstanceID,
			HostID:       spec.HostID,
			DatabaseID:   spec.RestoreConfig.SourceDatabaseID,
			NodeName:     spec.RestoreConfig.SourceNodeName,
			Repositories: []*pgbackrest.Repository{spec.RestoreConfig.Repository},
			ParentID:     configsDir.ID,
			Type:         PgBackRestConfigTypeRestore,
			OwnerUID:     o.cfg.DatabaseOwnerUID,
			OwnerGID:     o.cfg.DatabaseOwnerUID,
		})
	}

	resources, err := database.NewInstanceResources(instance, orchestratorResources)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance resources: %w", err)
	}

	return resources, nil
}

func (o *Orchestrator) GetInstanceConnectionInfo(ctx context.Context, databaseID, instanceID uuid.UUID) (*database.ConnectionInfo, error) {
	container, err := GetPostgresContainer(ctx, o.docker, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get postgres container: %w", err)
	}
	inspect, err := o.docker.ContainerInspect(ctx, container.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect postgres container: %w", err)
	}
	bridge, ok := inspect.NetworkSettings.Networks["bridge"]
	if !ok {
		return nil, fmt.Errorf("no bridge network found for postgres container %q", container.ID)
	}
	adminHost := bridge.IPAddress
	return &database.ConnectionInfo{
		AdminHost:       adminHost,
		AdminPort:       5432,
		PeerHost:        fmt.Sprintf("%s.%s-database", inspect.Config.Hostname, databaseID),
		PeerPort:        5432,
		PeerSSLCert:     "/opt/pgedge/certificates/postgres/superuser.crt",
		PeerSSLKey:      "/opt/pgedge/certificates/postgres/superuser.key",
		PeerSSLRootCert: "/opt/pgedge/certificates/postgres/ca.crt",
		PatroniPort:     8888,
	}, nil
}

func (o *Orchestrator) WorkerQueues() ([]workflow.Queue, error) {
	queues := []workflow.Queue{
		workflow.Queue(o.cfg.HostID.String()),
		workflow.Queue(o.cfg.ClusterID.String()),
	}
	if o.controlAvailable {
		queues = append(queues, workflow.Queue(o.swarmID))
	}
	return queues, nil
}

func (o *Orchestrator) CreatePgBackRestBackup(ctx context.Context, w io.Writer, instanceID uuid.UUID, options *pgbackrest.BackupOptions) error {
	backupCmd := PgBackRestBackupCmd("backup", options.StringSlice()...)

	err := PostgresContainerExec(ctx, w, o.docker, instanceID, backupCmd.StringSlice())
	if err != nil {
		return fmt.Errorf("failed to exec backup command: %w", err)
	}

	return nil
}
