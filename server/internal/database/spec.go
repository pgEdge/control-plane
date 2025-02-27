package database

import (
	"errors"
	"fmt"
	"maps"
	"strconv"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/host"
)

var ErrHostNotInDBSpec = errors.New("host not in db spec")

type Node struct {
	Name             string         `json:"name"`
	HostIDs          []uuid.UUID    `json:"host_ids"`
	PostgresVersion  string         `json:"postgres_version"`
	Port             int            `json:"port"`
	StorageClass     string         `json:"storage_class"`
	StorageSizeBytes uint64         `json:"storage_size"`
	CPUs             float64        `json:"cpus"`
	MemoryBytes      uint64         `json:"memory"`
	PostgreSQLConf   map[string]any `json:"postgresql_conf"`
	BackupConfig     *BackupConfig  `json:"backup_config"`
}

type User struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	DBOwner    bool     `json:"db_owner,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Roles      []string `json:"roles,omitempty"`
}

type Extension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type BackupProvider string

const (
	BackupProviderPgBackrest BackupProvider = "pgbackrest"
	BackupProviderPgDump     BackupProvider = "pg_dump"
)

type BackupRepositoryType string

const (
	BackupRepositoryTypeS3    BackupRepositoryType = "s3"
	BackupRepositoryTypeGCS   BackupRepositoryType = "gcs"
	BackupRepositoryTypeAzure BackupRepositoryType = "azure"
)

type RetentionFullType string

const (
	RetentionFullTypeTime  RetentionFullType = "time"
	RetentionFullTypeCount RetentionFullType = "count"
)

type BackupRepository struct {
	ID                uuid.UUID            `json:"id"`
	Type              BackupRepositoryType `json:"type"`
	S3Bucket          *string              `json:"s3_bucket,omitempty"`
	S3Region          *string              `json:"s3_region,omitempty"`
	S3Endpoint        *string              `json:"s3_endpoint,omitempty"`
	GCSBucket         *string              `json:"gcs_bucket,omitempty"`
	GCSEndpoint       *string              `json:"gcs_endpoint,omitempty"`
	AzureAccount      *string              `json:"azure_account,omitempty"`
	AzureContainer    *string              `json:"azure_container,omitempty"`
	AzureEndpoint     *string              `json:"azure_endpoint,omitempty"`
	RetentionFull     *int                 `json:"retention_full"`
	RetentionFullType *RetentionFullType   `json:"retention_full_type"`
	BasePath          *string              `json:"base_path,omitempty"`
}

type BackupScheduleType string

const (
	BackupScheduleTypeFull        BackupScheduleType = "full"
	BackupScheduleTypeIncremental BackupScheduleType = "incr"
)

type BackupSchedule struct {
	ID             string             `json:"id"`
	Type           BackupScheduleType `json:"type"`
	CronExpression string             `json:"cron_expression"`
}

type BackupConfig struct {
	ID           string              `json:"id"`
	Provider     BackupProvider      `json:"provider"`
	Repositories []*BackupRepository `json:"repositories"`
	Schedules    []*BackupSchedule   `json:"schedules"`
}

type RestoreConfig struct {
	Provider   BackupProvider   `json:"provider"`
	Repository BackupRepository `json:"repository"`
}

type Spec struct {
	DatabaseID         uuid.UUID         `json:"database_id"`
	TenantID           *uuid.UUID        `json:"tenant_id,omitempty"`
	DatabaseName       string            `json:"database_name"`
	PostgresVersion    string            `json:"postgres_version"`
	SpockVersion       string            `json:"spock_version"`
	Port               int               `json:"port"`
	DeletionProtection bool              `json:"deletion_protection"`
	StorageClass       string            `json:"storage_class"`
	StorageSizeBytes   uint64            `json:"storage_size"`
	CPUs               float64           `json:"cpus"`
	MemoryBytes        uint64            `json:"memory"`
	Nodes              []*Node           `json:"nodes"`
	DatabaseUsers      []*User           `json:"database_users"`
	Features           map[string]string `json:"features"`
	BackupConfig       *BackupConfig     `json:"backup_config"`
	RestoreConfig      *RestoreConfig    `json:"restore_config"`
	PostgreSQLConf     map[string]any    `json:"postgresql_conf"`
}

func InstanceIDFor(hostID, databaseID uuid.UUID, nodeName string) uuid.UUID {
	space := uuid.UUID(hostID)
	data := []byte(databaseID.String() + ":" + nodeName)
	return uuid.NewSHA1(space, data)
}

type InstanceSpec struct {
	InstanceID       uuid.UUID           `json:"instance_id"`
	TenantID         *uuid.UUID          `json:"tenant_id,omitempty"`
	DatabaseID       uuid.UUID           `json:"database_id"`
	HostID           uuid.UUID           `json:"host_id"`
	DatabaseName     string              `json:"database_name"`
	NodeName         string              `json:"node_name"`
	NodeOrdinal      int                 `json:"node_ordinal"`
	PgEdgeVersion    *host.PgEdgeVersion `json:"pg_edge_version"`
	Port             int                 `json:"port"`
	StorageClass     string              `json:"storage_class"`
	StorageSizeBytes uint64              `json:"storage_size"`
	CPUs             float64             `json:"cpus"`
	MemoryBytes      uint64              `json:"memory"`
	DatabaseUsers    []*User             `json:"database_users"`
	Features         map[string]string   `json:"features"`
	BackupConfig     *BackupConfig       `json:"backup_config"`
	RestoreConfig    *RestoreConfig      `json:"restore_config"`
	PostgreSQLConf   map[string]any      `json:"postgresql_conf"`
}

func (i *InstanceSpec) Hostname() string {
	return fmt.Sprintf("postgres-%s-%s", i.NodeName, i.InstanceID)
}

func (i *InstanceSpec) HostnameWithDomain() string {
	return fmt.Sprintf("%s.%s-database", i.Hostname(), i.DatabaseID)
}

type NodeInstances struct {
	NodeName  string          `json:"node_name"`
	Instances []*InstanceSpec `json:"instances"`
}

func (s *Spec) NodeInstances() ([]*NodeInstances, error) {
	specVersion, err := host.NewPgEdgeVersion(s.PostgresVersion, s.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse version from spec: %w", err)
	}

	nodes := make([]*NodeInstances, len(s.Nodes))
	for nodeIdx, node := range s.Nodes {
		nodeOrdinal, err := extractOrdinal(node.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to extract ordinal from node name: %w", err)
		}

		// Respect node-level overrides
		nodeVersion := specVersion
		if node.PostgresVersion != "" {
			nodeVersion, err = host.NewPgEdgeVersion(node.PostgresVersion, s.SpockVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to parse version from node spec: %w", err)
			}
		}
		port := s.Port
		if node.Port > 0 {
			port = node.Port
		}
		storageClass := s.StorageClass
		if node.StorageClass != "" {
			storageClass = node.StorageClass
		}
		storageSizeBytes := s.StorageSizeBytes
		if node.StorageSizeBytes > 0 {
			storageSizeBytes = node.StorageSizeBytes
		}
		cpus := s.CPUs
		if node.CPUs > 0 {
			cpus = node.CPUs
		}
		memoryBytes := s.MemoryBytes
		if node.MemoryBytes > 0 {
			memoryBytes = node.MemoryBytes
		}
		backupConfig := s.BackupConfig
		if node.BackupConfig != nil {
			backupConfig = node.BackupConfig
		}
		// Create a merged PostgreSQL configuration with node-level overrides
		postgresqlConf := maps.Clone(s.PostgreSQLConf)
		maps.Copy(node.PostgreSQLConf, postgresqlConf)

		instances := make([]*InstanceSpec, len(node.HostIDs))
		for hostIdx, hostID := range node.HostIDs {
			instances[hostIdx] = &InstanceSpec{
				InstanceID:       InstanceIDFor(hostID, s.DatabaseID, node.Name),
				TenantID:         s.TenantID,
				DatabaseID:       s.DatabaseID,
				HostID:           hostID,
				DatabaseName:     s.DatabaseName,
				NodeName:         node.Name,
				NodeOrdinal:      nodeOrdinal,
				PgEdgeVersion:    nodeVersion,
				Port:             port,
				StorageClass:     storageClass,
				StorageSizeBytes: storageSizeBytes,
				CPUs:             cpus,
				MemoryBytes:      memoryBytes,
				DatabaseUsers:    s.DatabaseUsers,
				Features:         s.Features,
				BackupConfig:     backupConfig,
				RestoreConfig:    s.RestoreConfig,
				PostgreSQLConf:   postgresqlConf,
			}
		}

		nodes[nodeIdx] = &NodeInstances{
			NodeName:  node.Name,
			Instances: instances,
		}
	}
	return nodes, nil
}

func extractOrdinal(name string) (int, error) {
	if len(name) < 2 {
		return 0, fmt.Errorf("invalid name: %s", name)
	}
	ordinal, err := strconv.Atoi(name[1:])
	if err != nil {
		return 0, fmt.Errorf("invalid name: %s", name)
	}
	return ordinal, nil
}
