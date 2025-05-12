package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"

	api "github.com/pgEdge/control-plane/api/gen/control_plane"
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
			Healthy: utils.PointerTo(status.Healthy),
			Error:   status.Error,
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
		Cpus:         h.CPUs,
		Memory:       humanizeBytes(h.MemBytes),
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
			StorageClass:    utils.NillablePointerTo(node.StorageClass),
			StorageSize:     utils.NillablePointerTo(humanizeBytes(node.StorageSizeBytes)),
			Cpus:            utils.NillablePointerTo(humanizeCPUs(node.CPUs)),
			Memory:          utils.NillablePointerTo(humanizeBytes(node.MemoryBytes)),
			PostgresqlConf:  node.PostgreSQLConf,
			BackupConfig:    backupConfigToAPI(node.BackupConfig),
			RestoreConfig:   restoreConfigToAPI(node.RestoreConfig),
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
		Provider:     string(config.Provider),
		Repositories: repositories,
		Schedules:    schedules,
	}
}

func restoreConfigToAPI(config *database.RestoreConfig) *api.RestoreConfigSpec {
	if config == nil {
		return nil
	}
	return &api.RestoreConfigSpec{
		Provider:           string(config.Provider),
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
		DatabaseName:       d.DatabaseName,
		PostgresVersion:    utils.NillablePointerTo(d.PostgresVersion),
		SpockVersion:       utils.NillablePointerTo(d.SpockVersion),
		Port:               utils.NillablePointerTo(d.Port),
		DeletionProtection: utils.NillablePointerTo(d.DeletionProtection),
		StorageClass:       utils.NillablePointerTo(d.StorageClass),
		StorageSize:        utils.NillablePointerTo(humanizeBytes(d.StorageSizeBytes)),
		Cpus:               utils.NillablePointerTo(humanizeCPUs(d.CPUs)),
		Memory:             utils.NillablePointerTo(humanizeBytes(d.MemoryBytes)),
		Nodes:              databaseNodesToAPI(d.Nodes),
		DatabaseUsers:      databaseUsersToAPI(d.DatabaseUsers),
		Features:           d.Features,
		BackupConfig:       backupConfigToAPI(d.BackupConfig),
		RestoreConfig:      restoreConfigToAPI(d.RestoreConfig),
		PostgresqlConf:     d.PostgreSQLConf,
	}
}

func databaseToAPI(d *database.Database) *api.Database {
	return &api.Database{
		ID:        d.DatabaseID.String(),
		TenantID:  stringifyStringerPtr(d.TenantID),
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
		State:     string(d.State),
		Spec:      databaseSpecToAPI(d.Spec),
	}
}

func apiToDatabaseNodes(apiNodes []*api.DatabaseNodeSpec) ([]*database.Node, error) {
	nodes := make([]*database.Node, len(apiNodes))
	for i, apiNode := range apiNodes {
		storageSize, err := parseBytes(apiNode.StorageSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse storage size: %w", err)
		}
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
			Name:             apiNode.Name,
			HostIDs:          hostIDs,
			PostgresVersion:  utils.FromPointer(apiNode.PostgresVersion),
			Port:             utils.FromPointer(apiNode.Port),
			StorageClass:     utils.FromPointer(apiNode.StorageClass),
			StorageSizeBytes: storageSize,
			CPUs:             cpus,
			MemoryBytes:      memory,
			PostgreSQLConf:   apiNode.PostgresqlConf,
			BackupConfig:     backupConfig,
			RestoreConfig:    restoreConfig,
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
		Provider:     database.BackupProvider(apiConfig.Provider),
		Repositories: repositories,
		Schedules:    schedules,
	}, nil
}

func apiToRestoreConfig(apiConfig *api.RestoreConfigSpec) (*database.RestoreConfig, error) {
	if apiConfig == nil {
		return nil, nil
	}
	if apiConfig.Repository == nil {
		return &database.RestoreConfig{
			Provider: database.BackupProvider(apiConfig.Provider),
		}, nil
	}
	databaseID, err := uuid.Parse(apiConfig.SourceDatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database ID: %w", err)
	}
	return &database.RestoreConfig{
		Provider:           database.BackupProvider(apiConfig.Provider),
		SourceDatabaseID:   databaseID,
		SourceNodeName:     apiConfig.SourceNodeName,
		SourceDatabaseName: apiConfig.SourceDatabaseName,
		RestoreOptions:     apiConfig.RestoreOptions,
		Repository: &pgbackrest.Repository{
			ID:             utils.FromPointer(apiConfig.Repository.ID),
			Type:           pgbackrest.RepositoryType(apiConfig.Repository.Type),
			S3Bucket:       utils.FromPointer(apiConfig.Repository.S3Bucket),
			S3Region:       utils.FromPointer(apiConfig.Repository.S3Region),
			S3Endpoint:     utils.FromPointer(apiConfig.Repository.S3Endpoint),
			S3Key:          utils.FromPointer(apiConfig.Repository.S3Key),
			S3KeySecret:    utils.FromPointer(apiConfig.Repository.S3KeySecret),
			GCSBucket:      utils.FromPointer(apiConfig.Repository.GcsBucket),
			GCSEndpoint:    utils.FromPointer(apiConfig.Repository.GcsEndpoint),
			GCSKey:         utils.FromPointer(apiConfig.Repository.GcsKey),
			AzureAccount:   utils.FromPointer(apiConfig.Repository.AzureAccount),
			AzureContainer: utils.FromPointer(apiConfig.Repository.AzureContainer),
			AzureEndpoint:  utils.FromPointer(apiConfig.Repository.AzureEndpoint),
			AzureKey:       utils.FromPointer(apiConfig.Repository.AzureKey),
			BasePath:       utils.FromPointer(apiConfig.Repository.BasePath),
			CustomOptions:  apiConfig.Repository.CustomOptions,
		},
	}, nil
}

func apiToDatabaseSpec(id, tID *string, apiSpec *api.DatabaseSpec) (*database.Spec, error) {
	databaseID, err := parseUUIDPtr(id)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database ID: %w", err)
	}
	var tenantID *uuid.UUID
	if tID != nil {
		parsedTenantID, err := parseUUID(*tID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tenant ID: %w", err)
		}
		tenantID = &parsedTenantID
	}
	storageSize, err := parseBytes(apiSpec.StorageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to parse storage size: %w", err)
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
		DatabaseID:         databaseID,
		TenantID:           tenantID,
		DatabaseName:       apiSpec.DatabaseName,
		PostgresVersion:    utils.FromPointer(apiSpec.PostgresVersion),
		SpockVersion:       utils.FromPointer(apiSpec.SpockVersion),
		Port:               utils.FromPointer(apiSpec.Port),
		DeletionProtection: utils.FromPointer(apiSpec.DeletionProtection),
		StorageClass:       utils.FromPointer(apiSpec.StorageClass),
		StorageSizeBytes:   storageSize,
		CPUs:               cpus,
		MemoryBytes:        memory,
		Nodes:              nodes,
		DatabaseUsers:      users,
		Features:           apiSpec.Features,
		BackupConfig:       backupConfig,
		PostgreSQLConf:     apiSpec.PostgresqlConf,
		RestoreConfig:      restoreConfig,
	}, nil
}

func taskToAPI(t *task.Task) *api.Task {
	var completedAt, err *string
	if !t.CompletedAt.IsZero() {
		completedAt = utils.PointerTo(t.CompletedAt.Format(time.RFC3339))
	}
	if t.Error != "" {
		err = &t.Error
	}
	return &api.Task{
		TaskID:      t.TaskID.String(),
		DatabaseID:  t.DatabaseID.String(),
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		CompletedAt: completedAt,
		Type:        string(t.Type),
		Status:      string(t.Status),
		Error:       err,
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
	var lastLineID *string
	if t.LastLineID != uuid.Nil {
		lastLineID = utils.PointerTo(t.LastLineID.String())
	}
	// we want to return an empty array if there are no lines
	lines := []string{}
	if len(t.Lines) > 0 {
		lines = t.Lines
	}
	return &api.TaskLog{
		DatabaseID: t.DatabaseID.String(),
		TaskID:     t.TaskID.String(),
		TaskStatus: string(status),
		LastLineID: lastLineID,
		Lines:      lines,
	}
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
	if req.AfterLineID != nil {
		afterLineID, err := uuid.Parse(*req.AfterLineID)
		if err != nil {
			return task.TaskLogOptions{}, fmt.Errorf("invalid after line ID %q: %w", *req.AfterLineID, err)
		}
		options.AfterLineID = afterLineID
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
