package swarm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/netip"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
)

var defaultVersion = host.MustPgEdgeVersion("17", "5")

const (
	OverlayDriver = "overlay"
)

// TODO: What should this look like?
var allVersions = []*host.PgEdgeVersion{
	defaultVersion,
	host.MustPgEdgeVersion("16", "5"),
	host.MustPgEdgeVersion("15", "5"),
}

type Orchestrator struct {
	cfg                config.Config
	docker             *docker.Docker
	logger             zerolog.Logger
	dbNetworkAllocator Allocator
	bridgeNetwork      *docker.NetworkInfo
	cpus               int
	memBytes           uint64
	swarmID            string
	swarmNodeID        string
	controlAvailable   bool
}

func NewOrchestrator(
	ctx context.Context,
	cfg config.Config,
	d *docker.Docker,
	logger zerolog.Logger,
) (*Orchestrator, error) {
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

	if info.Swarm.Cluster == nil {
		return nil, fmt.Errorf("docker is not in swarm mode")
	}

	dbNetworkPrefix, err := netip.ParsePrefix(cfg.DockerSwarm.DatabaseNetworksCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database network CIDR: %w", err)
	}

	return &Orchestrator{
		cfg:    cfg,
		docker: d,
		logger: logger,
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
	switch version.PostgresVersion.String() {
	case "17":
		tag = "pgedge:pg17_5.0.0-1"
	case "16":
		tag = "pgedge:pg16_5.0.0-1"
	case "15":
		tag = "pgedge:pg15_5.0.0-1"
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

	instanceHostname := fmt.Sprintf("postgres-%s", spec.InstanceID)

	// If there's more than one instance from the same swarm cluster, each
	// instance will output this same network. They'll get deduplicated when we
	// add them to the state.
	databaseNetwork := &Network{
		CohortID:  o.swarmID,
		Scope:     "swarm",
		Driver:    OverlayDriver,
		Name:      fmt.Sprintf("%s-database", spec.DatabaseID),
		Allocator: o.dbNetworkAllocator,
	}

	// directory resources
	instanceDir := &filesystem.DirResource{
		ID:     spec.InstanceID + "-instance",
		HostID: spec.HostID,
		Path:   filepath.Join(o.cfg.DataDir, "instances", spec.InstanceID),
	}
	dataDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-data",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "data",
		OwnerUID: o.cfg.DatabaseOwnerUID,
		OwnerGID: o.cfg.DatabaseOwnerUID,
	}
	configsDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-configs",
		HostID:   spec.HostID,
		ParentID: instanceDir.ID,
		Path:     "configs",
		OwnerUID: o.cfg.DatabaseOwnerUID,
		OwnerGID: o.cfg.DatabaseOwnerUID,
	}
	certificatesDir := &filesystem.DirResource{
		ID:       spec.InstanceID + "-certificates",
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

	serviceSpec := &PostgresServiceSpecResource{
		Instance:            spec,
		CohortMemberID:      o.swarmNodeID,
		Images:              images,
		ServiceName:         instanceHostname,
		InstanceHostname:    instanceHostname,
		DatabaseNetworkName: databaseNetwork.Name,
		DataDirID:           dataDir.ID,
		ConfigsDirID:        configsDir.ID,
		CertificatesDirID:   certificatesDir.ID,
	}
	service := &PostgresService{
		Instance:    spec,
		CohortID:    o.swarmID,
		ServiceName: instanceHostname,
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

	if spec.BackupConfig != nil {
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
		for _, schedule := range spec.BackupConfig.Schedules {
			orchestratorResources = append(orchestratorResources, scheduler.NewScheduledJobResource(
				fmt.Sprintf("%s-%s-%s", schedule.ID, spec.DatabaseID, spec.NodeName),
				schedule.CronExpression,
				scheduler.WorkflowCreatePgBackRestBackup,
				o.cfg.ClusterID,
				map[string]interface{}{
					"database_id": spec.DatabaseID,
					"node_name":   spec.NodeName,
					"type":        pgbackrest.BackupType(schedule.Type).String(),
				},
				[]resource.Identifier{PgBackRestStanzaIdentifier(spec.NodeName)},
			))
		}
	}

	if spec.RestoreConfig != nil {
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

func (o *Orchestrator) GenerateInstanceRestoreResources(spec *database.InstanceSpec, taskID uuid.UUID) (*database.InstanceResources, error) {
	if spec.RestoreConfig == nil {
		return nil, fmt.Errorf("missing restore config for node %s instance %s", spec.NodeName, spec.InstanceID)
	}

	// TODO: I'd like Patroni to use the backup config that pgbackrest outputs,
	// but it removes the postgresql.auto.conf when it starts bootstrapping the
	// cluster. Setting the restore command ourselves is a decent workaround.
	restoreCmd := PgBackRestRestoreCmd("archive-get", "%f", `"%p"`).String()
	if spec.PostgreSQLConf == nil {
		spec.PostgreSQLConf = map[string]any{
			"restore_command": restoreCmd,
		}
	} else {
		spec.PostgreSQLConf["restore_command"] = restoreCmd
	}

	resources, err := o.GenerateInstanceResources(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	var restoreOptions map[string]string
	if spec.RestoreConfig != nil && spec.RestoreConfig.RestoreOptions != nil {
		restoreOptions = maps.Clone(spec.RestoreConfig.RestoreOptions)
	}
	restoreResource, err := resource.ToResourceData(&PgBackRestRestore{
		DatabaseID:     spec.DatabaseID,
		HostID:         spec.HostID,
		InstanceID:     spec.InstanceID,
		TaskID:         taskID,
		DataDirID:      spec.InstanceID + "-data",
		NodeName:       spec.NodeName,
		RestoreOptions: restoreOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert restore resource to resource data: %w", err)
	}
	resources.Instance.OrchestratorDependencies = append(
		resources.Instance.OrchestratorDependencies,
		restoreResource.Identifier,
	)
	resources.Resources = append(resources.Resources, restoreResource)

	return resources, nil
}

func (o *Orchestrator) GetInstanceConnectionInfo(ctx context.Context, databaseID, instanceID string) (*database.ConnectionInfo, error) {
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
	dbPort, err := nat.NewPort("tcp", "5432")
	if err != nil {
		return nil, fmt.Errorf("failed to construct postgres nat port: %w", err)
	}

	var clientPort int
	// Some configurations won't expose the database port directly, such as
	// those that use a pooler or load balancer in front of Postgres.
	binding, ok := inspect.NetworkSettings.Ports[dbPort]
	if ok && len(binding) > 0 {
		p, err := strconv.Atoi(binding[0].HostPort)
		if err != nil {
			return nil, fmt.Errorf("failed to parse client port %q: %w", binding[0].HostPort, err)
		}
		clientPort = p
	}

	return &database.ConnectionInfo{
		AdminHost:         bridge.IPAddress,
		AdminPort:         5432,
		PeerHost:          fmt.Sprintf("%s.%s-database", inspect.Config.Hostname, databaseID),
		PeerPort:          5432,
		PeerSSLCert:       "/opt/pgedge/certificates/postgres/superuser.crt",
		PeerSSLKey:        "/opt/pgedge/certificates/postgres/superuser.key",
		PeerSSLRootCert:   "/opt/pgedge/certificates/postgres/ca.crt",
		PatroniPort:       8888,
		ClientHost:        o.cfg.Hostname,
		ClientIPv4Address: o.cfg.IPv4Address,
		ClientPort:        clientPort,
		InstanceHostname:  inspect.Config.Hostname,
	}, nil
}

func (o *Orchestrator) WorkerQueues() ([]workflow.Queue, error) {
	queues := []workflow.Queue{
		workflow.Queue(o.cfg.HostID),
		workflow.Queue(o.cfg.ClusterID),
	}
	if o.controlAvailable {
		queues = append(queues, workflow.Queue(o.swarmID))
	}
	return queues, nil
}

func (o *Orchestrator) CreatePgBackRestBackup(ctx context.Context, w io.Writer, instanceID string, options *pgbackrest.BackupOptions) error {
	backupCmd := PgBackRestBackupCmd("backup", options.StringSlice()...)

	err := PostgresContainerExec(ctx, w, o.docker, instanceID, backupCmd.StringSlice())
	if err != nil {
		return fmt.Errorf("failed to exec backup command: %w", err)
	}

	return nil
}

func (o *Orchestrator) ValidateInstanceSpecs(ctx context.Context, specs []*database.InstanceSpec) ([]*database.ValidationResult, error) {
	results := make([]*database.ValidationResult, len(specs))

	occupiedPorts := ds.NewSet[int]()
	for i, instance := range specs {
		result := &database.ValidationResult{
			InstanceID: instance.InstanceID,
			HostID:     instance.HostID,
			NodeName:   instance.NodeName,
			Valid:      true,
		}
		if instance.Port != nil {
			if occupiedPorts.Has(*instance.Port) {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf("port %d allocated to multiple instances on this host", instance.Port),
				)
			}
			occupiedPorts.Add(*instance.Port)
		}
		if err := o.validateInstanceSpec(ctx, instance, result); err != nil {
			return nil, err
		}
		results[i] = result
	}

	return results, nil
}

func (o *Orchestrator) StopInstance(
	ctx context.Context,
	instanceID string,
) error {
	return o.scaleInstance(ctx, instanceID, 0)
}

func (o *Orchestrator) StartInstance(
	ctx context.Context,
	instanceID string,
) error {
	return o.scaleInstance(ctx, instanceID, 1)
}

func (o *Orchestrator) scaleInstance(
	ctx context.Context,
	instanceID string,
	scale uint64,
) error {
	resp, err := o.docker.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":   "postgres",
		"pgedge.instance.id": instanceID,
	})
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return fmt.Errorf("failed to inspect postgres service: %w", err)
	}

	if err := o.docker.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   resp.ID,
		Scale:       scale,
		Wait:        true,
		WaitTimeout: time.Minute,
	}); err != nil {
		return fmt.Errorf("failed to scale up postgres service: %w", err)
	}

	return nil
}

func (o *Orchestrator) validateInstanceSpec(ctx context.Context, spec *database.InstanceSpec, result *database.ValidationResult) error {
	orchestratorOpts := spec.OrchestratorOpts

	// Short-circuit if there's nothing to validate
	if orchestratorOpts == nil || orchestratorOpts.Swarm == nil ||
		(len(orchestratorOpts.Swarm.ExtraVolumes) == 0 &&
			len(orchestratorOpts.Swarm.ExtraNetworks) == 0) {
		return nil
	}

	specVersion := spec.PgEdgeVersion
	if specVersion == nil {
		o.logger.Warn().Msg("PostgresVersion not provided, using default version")
		specVersion = defaultVersion
	}

	images, err := GetImages(o.cfg, specVersion)
	if err != nil {
		return fmt.Errorf("image fetch error: %w", err)
	}
	var endpointConfigs map[string]*network.EndpointSettings
	if len(orchestratorOpts.Swarm.ExtraNetworks) > 0 {
		endpointConfigs, err = ensureNetworks(ctx, o.docker, orchestratorOpts.Swarm.ExtraNetworks)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("network validation failed: %v", err))
			return nil
		}
	}

	var mounts []mount.Mount
	var mountTargets []string
	for _, vol := range orchestratorOpts.Swarm.ExtraVolumes {
		mounts = append(mounts, docker.BuildMount(vol.HostPath, vol.DestinationPath, false))
		mountTargets = append(mountTargets, vol.DestinationPath)
	}

	cmd := buildVolumeCheckCommand(mountTargets)
	output, err := o.runValidationContainer(
		ctx, images.PgEdgeImage,
		cmd, mounts,
		spec.Port,
		endpointConfigs,
	)
	bindMsg := docker.ExtractBindMountErrorMsg(err)
	portMsg := docker.ExtractPortErrorMsg(err)
	networkMsg := docker.ExtractNetworkErrorMsg(err)
	switch {
	case bindMsg != "":
		result.Valid = false
		result.Errors = append(result.Errors, bindMsg)
	case portMsg != "":
		result.Valid = false
		result.Errors = append(result.Errors, portMsg)
	case networkMsg != "":
		result.Valid = false
		result.Errors = append(result.Errors, networkMsg)
	case err != nil:
		return err
	case len(output) > 0:
		result.Valid = false
		result.Errors = append(result.Errors, output)
	}

	return nil
}

func (o *Orchestrator) runValidationContainer(
	ctx context.Context,
	image string,
	cmd []string,
	mounts []mount.Mount,
	port *int,
	endpoints map[string]*network.EndpointSettings,
) (string, error) {
	// Start container
	containerID, err := o.docker.ContainerRun(ctx, validationContainerOpts(image, cmd, mounts, port, endpoints))
	if err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}
	// Ensure container is removed afterward
	defer func() {
		if err := o.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
			o.logger.Error().Err(err).Msg("container cleanup failed")
		}
	}()

	// Wait for the container to complete
	if err := o.docker.ContainerWait(ctx, containerID, container.WaitConditionNotRunning, 30*time.Second); err != nil {
		return "", fmt.Errorf("container wait failed: %w", err)
	}

	// Capture logs
	buf := new(bytes.Buffer)
	if err := o.docker.ContainerLogs(ctx, buf, containerID, container.LogsOptions{ShowStdout: true}); err != nil {
		return "", fmt.Errorf("log fetch failed: %w", err)
	}

	// Stop the container gracefully
	timeoutSeconds := 5
	if err := o.docker.ContainerStop(ctx, containerID, &timeoutSeconds); err != nil {
		o.logger.Warn().Err(err).Msg("graceful stop failed")
	}

	return strings.TrimSpace(buf.String()), nil
}

func validationContainerOpts(
	image string,
	cmd []string,
	mounts []mount.Mount,
	port *int,
	endpoints map[string]*network.EndpointSettings,
) docker.ContainerRunOptions {
	opts := docker.ContainerRunOptions{
		Config: &container.Config{
			Image:      image,
			Entrypoint: cmd,
		},
		Host: &container.HostConfig{
			Mounts:       mounts,
			PortBindings: nat.PortMap{},
		},
	}
	// TODO (PLAT-170): this check prevents users from updating databases that
	// have a port enabled. Commenting this out is a short-term fix.
	// if port != nil && *port > 0 {
	// 	exposedPort := fmt.Sprintf("%d/tcp", *port)
	// 	portSet := nat.PortSet{
	// 		nat.Port(exposedPort): struct{}{},
	// 	}
	// 	portMap := nat.PortMap{
	// 		nat.Port(exposedPort): []nat.PortBinding{
	// 			{
	// 				HostIP:   "0.0.0.0",
	// 				HostPort: strconv.Itoa(*port),
	// 			},
	// 		},
	// 	}
	// 	opts.Config.ExposedPorts = portSet
	// 	opts.Host.PortBindings = portMap
	// }
	if len(endpoints) > 0 {
		opts.Net = &network.NetworkingConfig{
			EndpointsConfig: endpoints,
		}
	}

	return opts
}

const cmdTemplate = `
for d in %s; do
	if [ ! -d "$d" ]; then
		echo "$d is not a directory"
	fi
done
`

func buildVolumeCheckCommand(mountTargets []string) []string {
	if len(mountTargets) < 1 {
		return []string{"true"}
	}

	return []string{"sh", "-c", fmt.Sprintf(cmdTemplate, strings.Join(mountTargets, " "))}
}
func ensureNetworks(
	ctx context.Context,
	docker *docker.Docker,
	networks []database.ExtraNetworkSpec,
) (map[string]*network.EndpointSettings, error) {

	endpointConfigs := make(map[string]*network.EndpointSettings)

	for _, net := range networks {
		// Try to inspect the network
		info, err := docker.NetworkInspect(ctx, net.ID, network.InspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect network %q: %w", net.ID, err)
		}

		if info.Scope != "swarm" {
			return nil, fmt.Errorf("network %q must have scope 'swarm', got '%s'", net.ID, info.Scope)
		}

		if !info.Attachable {
			return nil, fmt.Errorf("network %q is not attachable; must be created with --attachable", net.ID)
		}

		// Register the endpoint config (excluding DriverOpts here, as they're not used at this stage)
		endpointConfigs[net.ID] = &network.EndpointSettings{
			Aliases: net.Aliases,
		}
	}

	return endpointConfigs, nil
}
