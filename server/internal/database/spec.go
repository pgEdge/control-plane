package database

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/utils"
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
	RestoreConfig    *RestoreConfig `json:"restore_config"`
}

func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	return &Node{
		Name:             n.Name,
		HostIDs:          slices.Clone(n.HostIDs),
		PostgresVersion:  n.PostgresVersion,
		Port:             n.Port,
		StorageClass:     n.StorageClass,
		StorageSizeBytes: n.StorageSizeBytes,
		CPUs:             n.CPUs,
		MemoryBytes:      n.MemoryBytes,
		PostgreSQLConf:   maps.Clone(n.PostgreSQLConf),
		BackupConfig:     n.BackupConfig.Clone(),
		RestoreConfig:    n.RestoreConfig.Clone(),
	}
}

type User struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	DBOwner    bool     `json:"db_owner,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Roles      []string `json:"roles,omitempty"`
}

func (u *User) Clone() *User {
	if u == nil {
		return nil
	}
	return &User{
		Username:   u.Username,
		Password:   u.Password,
		DBOwner:    u.DBOwner,
		Attributes: slices.Clone(u.Attributes),
		Roles:      slices.Clone(u.Roles),
	}
}

type Extension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type BackupProvider string

const (
	BackupProviderPgBackRest BackupProvider = "pgbackrest"
	BackupProviderPgDump     BackupProvider = "pg_dump"
)

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

func (b *BackupSchedule) Clone() *BackupSchedule {
	if b == nil {
		return nil
	}
	return &BackupSchedule{
		ID:             b.ID,
		Type:           b.Type,
		CronExpression: b.CronExpression,
	}
}

type BackupConfig struct {
	Provider     BackupProvider           `json:"provider"`
	Repositories []*pgbackrest.Repository `json:"repositories"`
	Schedules    []*BackupSchedule        `json:"schedules"`
}

func (b *BackupConfig) Clone() *BackupConfig {
	if b == nil {
		return nil
	}
	repos := make([]*pgbackrest.Repository, len(b.Repositories))
	for i, repo := range b.Repositories {
		repos[i] = repo.Clone()
	}
	schedules := make([]*BackupSchedule, len(b.Schedules))
	for i, schedule := range b.Schedules {
		schedules[i] = schedule.Clone()
	}

	return &BackupConfig{
		Provider:     b.Provider,
		Repositories: repos,
		Schedules:    schedules,
	}
}

type RestoreConfig struct {
	Provider           BackupProvider         `json:"provider"`
	SourceDatabaseID   uuid.UUID              `json:"source_database_id"`
	SourceNodeName     string                 `json:"source_node_name"`
	SourceDatabaseName string                 `json:"source_database_name"`
	Repository         *pgbackrest.Repository `json:"repository"`
	RestoreOptions     []string               `json:"restore_options"`
}

func (r *RestoreConfig) Clone() *RestoreConfig {
	if r == nil {
		return nil
	}
	return &RestoreConfig{
		Provider:           r.Provider,
		SourceDatabaseID:   r.SourceDatabaseID,
		SourceNodeName:     r.SourceNodeName,
		SourceDatabaseName: r.SourceDatabaseName,
		Repository:         r.Repository.Clone(),
		RestoreOptions:     slices.Clone(r.RestoreOptions),
	}
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

func (s *Spec) Node(name string) (*Node, error) {
	for _, node := range s.Nodes {
		if node.Name == name {
			return node, nil
		}
	}
	return nil, fmt.Errorf("node %s not found in spec", name)
}

func (s *Spec) NodeNames() []string {
	names := make([]string, len(s.Nodes))
	for i, node := range s.Nodes {
		names[i] = node.Name
	}
	return names
}

func (s *Spec) Clone() *Spec {
	nodes := make([]*Node, len(s.Nodes))
	for i, node := range s.Nodes {
		nodes[i] = node.Clone()
	}
	users := make([]*User, len(s.DatabaseUsers))
	for i, user := range s.DatabaseUsers {
		users[i] = user.Clone()
	}

	return &Spec{
		DatabaseID:         s.DatabaseID,
		TenantID:           utils.ClonePointer(s.TenantID),
		DatabaseName:       s.DatabaseName,
		PostgresVersion:    s.PostgresVersion,
		SpockVersion:       s.SpockVersion,
		Port:               s.Port,
		DeletionProtection: s.DeletionProtection,
		StorageClass:       s.StorageClass,
		StorageSizeBytes:   s.StorageSizeBytes,
		CPUs:               s.CPUs,
		MemoryBytes:        s.MemoryBytes,
		Features:           maps.Clone(s.Features),
		PostgreSQLConf:     maps.Clone(s.PostgreSQLConf),
		Nodes:              nodes,
		DatabaseUsers:      users,
		BackupConfig:       s.BackupConfig.Clone(),
		RestoreConfig:      s.RestoreConfig.Clone(),
	}
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
	EnableBackups    bool                `json:"enable_backups"`
	ClusterSize      int                 `json:"cluster_size"`
}

func (i *InstanceSpec) Hostname() string {
	return fmt.Sprintf("postgres-%s-%s", i.NodeName, i.InstanceID)
}

func (i *InstanceSpec) HostnameWithDomain() string {
	return fmt.Sprintf("%s.%s-database", i.Hostname(), i.DatabaseID)
}

func (i *InstanceSpec) UsesPgBackRest() bool {
	return i.BackupConfig != nil && i.BackupConfig.Provider == BackupProviderPgBackRest
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

	clusterSize := len(s.Nodes)
	nodes := make([]*NodeInstances, clusterSize)
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
		restoreConfig := s.RestoreConfig
		if node.RestoreConfig != nil {
			restoreConfig = node.RestoreConfig
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
				RestoreConfig:    restoreConfig,
				PostgreSQLConf:   postgresqlConf,
				// By default, we'll choose the last host in the list to run
				// backups. We'll want to incorporate the current state of the
				// cluster into this decision when we implement updates.
				EnableBackups: backupConfig != nil && hostIdx == len(node.HostIDs)-1,
				ClusterSize:   clusterSize,
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
