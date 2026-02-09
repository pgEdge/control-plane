package apiv1

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func hostToAPI(h *host.Host) *api.Host {
	components := make(map[string]*api.ComponentStatus, len(h.Status.Components))
	for name, status := range h.Status.Components {
		components[name] = &api.ComponentStatus{
			Healthy: status.Healthy,
			Error:   utils.PointerTo(status.Error),
			Details: status.Details,
		}
	}
	var cohort *api.HostCohort
	if h.Cohort != nil {
		cohort = &api.HostCohort{
			Type:             string(h.Cohort.Type),
			MemberID:         h.Cohort.MemberID,
			ControlAvailable: h.Cohort.ControlAvailable,
		}
	}
	supportedVersions := make([]*api.PgEdgeVersion, len(h.SupportedPgEdgeVersions))
	for i, v := range h.SupportedPgEdgeVersions {
		supportedVersions[i] = &api.PgEdgeVersion{
			PostgresVersion: v.PostgresVersion.String(),
			SpockVersion:    v.SpockVersion.String(),
		}
	}
	return &api.Host{
		Orchestrator: string(h.Orchestrator),
		DataDir:      h.DataDir,
		Hostname:     h.Hostname,
		Ipv4Address:  h.IPv4Address,
		Cpus:         utils.NillablePointerTo(h.CPUs),
		Memory:       utils.NillablePointerTo(humanizeBytes(h.MemBytes)),
		Cohort:       cohort,
		ID:           api.Identifier(h.ID),
		EtcdMode:     utils.NillablePointerTo(string(h.EtcdMode)),
		DefaultPgedgeVersion: &api.PgEdgeVersion{
			PostgresVersion: h.DefaultPgEdgeVersion.PostgresVersion.String(),
			SpockVersion:    h.DefaultPgEdgeVersion.SpockVersion.String(),
		},
		SupportedPgedgeVersions: supportedVersions,
		Status: &api.HostStatus{
			State:      string(h.Status.State),
			Components: components,
			UpdatedAt:  h.Status.UpdatedAt.Format(time.RFC3339),
		},
	}
}

func databaseNodesToAPI(nodes []*database.Node) []*api.DatabaseNodeSpec {
	apiNodes := make([]*api.DatabaseNodeSpec, len(nodes))
	for i, node := range nodes {
		hostIDs := make([]api.Identifier, len(node.HostIDs))
		for j, hostID := range node.HostIDs {
			hostIDs[j] = api.Identifier(hostID)
		}
		apiNodes[i] = &api.DatabaseNodeSpec{
			Name:             node.Name,
			HostIds:          hostIDs,
			PostgresVersion:  utils.NillablePointerTo(node.PostgresVersion),
			Port:             node.Port,
			Cpus:             utils.NillablePointerTo(humanizeCPUs(node.CPUs)),
			Memory:           utils.NillablePointerTo(humanizeBytes(node.MemoryBytes)),
			PostgresqlConf:   node.PostgreSQLConf,
			BackupConfig:     backupConfigToAPI(node.BackupConfig),
			RestoreConfig:    restoreConfigToAPI(node.RestoreConfig),
			OrchestratorOpts: orchestratorOptsToAPI(node.OrchestratorOpts),
			SourceNode:       utils.NillablePointerTo(node.SourceNode),
		}
	}
	return apiNodes
}

func databaseUsersToAPI(users []*database.User) []*api.DatabaseUserSpec {
	apiUsers := make([]*api.DatabaseUserSpec, len(users))
	for i, user := range users {
		// Password is intentionally excluded because it's a sensitive field.
		apiUsers[i] = &api.DatabaseUserSpec{
			Username:   user.Username,
			DbOwner:    &user.DBOwner,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		}
	}
	return apiUsers
}

func backupConfigToAPI(config *database.BackupConfig) *api.BackupConfigSpec {
	if config == nil {
		return nil
	}
	repositories := make([]*api.BackupRepositorySpec, len(config.Repositories))
	for i, repo := range config.Repositories {
		var id *api.Identifier
		if repo.ID != "" {
			id = utils.PointerTo(api.Identifier(repo.ID))
		}
		// We intentionally exclude credential fields because they're sensitive.
		repositories[i] = &api.BackupRepositorySpec{
			ID:                id,
			Type:              string(repo.Type),
			S3Bucket:          utils.NillablePointerTo(repo.S3Bucket),
			S3Region:          utils.NillablePointerTo(repo.S3Region),
			S3Endpoint:        utils.NillablePointerTo(repo.S3Endpoint),
			GcsBucket:         utils.NillablePointerTo(repo.GCSBucket),
			GcsEndpoint:       utils.NillablePointerTo(repo.GCSEndpoint),
			AzureAccount:      utils.NillablePointerTo(repo.AzureAccount),
			AzureContainer:    utils.NillablePointerTo(repo.AzureContainer),
			AzureEndpoint:     utils.NillablePointerTo(repo.AzureEndpoint),
			RetentionFull:     utils.NillablePointerTo(repo.RetentionFull),
			RetentionFullType: utils.NillablePointerTo(string(repo.RetentionFullType)),
			BasePath:          utils.NillablePointerTo(repo.BasePath),
			CustomOptions:     repo.CustomOptions,
		}
	}
	schedules := make([]*api.BackupScheduleSpec, len(config.Schedules))
	for i, schedule := range config.Schedules {
		schedules[i] = &api.BackupScheduleSpec{
			ID:             schedule.ID,
			Type:           string(schedule.Type),
			CronExpression: schedule.CronExpression,
		}
	}

	return &api.BackupConfigSpec{
		Repositories: repositories,
		Schedules:    schedules,
	}
}

func restoreConfigToAPI(config *database.RestoreConfig) *api.RestoreConfigSpec {
	if config == nil {
		return nil
	}
	out := &api.RestoreConfigSpec{
		SourceDatabaseID:   api.Identifier(config.SourceDatabaseID),
		SourceNodeName:     config.SourceNodeName,
		SourceDatabaseName: config.SourceDatabaseName,
		RestoreOptions:     config.RestoreOptions,
	}
	if config.Repository != nil {
		var id *api.Identifier
		if config.Repository.ID != "" {
			id = utils.PointerTo(api.Identifier(config.Repository.ID))
		}
		// We intentionally exclude credential fields because they're sensitive.
		out.Repository = &api.RestoreRepositorySpec{
			ID:             id,
			Type:           string(config.Repository.Type),
			S3Bucket:       utils.NillablePointerTo(config.Repository.S3Bucket),
			S3Region:       utils.NillablePointerTo(config.Repository.S3Region),
			S3Endpoint:     utils.NillablePointerTo(config.Repository.S3Endpoint),
			GcsBucket:      utils.NillablePointerTo(config.Repository.GCSBucket),
			GcsEndpoint:    utils.NillablePointerTo(config.Repository.GCSEndpoint),
			AzureAccount:   utils.NillablePointerTo(config.Repository.AzureAccount),
			AzureContainer: utils.NillablePointerTo(config.Repository.AzureContainer),
			AzureEndpoint:  utils.NillablePointerTo(config.Repository.AzureEndpoint),
			BasePath:       utils.NillablePointerTo(config.Repository.BasePath),
			CustomOptions:  config.Repository.CustomOptions,
		}
	}
	return out
}

func serviceSpecToAPI(svc *database.ServiceSpec) *api.ServiceSpec {
	if svc == nil {
		return nil
	}

	hostIDs := make([]api.Identifier, len(svc.HostIDs))
	for i, hostID := range svc.HostIDs {
		hostIDs[i] = api.Identifier(hostID)
	}

	// Strip sensitive keys from config before returning to API
	var filteredConfig map[string]any
	if svc.Config != nil {
		filteredConfig = make(map[string]any, len(svc.Config))
		for k, v := range svc.Config {
			kLower := strings.ToLower(k)
			if strings.Contains(kLower, "api_key") || strings.Contains(kLower, "secret") || strings.Contains(kLower, "password") {
				continue
			}
			filteredConfig[k] = v
		}
	}

	return &api.ServiceSpec{
		ServiceID:   api.Identifier(svc.ServiceID),
		ServiceType: svc.ServiceType,
		Version:     svc.Version,
		HostIds:     hostIDs,
		Port:        svc.Port,
		Config:      filteredConfig,
		Cpus:        utils.NillablePointerTo(humanizeCPUs(utils.FromPointer(svc.CPUs))),
		Memory:      utils.NillablePointerTo(humanizeBytes(utils.FromPointer(svc.MemoryBytes))),
	}
}

func serviceSpecsToAPI(services []*database.ServiceSpec) []*api.ServiceSpec {
	if len(services) == 0 {
		return nil
	}

	apiServices := make([]*api.ServiceSpec, len(services))
	for i, svc := range services {
		apiServices[i] = serviceSpecToAPI(svc)
	}
	return apiServices
}

func databaseSpecToAPI(d *database.Spec) *api.DatabaseSpec {
	return &api.DatabaseSpec{
		DatabaseName:     d.DatabaseName,
		PostgresVersion:  utils.NillablePointerTo(d.PostgresVersion),
		SpockVersion:     utils.NillablePointerTo(d.SpockVersion),
		Port:             d.Port,
		Cpus:             utils.NillablePointerTo(humanizeCPUs(d.CPUs)),
		Memory:           utils.NillablePointerTo(humanizeBytes(d.MemoryBytes)),
		Nodes:            databaseNodesToAPI(d.Nodes),
		DatabaseUsers:    databaseUsersToAPI(d.DatabaseUsers),
		Services:         serviceSpecsToAPI(d.Services),
		BackupConfig:     backupConfigToAPI(d.BackupConfig),
		RestoreConfig:    restoreConfigToAPI(d.RestoreConfig),
		PostgresqlConf:   d.PostgreSQLConf,
		OrchestratorOpts: orchestratorOptsToAPI(d.OrchestratorOpts),
	}
}

func portMappingToAPI(pm database.PortMapping) *api.PortMapping {
	return &api.PortMapping{
		Name:          pm.Name,
		ContainerPort: pm.ContainerPort,
		HostPort:      pm.HostPort,
	}
}

func healthCheckResultToAPI(hc *database.HealthCheckResult) *api.HealthCheckResult {
	if hc == nil {
		return nil
	}
	return &api.HealthCheckResult{
		Status:    hc.Status,
		Message:   utils.NillablePointerTo(hc.Message),
		CheckedAt: hc.CheckedAt.Format(time.RFC3339),
	}
}

func serviceInstanceStatusToAPI(status *database.ServiceInstanceStatus) *api.ServiceInstanceStatus {
	if status == nil {
		return nil
	}

	ports := make([]*api.PortMapping, len(status.Ports))
	for i, pm := range status.Ports {
		ports[i] = portMappingToAPI(pm)
	}

	var lastHealthAt *string
	if status.LastHealthAt != nil {
		lastHealthAt = utils.PointerTo(status.LastHealthAt.Format(time.RFC3339))
	}

	return &api.ServiceInstanceStatus{
		ContainerID:  status.ContainerID,
		ImageVersion: status.ImageVersion,
		Hostname:     status.Hostname,
		Ipv4Address:  status.IPv4Address,
		Ports:        ports,
		HealthCheck:  healthCheckResultToAPI(status.HealthCheck),
		LastHealthAt: lastHealthAt,
		ServiceReady: status.ServiceReady,
	}
}

func serviceInstanceToAPI(si *database.ServiceInstance) *api.Serviceinstance {
	if si == nil {
		return nil
	}

	return &api.Serviceinstance{
		ServiceInstanceID: si.ServiceInstanceID,
		ServiceID:         si.ServiceID,
		DatabaseID:        api.Identifier(si.DatabaseID),
		HostID:            si.HostID,
		State:             string(si.State),
		Status:            serviceInstanceStatusToAPI(si.Status),
		CreatedAt:         si.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         si.UpdatedAt.Format(time.RFC3339),
		Error:             utils.NillablePointerTo(si.Error),
	}
}

func databaseToAPI(d *database.Database) *api.Database {
	if d == nil {
		return nil
	}

	var spec *api.DatabaseSpec
	if d.Spec != nil {
		spec = databaseSpecToAPI(d.Spec)
	}

	instances := make([]*api.Instance, len(d.Instances))
	for i, inst := range d.Instances {
		instances[i] = instanceToAPI(inst)
	}
	// Sort by node ID, instance ID asc
	slices.SortStableFunc(instances, func(a, b *api.Instance) int {
		if nodeEq := strings.Compare(a.NodeName, b.NodeName); nodeEq != 0 {
			return nodeEq
		}
		return strings.Compare(a.ID, b.ID)
	})

	serviceInstances := make([]*api.Serviceinstance, len(d.ServiceInstances))
	for i, si := range d.ServiceInstances {
		serviceInstances[i] = serviceInstanceToAPI(si)
	}
	// Sort by service ID, host ID asc
	slices.SortStableFunc(serviceInstances, func(a, b *api.Serviceinstance) int {
		if svcEq := strings.Compare(a.ServiceID, b.ServiceID); svcEq != 0 {
			return svcEq
		}
		return strings.Compare(a.HostID, b.HostID)
	})

	var tenantID *api.Identifier
	if d.TenantID != nil {
		tenantID = utils.PointerTo(api.Identifier(*d.TenantID))
	}

	return &api.Database{
		ID:               api.Identifier(d.DatabaseID),
		TenantID:         tenantID,
		CreatedAt:        d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        d.UpdatedAt.Format(time.RFC3339),
		State:            string(d.State),
		Spec:             spec,
		Instances:        instances,
		ServiceInstances: serviceInstances,
	}
}

func instanceConnectionInfoToAPI(status *database.InstanceStatus) *api.InstanceConnectionInfo {
	if status == nil || status.Port == nil || *status.Port == 0 {
		return nil
	}
	return &api.InstanceConnectionInfo{
		Hostname:    status.Hostname,
		Ipv4Address: status.IPv4Address,
		Port:        status.Port,
	}
}

func instancePostgresStatusToAPI(status *database.InstanceStatus) *api.InstancePostgresStatus {
	if status == nil {
		return nil
	}
	return &api.InstancePostgresStatus{
		Version:        status.PostgresVersion,
		PatroniState:   stringifyStringerPtr(status.PatroniState),
		Role:           stringifyStringerPtr(status.Role),
		PendingRestart: status.PendingRestart,
		PatroniPaused:  status.PatroniPaused,
	}
}

func instanceSpockStatusToAPI(status *database.InstanceStatus) *api.InstanceSpockStatus {
	if status == nil || !status.IsPrimary() {
		return nil
	}
	subs := make([]*api.InstanceSubscription, len(status.Subscriptions))
	for i, sub := range status.Subscriptions {
		subs[i] = &api.InstanceSubscription{
			ProviderNode: sub.ProviderNode,
			Name:         sub.Name,
			Status:       sub.Status,
		}
	}
	return &api.InstanceSpockStatus{
		Version:       status.SpockVersion,
		Subscriptions: subs,
		ReadOnly:      status.ReadOnly,
	}
}

func instanceToAPI(instance *database.Instance) *api.Instance {
	if instance == nil {
		return nil
	}

	apiInst := &api.Instance{
		ID:        instance.InstanceID,
		HostID:    instance.HostID,
		NodeName:  instance.NodeName,
		State:     string(instance.State),
		CreatedAt: instance.CreatedAt.Format(time.RFC3339),
		UpdatedAt: instance.UpdatedAt.Format(time.RFC3339),
		Error:     utils.NillablePointerTo(instance.Error),
	}

	if status := instance.Status; status != nil {
		apiInst.ConnectionInfo = instanceConnectionInfoToAPI(status)
		apiInst.Postgres = instancePostgresStatusToAPI(status)
		apiInst.Spock = instanceSpockStatusToAPI(status)
		if status.StatusUpdatedAt != nil {
			apiInst.StatusUpdatedAt = utils.PointerTo(status.StatusUpdatedAt.Format(time.RFC3339))
		}

		// An instance error takes precedence over its status error because it
		// represents a failed modification to the database.
		if apiInst.Error == nil && status.Error != nil {
			apiInst.Error = status.Error
		}
	}

	return apiInst
}

func apiToDatabaseNodes(apiNodes []*api.DatabaseNodeSpec) ([]*database.Node, error) {
	nodes := make([]*database.Node, len(apiNodes))
	for i, apiNode := range apiNodes {
		cpus, err := parseCPUs(apiNode.Cpus)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CPUs: %w", err)
		}
		memory, err := parseBytes(apiNode.Memory)
		if err != nil {
			return nil, fmt.Errorf("failed to parse memory: %w", err)
		}
		// Host IDs have already been validated before this is called.
		hostIDs := make([]string, len(apiNode.HostIds))
		for i, h := range apiNode.HostIds {
			hostIDs[i] = string(h)
		}
		backupConfig, err := apiToBackupConfig(apiNode.BackupConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse backup config: %w", err)
		}
		restoreConfig, err := apiToRestoreConfig(apiNode.RestoreConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse restore config: %w", err)
		}
		nodes[i] = &database.Node{
			Name:             apiNode.Name,
			HostIDs:          hostIDs,
			PostgresVersion:  utils.FromPointer(apiNode.PostgresVersion),
			Port:             apiNode.Port,
			CPUs:             cpus,
			MemoryBytes:      memory,
			PostgreSQLConf:   apiNode.PostgresqlConf,
			BackupConfig:     backupConfig,
			RestoreConfig:    restoreConfig,
			OrchestratorOpts: orchestratorOptsToDatabase(apiNode.OrchestratorOpts),
			SourceNode:       utils.FromPointer(apiNode.SourceNode),
		}
	}
	return nodes, nil
}

func apiToBackupConfig(apiConfig *api.BackupConfigSpec) (*database.BackupConfig, error) {
	if apiConfig == nil {
		return nil, nil
	}

	repositories := make([]*pgbackrest.Repository, len(apiConfig.Repositories))
	for i, apiRepo := range apiConfig.Repositories {
		// ID has been validated before this point
		var id string
		if apiRepo.ID != nil {
			id = string(*apiRepo.ID)
		}
		repositories[i] = &pgbackrest.Repository{
			ID:                id,
			Type:              pgbackrest.RepositoryType(apiRepo.Type),
			S3Bucket:          utils.FromPointer(apiRepo.S3Bucket),
			S3Region:          utils.FromPointer(apiRepo.S3Region),
			S3Endpoint:        utils.FromPointer(apiRepo.S3Endpoint),
			S3Key:             utils.FromPointer(apiRepo.S3Key),
			S3KeySecret:       utils.FromPointer(apiRepo.S3KeySecret),
			GCSBucket:         utils.FromPointer(apiRepo.GcsBucket),
			GCSEndpoint:       utils.FromPointer(apiRepo.GcsEndpoint),
			GCSKey:            utils.FromPointer(apiRepo.GcsKey),
			AzureAccount:      utils.FromPointer(apiRepo.AzureAccount),
			AzureContainer:    utils.FromPointer(apiRepo.AzureContainer),
			AzureEndpoint:     utils.FromPointer(apiRepo.AzureEndpoint),
			AzureKey:          utils.FromPointer(apiRepo.AzureKey),
			RetentionFull:     utils.FromPointer(apiRepo.RetentionFull),
			RetentionFullType: pgbackrest.RetentionFullType(utils.FromPointer(apiRepo.RetentionFullType)),
			BasePath:          utils.FromPointer(apiRepo.BasePath),
			CustomOptions:     apiRepo.CustomOptions,
		}
	}
	schedules := make([]*database.BackupSchedule, len(apiConfig.Schedules))
	for j, apiSchedule := range apiConfig.Schedules {
		schedules[j] = &database.BackupSchedule{
			ID:             apiSchedule.ID,
			Type:           database.BackupScheduleType(apiSchedule.Type),
			CronExpression: apiSchedule.CronExpression,
		}
	}
	return &database.BackupConfig{
		Repositories: repositories,
		Schedules:    schedules,
	}, nil
}

func apiRestoreToRepository(apiRepository *api.RestoreRepositorySpec) (*pgbackrest.Repository, error) {
	if apiRepository == nil {
		return nil, nil
	}
	// ID has been validated before this point
	var id string
	if apiRepository.ID != nil {
		id = string(*apiRepository.ID)
	}
	return &pgbackrest.Repository{
		ID:             id,
		Type:           pgbackrest.RepositoryType(apiRepository.Type),
		S3Bucket:       utils.FromPointer(apiRepository.S3Bucket),
		S3Region:       utils.FromPointer(apiRepository.S3Region),
		S3Endpoint:     utils.FromPointer(apiRepository.S3Endpoint),
		S3Key:          utils.FromPointer(apiRepository.S3Key),
		S3KeySecret:    utils.FromPointer(apiRepository.S3KeySecret),
		GCSBucket:      utils.FromPointer(apiRepository.GcsBucket),
		GCSEndpoint:    utils.FromPointer(apiRepository.GcsEndpoint),
		GCSKey:         utils.FromPointer(apiRepository.GcsKey),
		AzureAccount:   utils.FromPointer(apiRepository.AzureAccount),
		AzureContainer: utils.FromPointer(apiRepository.AzureContainer),
		AzureEndpoint:  utils.FromPointer(apiRepository.AzureEndpoint),
		AzureKey:       utils.FromPointer(apiRepository.AzureKey),
		BasePath:       utils.FromPointer(apiRepository.BasePath),
		CustomOptions:  apiRepository.CustomOptions,
	}, nil
}

func apiToRestoreConfig(apiConfig *api.RestoreConfigSpec) (*database.RestoreConfig, error) {
	if apiConfig == nil {
		return nil, nil
	}

	err := errors.Join(validateRestoreConfig(apiConfig, nil)...)
	if err != nil {
		return nil, err
	}

	repo, err := apiRestoreToRepository(apiConfig.Repository)
	if err != nil {
		return nil, err
	}
	return &database.RestoreConfig{
		SourceDatabaseID:   string(apiConfig.SourceDatabaseID),
		SourceNodeName:     apiConfig.SourceNodeName,
		SourceDatabaseName: apiConfig.SourceDatabaseName,
		RestoreOptions:     apiConfig.RestoreOptions,
		Repository:         repo,
	}, nil
}

func apiToServiceSpec(apiSvc *api.ServiceSpec) (*database.ServiceSpec, error) {
	if apiSvc == nil {
		return nil, nil
	}

	hostIDs := make([]string, len(apiSvc.HostIds))
	for i, hostID := range apiSvc.HostIds {
		hostIDs[i] = string(hostID)
	}

	var cpus *float64
	if apiSvc.Cpus != nil {
		c, err := parseCPUs(apiSvc.Cpus)
		if err != nil {
			return nil, fmt.Errorf("failed to parse service CPUs: %w", err)
		}
		cpus = &c
	}

	var memory *uint64
	if apiSvc.Memory != nil {
		m, err := parseBytes(apiSvc.Memory)
		if err != nil {
			return nil, fmt.Errorf("failed to parse service memory: %w", err)
		}
		memory = &m
	}

	return &database.ServiceSpec{
		ServiceID:   string(apiSvc.ServiceID),
		ServiceType: apiSvc.ServiceType,
		Version:     apiSvc.Version,
		HostIDs:     hostIDs,
		Port:        apiSvc.Port,
		Config:      apiSvc.Config,
		CPUs:        cpus,
		MemoryBytes: memory,
	}, nil
}

func apiToServiceSpecs(apiServices []*api.ServiceSpec) ([]*database.ServiceSpec, error) {
	if len(apiServices) == 0 {
		return nil, nil
	}

	services := make([]*database.ServiceSpec, len(apiServices))
	for i, apiSvc := range apiServices {
		svc, err := apiToServiceSpec(apiSvc)
		if err != nil {
			return nil, fmt.Errorf("failed to convert service %d: %w", i, err)
		}
		services[i] = svc
	}
	return services, nil
}

func apiToDatabaseSpec(id, tID *api.Identifier, apiSpec *api.DatabaseSpec) (*database.Spec, error) {
	var databaseID string
	var err error
	if id != nil {
		databaseID, err = identToString(*id, []string{"id"})
		if err != nil {
			return nil, err
		}
	} else {
		databaseID = uuid.NewString()
	}
	var tenantID *string
	if tID != nil {
		t, err := identToString(*tID, []string{"tenant_id"})
		if err != nil {
			return nil, err
		}
		tenantID = &t
	}
	if err := validateDatabaseSpec(apiSpec); err != nil {
		return nil, err
	}

	cpus, err := parseCPUs(apiSpec.Cpus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CPUs: %w", err)
	}
	memory, err := parseBytes(apiSpec.Memory)
	if err != nil {
		return nil, fmt.Errorf("failed to parse memory: %w", err)
	}
	nodes, err := apiToDatabaseNodes(apiSpec.Nodes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nodes: %w", err)
	}
	users := make([]*database.User, len(apiSpec.DatabaseUsers))
	for i, apiUser := range apiSpec.DatabaseUsers {
		users[i] = &database.User{
			Username:   apiUser.Username,
			Password:   utils.FromPointer(apiUser.Password),
			DBOwner:    utils.FromPointer(apiUser.DbOwner),
			Attributes: apiUser.Attributes,
			Roles:      apiUser.Roles,
		}
	}
	services, err := apiToServiceSpecs(apiSpec.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to parse services: %w", err)
	}
	backupConfig, err := apiToBackupConfig(apiSpec.BackupConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse backup configs: %w", err)
	}
	restoreConfig, err := apiToRestoreConfig(apiSpec.RestoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse restore config: %w", err)
	}

	return &database.Spec{
		DatabaseID:       databaseID,
		TenantID:         tenantID,
		DatabaseName:     apiSpec.DatabaseName,
		PostgresVersion:  utils.FromPointer(apiSpec.PostgresVersion),
		SpockVersion:     utils.FromPointer(apiSpec.SpockVersion),
		Port:             apiSpec.Port,
		CPUs:             cpus,
		MemoryBytes:      memory,
		Nodes:            nodes,
		DatabaseUsers:    users,
		Services:         services,
		BackupConfig:     backupConfig,
		PostgreSQLConf:   apiSpec.PostgresqlConf,
		RestoreConfig:    restoreConfig,
		OrchestratorOpts: orchestratorOptsToDatabase(apiSpec.OrchestratorOpts),
	}, nil
}

func taskToAPI(t *task.Task) *api.Task {
	var (
		completedAt *string
		parentID    *string
	)
	if !t.CompletedAt.IsZero() {
		completedAt = utils.PointerTo(t.CompletedAt.Format(time.RFC3339))
	}
	if t.ParentID != uuid.Nil {
		parentID = utils.PointerTo(t.ParentID.String())
	}
	return &api.Task{
		ParentID:    parentID,
		Scope:       t.Scope.String(),
		EntityID:    t.EntityID,
		DatabaseID:  utils.NillablePointerTo(t.DatabaseID),
		TaskID:      t.TaskID.String(),
		NodeName:    utils.NillablePointerTo(t.NodeName),
		HostID:      utils.NillablePointerTo(t.HostID),
		InstanceID:  utils.NillablePointerTo(t.InstanceID),
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		CompletedAt: completedAt,
		Type:        string(t.Type),
		Status:      string(t.Status),
		Error:       utils.NillablePointerTo(t.Error),
	}
}

func tasksToAPI(tasks []*task.Task) []*api.Task {
	apiTasks := make([]*api.Task, len(tasks))
	for i, t := range tasks {
		apiTasks[i] = taskToAPI(t)
	}
	return apiTasks
}

func taskLogToAPI(t *task.TaskLog, status task.Status) *api.TaskLog {
	var lastEntryID *string
	if t.LastEntryID != uuid.Nil {
		lastEntryID = utils.PointerTo(t.LastEntryID.String())
	}
	return &api.TaskLog{
		Scope:       t.Scope.String(),
		EntityID:    t.EntityID,
		DatabaseID:  utils.NillablePointerTo(t.DatabaseID),
		TaskID:      t.TaskID.String(),
		TaskStatus:  string(status),
		LastEntryID: lastEntryID,
		Entries:     taskLogEntriesToAPI(t.Entries),
	}
}

func taskLogEntriesToAPI(entries []task.LogEntry) []*api.TaskLogEntry {
	// we want to return an empty JSON array, not null, if there are no entries
	if len(entries) == 0 {
		return []*api.TaskLogEntry{}
	}
	apiEntries := make([]*api.TaskLogEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = &api.TaskLogEntry{
			Message:   e.Message,
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Fields:    e.Fields,
		}
	}
	return apiEntries
}

func taskListOptions(req *api.ListDatabaseTasksPayload) (task.TaskListOptions, error) {
	options := task.TaskListOptions{}
	if req.Limit != nil {
		options.Limit = *req.Limit
	}
	if req.AfterTaskID != nil {
		afterTaskID, err := uuid.Parse(*req.AfterTaskID)
		if err != nil {
			return task.TaskListOptions{}, fmt.Errorf("invalid after task ID %q: %w", *req.AfterTaskID, err)
		}
		options.AfterTaskID = afterTaskID
	}
	if req.SortOrder != nil {
		switch *req.SortOrder {
		case "asc", "ascend", "ascending":
			options.SortOrder = task.SortAscend
		case "desc", "descend", "descending":
			options.SortOrder = task.SortDescend
		default:
			return task.TaskListOptions{}, fmt.Errorf("invalid sort order %q", *req.SortOrder)
		}
	}

	return options, nil
}

func taskLogOptions(req *api.GetDatabaseTaskLogPayload) (task.TaskLogOptions, error) {
	options := task.TaskLogOptions{}
	if req.Limit != nil {
		options.Limit = *req.Limit
	}
	if req.AfterEntryID != nil {
		afterEntryID, err := uuid.Parse(*req.AfterEntryID)
		if err != nil {
			return task.TaskLogOptions{}, fmt.Errorf("invalid after entry ID %q: %w", *req.AfterEntryID, err)
		}
		options.AfterEntryID = afterEntryID
	}

	return options, nil
}

func humanizeBytes(size uint64) string {
	if size == 0 {
		return ""
	}
	h := humanize.Bytes(size)
	return strings.ReplaceAll(h, " ", "")
}

func humanizeCPUs(cpus float64) string {
	if cpus == 0 {
		return ""
	}
	h := humanize.SI(cpus, "")
	return strings.ReplaceAll(h, " ", "")
}

func parseBytes(size *string) (uint64, error) {
	if size == nil {
		return 0, nil
	}
	s := *size
	if s == "" {
		return 0, nil
	}
	bytes, err := humanize.ParseBytes(s)
	if err != nil {
		return 0, fmt.Errorf("failed to parse bytes: %w", err)
	}
	return bytes, nil
}

func parseCPUs(cpus *string) (float64, error) {
	if cpus == nil {
		return 0, nil
	}
	s := *cpus
	if s == "" {
		return 0, nil
	}
	c, _, err := humanize.ParseSI(s)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPUs: %w", err)
	}
	return c, nil
}

type stringer interface {
	String() string
}

func stringifyStringerPtr[T stringer](v *T) *string {
	if v == nil {
		return nil
	}
	return utils.PointerTo((*v).String())
}

func extraVolumesToDatabase(extraVolumes []*api.ExtraVolumesSpec) []database.ExtraVolumesSpec {
	var result []database.ExtraVolumesSpec
	for _, vol := range extraVolumes {
		if vol == nil {
			continue
		}

		result = append(result, database.ExtraVolumesSpec{
			HostPath:        vol.HostPath,
			DestinationPath: vol.DestinationPath,
		})
	}
	return result
}

func extraVolumesToAPI(vols []database.ExtraVolumesSpec) []*api.ExtraVolumesSpec {
	if len(vols) == 0 {
		return nil
	}
	result := make([]*api.ExtraVolumesSpec, len(vols))
	for i, v := range vols {
		result[i] = &api.ExtraVolumesSpec{
			HostPath:        v.HostPath,
			DestinationPath: v.DestinationPath,
		}
	}
	return result
}

func dbIdentToString(id api.Identifier) (string, error) {
	return identToString(id, []string{"database_id"})
}

func hostIdentToString(id api.Identifier) (string, error) {
	return identToString(id, []string{"host_id"})
}

func identToString(id api.Identifier, path []string) (string, error) {
	out := string(id)
	if err := validateIdentifier(out, path); err != nil {
		return "", err
	}
	return out, nil
}

func orchestratorOptsToDatabase(opts *api.OrchestratorOpts) *database.OrchestratorOpts {
	if opts == nil {
		return nil
	}
	return &database.OrchestratorOpts{
		Swarm: &database.SwarmOpts{
			ExtraVolumes:  extraVolumesToDatabase(opts.Swarm.ExtraVolumes),
			ExtraNetworks: extraNetworksToDatabase(opts.Swarm.ExtraNetworks),
			ExtraLabels:   maps.Clone(opts.Swarm.ExtraLabels),
		},
	}
}

func orchestratorOptsToAPI(opts *database.OrchestratorOpts) *api.OrchestratorOpts {
	if opts == nil {
		return nil
	}
	return &api.OrchestratorOpts{
		Swarm: &api.SwarmOpts{
			ExtraVolumes:  extraVolumesToAPI(opts.Swarm.ExtraVolumes),
			ExtraNetworks: extraNetworksToAPI(opts.Swarm.ExtraNetworks),
			ExtraLabels:   maps.Clone(opts.Swarm.ExtraLabels),
		},
	}

}

func extraNetworksToDatabase(networks []*api.ExtraNetworkSpec) []database.ExtraNetworkSpec {
	var result []database.ExtraNetworkSpec
	for _, net := range networks {
		if net == nil {
			continue
		}
		result = append(result, database.ExtraNetworkSpec{
			ID:         net.ID,
			Aliases:    net.Aliases,
			DriverOpts: net.DriverOpts,
		})
	}
	return result
}

func extraNetworksToAPI(nets []database.ExtraNetworkSpec) []*api.ExtraNetworkSpec {
	if len(nets) == 0 {
		return nil
	}
	result := make([]*api.ExtraNetworkSpec, len(nets))
	for i, net := range nets {
		result[i] = &api.ExtraNetworkSpec{
			ID:         net.ID,
			Aliases:    net.Aliases,
			DriverOpts: net.DriverOpts,
		}
	}
	return result
}

func taskListOptionsFromHost(req *api.ListHostTasksPayload) (task.TaskListOptions, error) {
	options := task.TaskListOptions{}
	if req.Limit != nil {
		options.Limit = *req.Limit
	}
	if req.AfterTaskID != nil {
		afterTaskID, err := uuid.Parse(*req.AfterTaskID)
		if err != nil {
			return task.TaskListOptions{}, fmt.Errorf("invalid after task ID %q: %w", *req.AfterTaskID, err)
		}
		options.AfterTaskID = afterTaskID
	}
	if req.SortOrder != nil {
		switch *req.SortOrder {
		case "asc", "ascend", "ascending":
			options.SortOrder = task.SortAscend
		case "desc", "descend", "descending":
			options.SortOrder = task.SortDescend
		default:
			return task.TaskListOptions{}, fmt.Errorf("invalid sort order %q", *req.SortOrder)
		}
	}
	return options, nil
}

func taskLogOptionsFromHost(req *api.GetHostTaskLogPayload) (task.TaskLogOptions, error) {
	options := task.TaskLogOptions{}
	if req.Limit != nil {
		options.Limit = *req.Limit
	}
	if req.AfterEntryID != nil {
		afterEntryID, err := uuid.Parse(*req.AfterEntryID)
		if err != nil {
			return task.TaskLogOptions{}, fmt.Errorf("invalid after entry ID %q: %w", *req.AfterEntryID, err)
		}
		options.AfterEntryID = afterEntryID
	}
	return options, nil
}

func taskListOptionsFromGeneric(req *api.ListTasksPayload) (task.Scope, string, error) {
	if req.Scope == nil {
		// No scope specified - return empty scope and entity ID
		return "", "", nil
	}

	scope, err := parseScope(*req.Scope)
	if err != nil {
		return "", "", err
	}

	var entityID string
	if req.EntityID != nil {
		id, err := identToString(*req.EntityID, []string{"entity_id"})
		if err != nil {
			return "", "", err
		}
		entityID = id
	}

	return scope, entityID, nil
}

func parseScope(scopeStr string) (task.Scope, error) {
	switch scopeStr {
	case "database":
		return task.ScopeDatabase, nil
	case "host":
		return task.ScopeHost, nil
	default:
		return "", fmt.Errorf("invalid scope %q", scopeStr)
	}
}
