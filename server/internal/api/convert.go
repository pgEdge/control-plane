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

// func instanceToApi(i *database.Instance) *api.Instance {
// 	return &api.Instance{
// 		ID:          i.InstanceID.String(),
// 		HostID:      i.HostID.String(),
// 		NodeName:    i.NodeName,
// 		ReplicaName: utils.PointerTo(i.ReplicaName),
// 		CreatedAt:   i.CreatedAt.Format(time.RFC3339),
// 		UpdatedAt:   i.UpdatedAt.Format(time.RFC3339),
// 		State:       string(i.State),

// 		InstanceID: i.InstanceID,
// 		HostID:     i.HostID,
// 		DatabaseID: i.DatabaseID,
// 		Role:       string(i.Role),
// 		UpdatedAt:  i.UpdatedAt.Format(time.RFC3339),
// 	}
// }

func instanceInterfacesToAPI(interfaces []*database.InstanceInterface) []*api.InstanceInterface {
	apiInterfaces := make([]*api.InstanceInterface, len(interfaces))
	for i, iface := range interfaces {
		apiInterfaces[i] = &api.InstanceInterface{
			NetworkType: string(iface.NetworkType),
			NetworkID:   utils.NillablePointerTo(iface.NetworkID),
			Hostname:    utils.NillablePointerTo(iface.Hostname),
			Ipv4Address: utils.NillablePointerTo(iface.IPv4Address),
			Port:        iface.Port,
		}
	}
	return apiInterfaces
}

func instanceToAPI(i *database.Instance) *api.Instance {
	return &api.Instance{
		ID:              i.InstanceID.String(),
		HostID:          i.HostID.String(),
		NodeName:        i.NodeName,
		ReplicaName:     utils.NillablePointerTo(i.ReplicaName),
		CreatedAt:       i.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       i.UpdatedAt.Format(time.RFC3339),
		State:           string(i.State),
		PatroniState:    utils.NillablePointerTo(string(i.PatroniState)),
		Role:            utils.NillablePointerTo(string(i.Role)),
		ReadOnly:        &i.ReadOnly,
		PendingRestart:  &i.PendingRestart,
		PatroniPaused:   &i.PatroniPaused,
		Interfaces:      instanceInterfacesToAPI(i.Interfaces),
		PostgresVersion: utils.NillablePointerTo(i.PostgresVersion),
		SpockVersion:    utils.NillablePointerTo(i.SpockVersion),
	}
}

func databaseNodesToAPI(nodes []*database.Node) []*api.DatabaseNodeSpec {
	apiNodes := make([]*api.DatabaseNodeSpec, len(nodes))
	for i, node := range nodes {
		apiReplicas := make([]*api.DatabaseReplicaSpec, len(node.ReadReplicas))
		for i, replica := range node.ReadReplicas {
			apiReplicas[i] = &api.DatabaseReplicaSpec{
				HostID: replica.HostID.String(),
			}
		}
		apiNodes[i] = &api.DatabaseNodeSpec{
			Name:            node.Name,
			HostID:          node.HostID.String(),
			PostgresVersion: utils.NillablePointerTo(node.PostgresVersion),
			Port:            utils.NillablePointerTo(node.Port),
			StorageClass:    utils.NillablePointerTo(node.StorageClass),
			StorageSize:     utils.NillablePointerTo(humanizeBytes(node.StorageSizeBytes)),
			Cpus:            utils.NillablePointerTo(humanizeCPUs(node.CPUs)),
			Memory:          utils.NillablePointerTo(humanizeBytes(node.MemoryBytes)),
			ReadReplicas:    apiReplicas,
			PostgresqlConf:  node.PostgreSQLConf,
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

func backupConfigsToAPI(configs []*database.BackupConfig) []*api.BackupConfigSpec {
	apiConfigs := make([]*api.BackupConfigSpec, len(configs))
	for i, config := range configs {
		repositories := make([]*api.BackupRepositorySpec, len(config.Repositories))
		for j, repo := range config.Repositories {
			repositories[j] = &api.BackupRepositorySpec{
				ID:                utils.PointerTo(repo.ID.String()),
				Type:              string(repo.Type),
				S3Bucket:          repo.S3Bucket,
				S3Region:          repo.S3Region,
				S3Endpoint:        repo.S3Endpoint,
				GcsBucket:         repo.GCSBucket,
				GcsEndpoint:       repo.GCSEndpoint,
				AzureAccount:      repo.AzureAccount,
				AzureContainer:    repo.AzureContainer,
				AzureEndpoint:     repo.AzureEndpoint,
				RetentionFull:     repo.RetentionFull,
				RetentionFullType: stringifyPtr(repo.RetentionFullType),
				BasePath:          repo.BasePath,
			}
		}
		schedules := make([]*api.BackupScheduleSpec, len(config.Schedules))
		for j, schedule := range config.Schedules {
			schedules[j] = &api.BackupScheduleSpec{
				ID:             schedule.ID,
				Type:           string(schedule.Type),
				CronExpression: schedule.CronExpression,
			}
		}

		apiConfigs[i] = &api.BackupConfigSpec{
			ID:           config.ID,
			NodeNames:    config.Nodes,
			Provider:     string(config.Provider),
			Repositories: repositories,
			Schedules:    schedules,
		}
	}
	return apiConfigs
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
		BackupConfigs:      backupConfigsToAPI(d.BackupConfigs),
		PostgresqlConf:     d.PostgreSQLConf,
	}
}

func databaseToAPI(d *database.Database) *api.Database {
	instances := make([]*api.Instance, len(d.Instances))
	for i, instance := range d.Instances {
		instances[i] = instanceToAPI(instance)
	}
	return &api.Database{
		ID:        d.DatabaseID.String(),
		TenantID:  stringifyStringerPtr(d.TenantID),
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
		State:     string(d.State),
		Instances: instances,
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
		hostID, err := uuid.Parse(apiNode.HostID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse host ID: %w", err)
		}
		readReplicas := make([]*database.ReadReplica, len(apiNode.ReadReplicas))
		for i, replica := range apiNode.ReadReplicas {
			replicaHost, err := uuid.Parse(replica.HostID)
			if err != nil {
				return nil, fmt.Errorf("failed to parse replica host ID: %w", err)
			}
			readReplicas[i] = &database.ReadReplica{
				HostID: replicaHost,
			}
		}
		nodes[i] = &database.Node{
			Name:             apiNode.Name,
			HostID:           hostID,
			PostgresVersion:  utils.FromPointer(apiNode.PostgresVersion),
			Port:             utils.FromPointer(apiNode.Port),
			StorageClass:     utils.FromPointer(apiNode.StorageClass),
			StorageSizeBytes: storageSize,
			CPUs:             cpus,
			MemoryBytes:      memory,
			ReadReplicas:     readReplicas,
			PostgreSQLConf:   apiNode.PostgresqlConf,
		}
	}
	return nodes, nil
}

func apiToBackupConfigs(apiConfigs []*api.BackupConfigSpec) ([]*database.BackupConfig, error) {
	configs := make([]*database.BackupConfig, len(apiConfigs))
	for i, apiConfig := range apiConfigs {
		repositories := make([]*database.BackupRepository, len(apiConfig.Repositories))
		for j, apiRepo := range apiConfig.Repositories {
			// repoID := uuid.Nil
			// if apiRepo.ID != nil {

			// 	repoID, err = uuid.Parse(*apiRepo.ID)
			// 	if err != nil {
			// 		return nil, fmt.Errorf("failed to parse repository ID: %w", err)
			// 	}
			// }
			repoID, err := parseUUIDPtr(apiRepo.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to parse repository ID: %w", err)
			}
			repositories[j] = &database.BackupRepository{
				ID:                repoID,
				Type:              database.BackupRepositoryType(apiRepo.Type),
				S3Bucket:          apiRepo.S3Bucket,
				S3Region:          apiRepo.S3Region,
				S3Endpoint:        apiRepo.S3Endpoint,
				GCSBucket:         apiRepo.GcsBucket,
				GCSEndpoint:       apiRepo.GcsEndpoint,
				AzureAccount:      apiRepo.AzureAccount,
				AzureContainer:    apiRepo.AzureContainer,
				AzureEndpoint:     apiRepo.AzureEndpoint,
				RetentionFull:     apiRepo.RetentionFull,
				RetentionFullType: parsePtr[database.RetentionFullType](apiRepo.RetentionFullType),
				BasePath:          apiRepo.BasePath,
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
		configs[i] = &database.BackupConfig{
			ID:           apiConfig.ID,
			Nodes:        apiConfig.NodeNames,
			Provider:     database.BackupProvider(apiConfig.Provider),
			Repositories: repositories,
			Schedules:    schedules,
		}
	}
	return configs, nil
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
	repoID, err := uuid.Parse(apiConfig.Repository.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository ID: %w", err)
	}
	// repoID := uuid.Nil
	// if apiConfig.Repository.ID != nil {
	// 	repoID, err := uuid.Parse(*apiConfig.Repository.ID)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to parse repository ID: %w", err)
	// 	}
	// }
	return &database.RestoreConfig{
		Provider: database.BackupProvider(apiConfig.Provider),
		Repository: database.BackupRepository{
			ID:             repoID,
			Type:           database.BackupRepositoryType(apiConfig.Repository.Type),
			S3Bucket:       apiConfig.Repository.S3Bucket,
			S3Region:       apiConfig.Repository.S3Region,
			S3Endpoint:     apiConfig.Repository.S3Endpoint,
			GCSBucket:      apiConfig.Repository.GcsBucket,
			GCSEndpoint:    apiConfig.Repository.GcsEndpoint,
			AzureAccount:   apiConfig.Repository.AzureAccount,
			AzureContainer: apiConfig.Repository.AzureContainer,
			AzureEndpoint:  apiConfig.Repository.AzureEndpoint,
			BasePath:       apiConfig.Repository.BasePath,
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
	backupConfigs, err := apiToBackupConfigs(apiSpec.BackupConfigs)
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
		BackupConfigs:      backupConfigs,
		PostgreSQLConf:     apiSpec.PostgresqlConf,
		RestoreConfig:      restoreConfig,
	}, nil
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

func stringifyPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	return utils.PointerTo(string(*v))
}

func parsePtr[T ~string](v *string) *T {
	if v == nil {
		return nil
	}
	t := T(*v)
	return &t
}
