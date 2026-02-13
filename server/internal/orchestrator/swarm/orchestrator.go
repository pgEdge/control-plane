package swarm

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/netip"
	"path/filepath"
	"slices"
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
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const (
	OverlayDriver = "overlay"
)

type Orchestrator struct {
	cfg                config.Config
	versions           *Versions
	serviceVersions    *ServiceVersions
	docker             *docker.Docker
	logger             zerolog.Logger
	dbNetworkAllocator Allocator
	bridgeNetwork      *docker.NetworkInfo
	cpus               int
	memBytes           uint64
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

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return nil, fmt.Errorf("docker swarm mode is not active - current state: %s", info.Swarm.LocalNodeState)
	}

	dbNetworkPrefix, err := netip.ParsePrefix(cfg.DockerSwarm.DatabaseNetworksCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database network CIDR: %w", err)
	}

	return &Orchestrator{
		cfg:             cfg,
		versions:        NewVersions(cfg),
		serviceVersions: NewServiceVersions(cfg),
		docker:          d,
		logger:          logger,
		dbNetworkAllocator: Allocator{
			Prefix: dbNetworkPrefix,
			Bits:   cfg.DockerSwarm.DatabaseNetworksSubnetBits,
		},
		bridgeNetwork:    bridgeInfo,
		cpus:             info.NCPU,
		memBytes:         uint64(info.MemTotal),
		swarmNodeID:      info.Swarm.NodeID,
		controlAvailable: info.Swarm.ControlAvailable,
	}, nil
}

func (o *Orchestrator) PopulateHost(ctx context.Context, h *host.Host) error {
	h.CPUs = o.cpus
	h.MemBytes = o.memBytes
	h.Cohort = &host.Cohort{
		Type:             host.CohortTypeSwarm,
		MemberID:         o.swarmNodeID,
		ControlAvailable: o.controlAvailable,
	}
	h.DefaultPgEdgeVersion = o.versions.Default()
	h.SupportedPgEdgeVersions = o.versions.Supported()

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

func (o *Orchestrator) GenerateInstanceResources(spec *database.InstanceSpec) (*database.InstanceResources, error) {
	instance, orchestratorResources, err := o.instanceResources(spec)
	if err != nil {
		return nil, err
	}

	resources, err := database.NewInstanceResources(instance, orchestratorResources)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance resources: %w", err)
	}
	return resources, nil
}

// ServiceInstanceName generates a Docker Swarm service name for a service instance.
// It follows the same host ID hashing convention used for Postgres instance IDs
// (see database.InstanceIDFor), producing shorter, more readable names when host
// IDs are UUIDs.
func ServiceInstanceName(serviceType, databaseID, serviceID, hostID string) string {
	hash := sha1.Sum([]byte(hostID))
	base36 := new(big.Int).SetBytes(hash[:]).Text(36)
	return fmt.Sprintf("%s-%s-%s-%s", serviceType, databaseID, serviceID, base36[:8])
}

func (o *Orchestrator) instanceResources(spec *database.InstanceSpec) (*database.InstanceResource, []resource.Resource, error) {
	images, err := o.versions.GetImages(spec.PgEdgeVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get images: %w", err)
	}

	instanceHostname := fmt.Sprintf("postgres-%s", spec.InstanceID)

	// If there's more than one instance from the same swarm cluster, each
	// instance will output this same network. They'll get deduplicated when we
	// add them to the state.
	databaseNetwork := &Network{
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
		DatabaseID:           spec.DatabaseID,
		NodeName:             spec.NodeName,
		PatroniClusterPrefix: patroni.ClusterPrefix(spec.DatabaseID, spec.NodeName),
	}
	patroniMember := &PatroniMember{
		DatabaseID: spec.DatabaseID,
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
	checkWillRestart := &CheckWillRestart{
		InstanceID: spec.InstanceID,
	}
	switchover := &Switchover{
		HostID:     spec.HostID,
		InstanceID: spec.InstanceID,
	}
	service := &PostgresService{
		InstanceID:  spec.InstanceID,
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
		checkWillRestart,
		switchover,
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

	return instance, orchestratorResources, nil
}

func (o *Orchestrator) GenerateInstanceRestoreResources(spec *database.InstanceSpec, taskID uuid.UUID) (*database.InstanceResources, error) {
	if spec.RestoreConfig == nil {
		return nil, fmt.Errorf("missing restore config for node %s instance %s", spec.NodeName, spec.InstanceID)
	}

	spec.InPlaceRestore = true

	instance, resources, err := o.instanceResources(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	resources = append(resources,
		&ScaleService{
			InstanceID:     spec.InstanceID,
			ScaleDirection: ScaleDirectionDOWN,
		},
		&PgBackRestRestore{
			DatabaseID:     spec.DatabaseID,
			HostID:         spec.HostID,
			InstanceID:     spec.InstanceID,
			TaskID:         taskID,
			DataDirID:      spec.InstanceID + "-data",
			NodeName:       spec.NodeName,
			RestoreOptions: spec.RestoreConfig.RestoreOptions,
		},
		&ScaleService{
			InstanceID:     spec.InstanceID,
			ScaleDirection: ScaleDirectionUP,
			Deps:           []resource.Identifier{PgBackRestRestoreResourceIdentifier(spec.InstanceID)},
		},
	)

	instance.OrchestratorDependencies = append(instance.OrchestratorDependencies, ScaleServiceResourceIdentifier(spec.InstanceID, ScaleDirectionUP))

	instanceResources, err := database.NewInstanceResources(instance, resources)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize instance resources: %w", err)
	}
	return instanceResources, nil
}

func (o *Orchestrator) GenerateServiceInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	// Get service image based on service type and version
	serviceImage, err := o.serviceVersions.GetServiceImage(spec.ServiceSpec.ServiceType, spec.ServiceSpec.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get service image: %w", err)
	}

	// Validate compatibility with database version
	if spec.PgEdgeVersion != nil {
		if err := serviceImage.ValidateCompatibility(
			spec.PgEdgeVersion.PostgresVersion,
			spec.PgEdgeVersion.SpockVersion,
		); err != nil {
			return nil, fmt.Errorf("service %q version %q is not compatible with this database: %w",
				spec.ServiceSpec.ServiceType, spec.ServiceSpec.Version, err)
		}
	}

	// Database network (shared with postgres instances)
	databaseNetwork := &Network{
		Scope:     "swarm",
		Driver:    OverlayDriver,
		Name:      fmt.Sprintf("%s-database", spec.DatabaseID),
		Allocator: o.dbNetworkAllocator,
	}

	// Service user role resource (manages database user lifecycle)
	serviceUserRole := &ServiceUserRole{
		ServiceInstanceID: spec.ServiceInstanceID,
		DatabaseID:        spec.DatabaseID,
		DatabaseName:      spec.DatabaseName,
		Username:          spec.Credentials.Username,
		HostID:            spec.HostID,
	}

	// Service instance spec resource
	serviceName := ServiceInstanceName(spec.ServiceSpec.ServiceType, spec.DatabaseID, spec.ServiceSpec.ServiceID, spec.HostID)
	serviceInstanceSpec := &ServiceInstanceSpecResource{
		ServiceInstanceID: spec.ServiceInstanceID,
		ServiceSpec:       spec.ServiceSpec,
		DatabaseID:        spec.DatabaseID,
		DatabaseName:      spec.DatabaseName,
		HostID:            spec.HostID,
		ServiceName:       serviceName,
		Hostname:          serviceName,
		CohortMemberID:    o.swarmNodeID, // Use orchestrator's swarm node ID (same as Postgres instances)
		ServiceImage:      serviceImage,
		Credentials:       spec.Credentials,
		DatabaseNetworkID: databaseNetwork.Name,
		DatabaseHost:      spec.DatabaseHost,
		DatabasePort:      spec.DatabasePort,
		Port:              spec.Port,
	}

	// Service instance resource (actual Docker service)
	serviceInstance := &ServiceInstanceResource{
		ServiceInstanceID: spec.ServiceInstanceID,
		DatabaseID:        spec.DatabaseID,
		ServiceName:       serviceName,
	}

	orchestratorResources := []resource.Resource{
		databaseNetwork,
		serviceUserRole,
		serviceInstanceSpec,
		serviceInstance,
	}

	// Convert to resource data
	data := make([]*resource.ResourceData, len(orchestratorResources))
	for i, res := range orchestratorResources {
		d, err := resource.ToResourceData(res)
		if err != nil {
			return nil, fmt.Errorf("failed to convert resource to resource data: %w", err)
		}
		data[i] = d
	}

	return &database.ServiceInstanceResources{
		ServiceInstance: &database.ServiceInstance{
			ServiceInstanceID: spec.ServiceInstanceID,
			ServiceID:         spec.ServiceSpec.ServiceID,
			DatabaseID:        spec.DatabaseID,
			HostID:            spec.HostID,
			State:             database.ServiceInstanceStateCreating,
		},
		Resources: data,
	}, nil
}

func (o *Orchestrator) GetInstanceConnectionInfo(ctx context.Context, databaseID, instanceID string) (*database.ConnectionInfo, error) {
	container, err := GetPostgresContainer(ctx, o.docker, instanceID)
	if err != nil {
		if errors.Is(err, ErrNoPostgresContainer) {
			return nil, fmt.Errorf("%w: %v", database.ErrInstanceStopped, err)
		}
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

func (o *Orchestrator) GetServiceInstanceStatus(ctx context.Context, serviceInstanceID string) (*database.ServiceInstanceStatus, error) {
	// Get the service container to retrieve network info and ports
	// This activity is routed to the specific host where the service is constrained
	container, err := GetServiceContainer(ctx, o.docker, serviceInstanceID)
	if err != nil {
		if errors.Is(err, ErrNoServiceContainer) {
			// Container not found - service is still starting up
			// Return nil status (not an error) so the workflow can continue
			// The status will be populated later by monitoring or retry
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get service container: %w", err)
	}

	// Inspect the container to get network information and ports
	inspect, err := o.docker.ContainerInspect(ctx, container.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect service container: %w", err)
	}

	// Extract ports from container port bindings
	var ports []database.PortMapping
	for portStr, bindings := range inspect.NetworkSettings.Ports {
		// Skip if no bindings (unpublished ports)
		if len(bindings) == 0 {
			continue
		}

		// Parse container port
		containerPort, err := strconv.Atoi(portStr.Port())
		if err != nil {
			continue // Skip malformed port
		}

		// Get host port from first binding
		hostPort, err := strconv.Atoi(bindings[0].HostPort)
		if err != nil {
			continue // Skip malformed port
		}

		ports = append(ports, database.PortMapping{
			Name:          portStr.Proto(),
			ContainerPort: containerPort,
			HostPort:      utils.PointerTo(hostPort),
		})
	}

	// Determine readiness from container state and health info
	ready := inspect.State != nil && inspect.State.Running
	if ready && inspect.State.Health != nil {
		ready = inspect.State.Health.Status == "healthy"
	}

	return &database.ServiceInstanceStatus{
		ContainerID:  utils.PointerTo(inspect.ID),
		ImageVersion: utils.PointerTo(inspect.Config.Image),
		Hostname:     utils.PointerTo(inspect.Config.Hostname),
		IPv4Address:  utils.PointerTo(o.cfg.IPv4Address),
		Ports:        ports,
		ServiceReady: utils.PointerTo(ready),
	}, nil
}

func (o *Orchestrator) WorkerQueues() ([]workflow.Queue, error) {
	queues := []workflow.Queue{
		utils.AnyQueue(),
		utils.HostQueue(o.cfg.HostID),
	}
	if o.controlAvailable {
		queues = append(queues, utils.ManagerQueue())
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

func (o *Orchestrator) ValidateInstanceSpecs(ctx context.Context, changes []*database.InstanceSpecChange) ([]*database.ValidationResult, error) {
	results := make([]*database.ValidationResult, 0, len(changes)*3)

	for _, ch := range changes {
		updates := instanceDiff(ch.Previous, ch.Current)

		if updates.NewPort == nil && len(updates.NewVolumes) == 0 && len(updates.NewNetworks) == 0 {
			continue
		}

		updateResult := func(kind string, err error) {
			if err != nil {
				results = append(results, &database.ValidationResult{
					Valid:    false,
					NodeName: ch.Current.NodeName,
					HostID:   ch.Current.HostID,
					Errors:   []string{fmt.Sprintf("%s: %v", kind, err)},
				})
			} else {
				results = append(results, &database.ValidationResult{
					Valid:    true,
					NodeName: ch.Current.NodeName,
					HostID:   ch.Current.HostID,
				})
			}
		}

		// If more than one kind of update, run the single combined validation.
		changedKinds := 0
		if updates.NewPort != nil && *updates.NewPort > 0 {
			changedKinds++
		}
		if len(updates.NewVolumes) > 0 {
			changedKinds++
		}
		if len(updates.NewNetworks) > 0 {
			changedKinds++
		}

		if changedKinds > 1 {
			res := &database.ValidationResult{
				Valid:    true,
				NodeName: ch.Current.NodeName,
				HostID:   ch.Current.HostID,
			}
			if err := o.validateInstanceSpec(ctx, ch.Current, res); err != nil {
				updateResult("combined validation error", err)
				continue
			}
			results = append(results, res)
			continue
		}

		// Otherwise, targeted validations.
		if updates.NewPort != nil && *updates.NewPort > 0 {
			updateResult("port", o.validatePortAvailable(ctx, ch.Current.NodeName, *updates.NewPort))
		}
		if len(updates.NewVolumes) > 0 {
			updateResult("volumes", o.validateVolumes(ctx, ch.Current.NodeName, updates.NewVolumes))
		}
		if len(updates.NewNetworks) > 0 {
			updateResult("networks", o.validateNetworks(ctx, ch.Current.NodeName, updates.NewNetworks))
		}
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
	if err != nil {
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

	var endpointConfigs map[string]*network.EndpointSettings
	if orchestratorOpts != nil && orchestratorOpts.Swarm != nil && len(orchestratorOpts.Swarm.ExtraNetworks) > 0 {
		info, err := ensureNetworks(ctx, o.docker, orchestratorOpts.Swarm.ExtraNetworks)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("network validation failed: %v", err))
			return nil
		}
		endpointConfigs = info
	}

	var mounts []mount.Mount
	var mountTargets []string
	if orchestratorOpts != nil && orchestratorOpts.Swarm != nil && len(orchestratorOpts.Swarm.ExtraVolumes) > 0 {
		for _, vol := range orchestratorOpts.Swarm.ExtraVolumes {
			mounts = append(mounts, docker.BuildMount(vol.HostPath, vol.DestinationPath, false))
			mountTargets = append(mountTargets, vol.DestinationPath)
		}
	}

	// If there are NO networks, NO mounts, and port is nil/0 → nothing to validate
	if len(mounts) == 0 && len(endpointConfigs) == 0 && (spec.Port == nil || *spec.Port == 0) {
		return nil
	}

	specVersion := spec.PgEdgeVersion
	if specVersion == nil {
		o.logger.Warn().Msg("PostgresVersion not provided, using default version")
		specVersion = o.versions.defaultVersion
	}

	images, err := o.versions.GetImages(specVersion)
	if err != nil {
		return fmt.Errorf("image fetch error: %w", err)
	}

	cmd := buildVolumeCheckCommand(mountTargets)
	output, err := o.runValidationContainer(
		ctx,
		images.PgEdgeImage,
		cmd,
		mounts,
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

func (o *Orchestrator) validatePortAvailable(ctx context.Context, nodeName string, port int) error {
	if port <= 0 {
		return nil
	}

	specVersion := o.versions.defaultVersion
	images, err := o.versions.GetImages(specVersion)
	if err != nil {
		return fmt.Errorf("image fetch error: %w", err)
	}

	cmd := []string{"sh", "-c", "true"}
	_, runErr := o.runValidationContainer(ctx, images.PgEdgeImage, cmd, nil, &port, nil)

	if msg := docker.ExtractPortErrorMsg(runErr); msg != "" {
		return errors.New(msg)
	}
	return runErr
}

func (o *Orchestrator) validateVolumes(ctx context.Context, nodeName string, vols []database.ExtraVolumesSpec) error {
	if len(vols) == 0 {
		return nil
	}

	var mounts []mount.Mount
	var targets []string
	for _, v := range vols {
		mounts = append(mounts, docker.BuildMount(v.HostPath, v.DestinationPath, false))
		targets = append(targets, v.DestinationPath)
	}

	specVersion := o.versions.defaultVersion
	images, err := o.versions.GetImages(specVersion)
	if err != nil {
		return fmt.Errorf("image fetch error: %w", err)
	}

	cmd := buildVolumeCheckCommand(targets)
	out, runErr := o.runValidationContainer(ctx, images.PgEdgeImage, cmd, mounts, nil, nil)

	if msg := docker.ExtractBindMountErrorMsg(runErr); msg != "" {
		return errors.New(msg)
	}
	if runErr == nil && strings.TrimSpace(out) != "" {
		return errors.New(strings.TrimSpace(out))
	}
	return runErr
}

func (o *Orchestrator) validateNetworks(ctx context.Context, nodeName string, nets []database.ExtraNetworkSpec) error {
	if len(nets) == 0 {
		return nil
	}
	_, err := ensureNetworks(ctx, o.docker, nets)
	if msg := docker.ExtractNetworkErrorMsg(err); msg != "" {
		return errors.New(msg)
	}
	return err
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

	// Bind only when a non-zero port is explicitly requested.
	if port != nil && *port > 0 {
		exposed := nat.Port(fmt.Sprintf("%d/tcp", *port))
		opts.Config.ExposedPorts = nat.PortSet{exposed: struct{}{}}
		opts.Host.PortBindings = nat.PortMap{
			exposed: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: strconv.Itoa(*port),
				},
			},
		}
	}

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
	dockerCli *docker.Docker,
	networks []database.ExtraNetworkSpec,
) (map[string]*network.EndpointSettings, error) {

	endpointConfigs := make(map[string]*network.EndpointSettings)

	for _, net := range networks {
		// Try to inspect the network
		info, err := dockerCli.NetworkInspect(ctx, net.ID, network.InspectOptions{})
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

type updatedInstanceFields struct {
	NewPort     *int
	NewVolumes  []database.ExtraVolumesSpec
	NewNetworks []database.ExtraNetworkSpec
}

func instanceDiff(prev, cur *database.InstanceSpec) updatedInstanceFields {
	var out updatedInstanceFields
	if cur == nil {
		return out
	}

	if prev == nil {
		// Full validation on first create
		if cur.Port != nil && *cur.Port != 0 {
			out.NewPort = cur.Port
		}
		if cur.OrchestratorOpts != nil && cur.OrchestratorOpts.Swarm != nil {
			out.NewVolumes = append(out.NewVolumes, cur.OrchestratorOpts.Swarm.ExtraVolumes...)
			out.NewNetworks = append(out.NewNetworks, cur.OrchestratorOpts.Swarm.ExtraNetworks...)
		}
		return out
	}

	// Port: validate only if non-zero and changed
	if (prev.Port == nil && cur.Port != nil && *cur.Port != 0) ||
		(prev.Port != nil && cur.Port != nil && *prev.Port != *cur.Port && *cur.Port != 0) {
		out.NewPort = cur.Port
	}
	// Volumes: only new volumes
	var prevVols, curVols []database.ExtraVolumesSpec
	if prev.OrchestratorOpts != nil && prev.OrchestratorOpts.Swarm != nil {
		prevVols = prev.OrchestratorOpts.Swarm.ExtraVolumes
	}
	if cur.OrchestratorOpts != nil && cur.OrchestratorOpts.Swarm != nil {
		curVols = cur.OrchestratorOpts.Swarm.ExtraVolumes
	}
	out.NewVolumes = diffVolumes(prevVols, curVols)

	// Networks: only new networks
	var prevNets, curNets []database.ExtraNetworkSpec
	if prev.OrchestratorOpts != nil && prev.OrchestratorOpts.Swarm != nil {
		prevNets = prev.OrchestratorOpts.Swarm.ExtraNetworks
	}
	if cur.OrchestratorOpts != nil && cur.OrchestratorOpts.Swarm != nil {
		curNets = cur.OrchestratorOpts.Swarm.ExtraNetworks
	}
	out.NewNetworks = diffNetworks(prevNets, curNets)

	return out
}

func diffVolumes(oldV, newV []database.ExtraVolumesSpec) []database.ExtraVolumesSpec {
	var out []database.ExtraVolumesSpec
	for _, v := range newV {
		if !containsVolume(oldV, v) {
			out = append(out, v)
		}
	}
	return out
}

func diffNetworks(oldN, newN []database.ExtraNetworkSpec) []database.ExtraNetworkSpec {
	var out []database.ExtraNetworkSpec
	for _, n := range newN {
		if !containsNetwork(oldN, n) {
			out = append(out, n)
		}
	}
	return out
}

func containsVolume(set []database.ExtraVolumesSpec, v database.ExtraVolumesSpec) bool {
	return slices.ContainsFunc(set, func(x database.ExtraVolumesSpec) bool {
		return x.HostPath == v.HostPath && x.DestinationPath == v.DestinationPath
	})
}

func containsNetwork(set []database.ExtraNetworkSpec, n database.ExtraNetworkSpec) bool {
	return slices.ContainsFunc(set, func(x database.ExtraNetworkSpec) bool {
		if x.ID != n.ID {
			return false
		}
		if !slices.Equal(x.Aliases, n.Aliases) {
			return false
		}
		if !maps.Equal(x.DriverOpts, n.DriverOpts) {
			return false
		}
		return true
	})
}
