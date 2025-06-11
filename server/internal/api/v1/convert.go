package v1

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"

	api "github.com/pgEdge/control-plane/api/v1/gen/control_plane"
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
			CohortID:         h.Cohort.CohortID,
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
		Hostname:     h.Hostname,
		Ipv4Address:  h.IPv4Address,
		Cpus:         utils.NillablePointerTo(h.CPUs),
		Memory:       utils.NillablePointerTo(humanizeBytes(h.MemBytes)),
		Cohort:       cohort,
		ID:           h.ID.String(),
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
		hostIDs := make([]string, len(node.HostIDs))
		for j, hostID := range node.HostIDs {
			hostIDs[j] = hostID.String()
		}
		apiNodes[i] = &api.DatabaseNodeSpec{
			Name:            node.Name,
			HostIds:         hostIDs,
			PostgresVersion: utils.NillablePointerTo(node.PostgresVersion),
			Port:            utils.NillablePointerTo(node.Port),
			Cpus:            utils.NillablePointerTo(humanizeCPUs(node.CPUs)),
			Memory:          utils.NillablePointerTo(humanizeBytes(node.MemoryBytes)),
			PostgresqlConf:  node.PostgreSQLConf,
			BackupConfig:    backupConfigToAPI(node.BackupConfig),
			RestoreConfig:   restoreConfigToAPI(node.RestoreConfig),
			ExtraVolumes:    extraVolumesToAPI(node.ExtraVolumes),
		}
	}
	return apiNodes
}

func databaseUsersToAPI(users []*database.User) []*api.DatabaseUserSpec {
	apiUsers := make([]*api.DatabaseUserSpec, len(users))
	for i, user := range users {
		apiUsers[i] = &api.DatabaseUserSpec{
			Username:   user.Username,
			Password:   user.Password, // TODO: Does this need to be censored?
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
		repositories[i] = &api.BackupRepositorySpec{
			ID:                utils.NillablePointerTo(repo.ID),
			Type:              string(repo.Type),
			S3Bucket:          utils.NillablePointerTo(repo.S3Bucket),
			S3Region:          utils.NillablePointerTo(repo.S3Region),
			S3Endpoint:        utils.NillablePointerTo(repo.S3Endpoint),
			S3Key:             utils.NillablePointerTo(repo.S3Key),
			S3KeySecret:       utils.NillablePointerTo(repo.S3KeySecret),
			GcsBucket:         utils.NillablePointerTo(repo.GCSBucket),
			GcsEndpoint:       utils.NillablePointerTo(repo.GCSEndpoint),
			GcsKey:            utils.NillablePointerTo(repo.GCSKey),
			AzureAccount:      utils.NillablePointerTo(repo.AzureAccount),
			AzureContainer:    utils.NillablePointerTo(repo.AzureContainer),
			AzureEndpoint:     utils.NillablePointerTo(repo.AzureEndpoint),
			AzureKey:          utils.NillablePointerTo(repo.AzureKey),
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
	return &api.RestoreConfigSpec{
		SourceDatabaseID:   config.SourceDatabaseID.String(),
		SourceNodeName:     config.SourceNodeName,
		SourceDatabaseName: config.SourceDatabaseName,
		RestoreOptions:     config.RestoreOptions,
		Repository: &api.RestoreRepositorySpec{
			ID:             utils.NillablePointerTo(config.Repository.ID),
			Type:           string(config.Repository.Type),
			S3Bucket:       utils.NillablePointerTo(config.Repository.S3Bucket),
			S3Region:       utils.NillablePointerTo(config.Repository.S3Region),
			S3Endpoint:     utils.NillablePointerTo(config.Repository.S3Endpoint),
			S3Key:          utils.NillablePointerTo(config.Repository.S3Key),
			S3KeySecret:    utils.NillablePointerTo(config.Repository.S3KeySecret),
			GcsBucket:      utils.NillablePointerTo(config.Repository.GCSBucket),
			GcsEndpoint:    utils.NillablePointerTo(config.Repository.GCSEndpoint),
			GcsKey:         utils.NillablePointerTo(config.Repository.GCSKey),
			AzureAccount:   utils.NillablePointerTo(config.Repository.AzureAccount),
			AzureContainer: utils.NillablePointerTo(config.Repository.AzureContainer),
			AzureEndpoint:  utils.NillablePointerTo(config.Repository.AzureEndpoint),
			AzureKey:       utils.NillablePointerTo(config.Repository.AzureKey),
			BasePath:       utils.NillablePointerTo(config.Repository.BasePath),
			CustomOptions:  config.Repository.CustomOptions,
		},
	}
}

func databaseSpecToAPI(d *database.Spec) *api.DatabaseSpec {
	return &api.DatabaseSpec{
		DatabaseName:    d.DatabaseName,
		PostgresVersion: utils.NillablePointerTo(d.PostgresVersion),
		SpockVersion:    utils.NillablePointerTo(d.SpockVersion),
		Port:            utils.NillablePointerTo(d.Port),
		Cpus:            utils.NillablePointerTo(humanizeCPUs(d.CPUs)),
		Memory:          utils.NillablePointerTo(humanizeBytes(d.MemoryBytes)),
		Nodes:           databaseNodesToAPI(d.Nodes),
		DatabaseUsers:   databaseUsersToAPI(d.DatabaseUsers),
		BackupConfig:    backupConfigToAPI(d.BackupConfig),
		RestoreConfig:   restoreConfigToAPI(d.RestoreConfig),
		PostgresqlConf:  d.PostgreSQLConf,
		ExtraVolumes:    extraVolumesToAPI(d.ExtraVolumes),
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

	return &api.Database{
		ID:        d.DatabaseID.String(),
		TenantID:  stringifyStringerPtr(d.TenantID),
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
		State:     string(d.State),
		Spec:      spec,
		Instances: instances,
	}
}

func instanceConnectionInfoToAPI(status *database.InstanceStatus) *api.InstanceConnectionInfo {
	if status == nil {
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
	if status == nil {
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
		ID:        instance.InstanceID.String(),
		HostID:    instance.HostID.String(),
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
		hostIDs := make([]uuid.UUID, len(apiNode.HostIds))
		for i, hostID := range apiNode.HostIds {
			parsedHostID, err := uuid.Parse(hostID)
			if err != nil {
				return nil, fmt.Errorf("failed to parse host ID: %w", err)
			}
			hostIDs[i] = parsedHostID
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
			Name:            apiNode.Name,
			HostIDs:         hostIDs,
			PostgresVersion: utils.FromPointer(apiNode.PostgresVersion),
			Port:            utils.FromPointer(apiNode.Port),
			CPUs:            cpus,
			MemoryBytes:     memory,
			PostgreSQLConf:  apiNode.PostgresqlConf,
			BackupConfig:    backupConfig,
			RestoreConfig:   restoreConfig,
			ExtraVolumes:    extraVolumesToDatabase(apiNode.ExtraVolumes),
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
		repositories[i] = &pgbackrest.Repository{
			ID:                utils.FromPointer(apiRepo.ID),
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

func apiRestoreToRepository(apiRepository *api.RestoreRepositorySpec) *pgbackrest.Repository {
	if apiRepository == nil {
		return nil
	}
	return &pgbackrest.Repository{
		ID:             utils.FromPointer(apiRepository.ID),
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
	}
}

func apiToRestoreConfig(apiConfig *api.RestoreConfigSpec) (*database.RestoreConfig, error) {
	if apiConfig == nil {
		return nil, nil
	}
	databaseID, err := uuid.Parse(apiConfig.SourceDatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database ID: %w", err)
	}
	return &database.RestoreConfig{
		SourceDatabaseID:   databaseID,
		SourceNodeName:     apiConfig.SourceNodeName,
		SourceDatabaseName: apiConfig.SourceDatabaseName,
		RestoreOptions:     apiConfig.RestoreOptions,
		Repository:         apiRestoreToRepository(apiConfig.Repository),
	}, nil
}

func apiToDatabaseSpec(id, tID *string, apiSpec *api.DatabaseSpec) (*database.Spec, error) {
	databaseID, err := parseUUIDPtr(id)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database ID: %w", err)
	}
	if databaseID == uuid.Nil {
		databaseID = uuid.New()
	}
	var tenantID *uuid.UUID
	if tID != nil {
		parsedTenantID, err := parseUUID(*tID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tenant ID: %w", err)
		}
		tenantID = &parsedTenantID
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
			Password:   apiUser.Password,
			DBOwner:    utils.FromPointer(apiUser.DbOwner),
			Attributes: apiUser.Attributes,
			Roles:      apiUser.Roles,
		}
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
		DatabaseID:      databaseID,
		TenantID:        tenantID,
		DatabaseName:    apiSpec.DatabaseName,
		PostgresVersion: utils.FromPointer(apiSpec.PostgresVersion),
		SpockVersion:    utils.FromPointer(apiSpec.SpockVersion),
		Port:            utils.FromPointer(apiSpec.Port),
		CPUs:            cpus,
		MemoryBytes:     memory,
		Nodes:           nodes,
		DatabaseUsers:   users,
		BackupConfig:    backupConfig,
		PostgreSQLConf:  apiSpec.PostgresqlConf,
		RestoreConfig:   restoreConfig,
		ExtraVolumes:    extraVolumesToDatabase(apiSpec.ExtraVolumes),
	}, nil
}

func taskToAPI(t *task.Task) *api.Task {
	var (
		completedAt *string
		hostID      *string
		instanceID  *string
		parentID    *string
	)
	if !t.CompletedAt.IsZero() {
		completedAt = utils.PointerTo(t.CompletedAt.Format(time.RFC3339))
	}
	if t.HostID != uuid.Nil {
		hostID = utils.PointerTo(t.HostID.String())
	}
	if t.InstanceID != uuid.Nil {
		instanceID = utils.PointerTo(t.InstanceID.String())
	}
	if t.ParentID != uuid.Nil {
		parentID = utils.PointerTo(t.ParentID.String())
	}
	return &api.Task{
		ParentID:    parentID,
		DatabaseID:  t.DatabaseID.String(),
		TaskID:      t.TaskID.String(),
		NodeName:    utils.NillablePointerTo(t.NodeName),
		HostID:      hostID,
		InstanceID:  instanceID,
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
		DatabaseID:  t.DatabaseID.String(),
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

func parseUUID(id string) (uuid.UUID, error) {
	if id == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(id)
}

func parseUUIDPtr(id *string) (uuid.UUID, error) {
	if id == nil {
		return uuid.Nil, nil
	}
	return uuid.Parse(*id)
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
