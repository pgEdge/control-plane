package database

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strconv"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var ErrNodeNotInDBSpec = errors.New("node not in db spec")

type ExtraVolumesSpec struct {
	HostPath        string `json:"host_path"`
	DestinationPath string `json:"destination_path"`
}
type ExtraNetworkSpec struct {
	ID         string            `json:"id"`                    // required
	Aliases    []string          `json:"aliases,omitempty"`     // optional
	DriverOpts map[string]string `json:"driver_opts,omitempty"` // optional
}

type SwarmOpts struct {
	ExtraVolumes  []ExtraVolumesSpec `json:"extra_volumes,omitempty"`
	ExtraNetworks []ExtraNetworkSpec `json:"extra_networks,omitempty"`
	ExtraLabels   map[string]string  `json:"extra_labels,omitempty"` // optional, used for custom labels on the swarm service
}
type OrchestratorOpts struct {
	Swarm *SwarmOpts `json:"docker,omitempty"`
}

type Node struct {
	Name             string            `json:"name"`
	HostIDs          []string          `json:"host_ids"`
	PostgresVersion  string            `json:"postgres_version"`
	Port             *int              `json:"port"`
	CPUs             float64           `json:"cpus"`
	MemoryBytes      uint64            `json:"memory"`
	PostgreSQLConf   map[string]any    `json:"postgresql_conf"`
	BackupConfig     *BackupConfig     `json:"backup_config"`
	RestoreConfig    *RestoreConfig    `json:"restore_config"`
	OrchestratorOpts *OrchestratorOpts `json:"orchestrator_opts,omitempty"`
	FailoverPolicy   string            `json:"failover_policy"`
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
		CPUs:             n.CPUs,
		MemoryBytes:      n.MemoryBytes,
		PostgreSQLConf:   maps.Clone(n.PostgreSQLConf),
		BackupConfig:     n.BackupConfig.Clone(),
		RestoreConfig:    n.RestoreConfig.Clone(),
		OrchestratorOpts: n.OrchestratorOpts.Clone(),
		FailoverPolicy:   n.FailoverPolicy,
	}
}

// DefaultSensitiveFieldsFrom will default this node's sensitive fields to the
// values from the given node.
func (n *Node) DefaultSensitiveFieldsFrom(other *Node) {
	if n.BackupConfig != nil && other.BackupConfig != nil {
		n.BackupConfig.DefaultSensitiveFieldsFrom(other.BackupConfig)
	}
	if n.RestoreConfig != nil && other.RestoreConfig != nil {
		n.RestoreConfig.DefaultSensitiveFieldsFrom(other.RestoreConfig)
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

// DefaultSensitiveFieldsFrom will default this user's sensitive fields to the
// values from the given user.
func (u *User) DefaultSensitiveFieldsFrom(other *User) {
	if u.Password == "" {
		u.Password = other.Password
	}
}

type Extension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	Repositories []*pgbackrest.Repository `json:"repositories"`
	Schedules    []*BackupSchedule        `json:"schedules"`
}

func (b *BackupConfig) Clone() *BackupConfig {
	if b == nil {
		return nil
	}
	var repos []*pgbackrest.Repository
	if len(b.Repositories) > 0 {
		repos = make([]*pgbackrest.Repository, len(b.Repositories))
		for i, repo := range b.Repositories {
			repos[i] = repo.Clone()
		}
	}
	var schedules []*BackupSchedule
	if len(b.Schedules) > 0 {
		schedules = make([]*BackupSchedule, len(b.Schedules))
		for i, schedule := range b.Schedules {
			schedules[i] = schedule.Clone()
		}
	}

	return &BackupConfig{
		Repositories: repos,
		Schedules:    schedules,
	}
}

// DefaultSensitiveFieldsFrom will default this config's sensitive fields to the
// values from the given config.
func (b *BackupConfig) DefaultSensitiveFieldsFrom(other *BackupConfig) {
	otherRepositoriesByID := make(map[string]*pgbackrest.Repository, len(other.Repositories))
	for _, r := range other.Repositories {
		otherRepositoriesByID[r.Identifier()] = r
	}

	for _, r := range b.Repositories {
		otherRepo, ok := otherRepositoriesByID[r.Identifier()]
		if ok {
			r.DefaultSensitiveFieldsFrom(otherRepo)
		}
	}
}

type RestoreConfig struct {
	SourceDatabaseID   string                 `json:"source_database_id"`
	SourceNodeName     string                 `json:"source_node_name"`
	SourceDatabaseName string                 `json:"source_database_name"`
	Repository         *pgbackrest.Repository `json:"repository"`
	RestoreOptions     map[string]string      `json:"restore_options"`
}

// DefaultSensitiveFieldsFrom will default this config's sensitive fields to the
// values from the given config.
func (r *RestoreConfig) DefaultSensitiveFieldsFrom(other *RestoreConfig) {
	if r.Repository != nil && other.Repository != nil {
		r.Repository.DefaultSensitiveFieldsFrom(other.Repository)
	}
}

func (r *RestoreConfig) Clone() *RestoreConfig {
	if r == nil {
		return nil
	}
	return &RestoreConfig{
		SourceDatabaseID:   r.SourceDatabaseID,
		SourceNodeName:     r.SourceNodeName,
		SourceDatabaseName: r.SourceDatabaseName,
		Repository:         r.Repository.Clone(),
		RestoreOptions:     maps.Clone(r.RestoreOptions),
	}
}

func (o *OrchestratorOpts) Clone() *OrchestratorOpts {
	if o == nil {
		return nil
	}
	return &OrchestratorOpts{
		Swarm: o.Swarm.Clone(),
	}
}

func (d *SwarmOpts) Clone() *SwarmOpts {
	if d == nil {
		return nil
	}
	clonedVolumes := make([]ExtraVolumesSpec, len(d.ExtraVolumes))
	copy(clonedVolumes, d.ExtraVolumes)

	clonedNetworks := make([]ExtraNetworkSpec, len(d.ExtraNetworks))
	for i, net := range d.ExtraNetworks {
		clonedNetworks[i] = ExtraNetworkSpec{
			ID:         net.ID,
			Aliases:    slices.Clone(net.Aliases),
			DriverOpts: maps.Clone(net.DriverOpts),
		}
	}

	return &SwarmOpts{
		ExtraVolumes:  clonedVolumes,
		ExtraNetworks: clonedNetworks,
		ExtraLabels:   maps.Clone(d.ExtraLabels),
	}
}

type Spec struct {
	DatabaseID       string            `json:"database_id"`
	TenantID         *string           `json:"tenant_id,omitempty"`
	DatabaseName     string            `json:"database_name"`
	PostgresVersion  string            `json:"postgres_version"`
	SpockVersion     string            `json:"spock_version"`
	Port             *int              `json:"port"`
	CPUs             float64           `json:"cpus"`
	MemoryBytes      uint64            `json:"memory"`
	Nodes            []*Node           `json:"nodes"`
	DatabaseUsers    []*User           `json:"database_users"`
	BackupConfig     *BackupConfig     `json:"backup_config"`
	RestoreConfig    *RestoreConfig    `json:"restore_config"`
	PostgreSQLConf   map[string]any    `json:"postgresql_conf"`
	OrchestratorOpts *OrchestratorOpts `json:"orchestrator_opts,omitempty"`
	FailoverPolicy   string            `json:"failover_policy"`
}

func (s *Spec) Node(name string) (*Node, error) {
	for _, node := range s.Nodes {
		if node.Name == name {
			return node, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNodeNotInDBSpec, name)
}

func (s *Spec) ValidateNodeNames(names ...string) error {
	existing := ds.NewSet(s.NodeNames()...)
	invalid := ds.NewSet(names...).Difference(existing)
	if invalid.Size() > 0 {
		return fmt.Errorf("%w: %v", ErrNodeNotInDBSpec, invalid.ToSlice())
	}
	return nil
}

func (s *Spec) NodeNames() []string {
	names := make([]string, len(s.Nodes))
	for i, node := range s.Nodes {
		names[i] = node.Name
	}
	return names
}

// NormalizeBackupConfig normalizes the backup config so that its defined
// per-node rather than at the database level. This is useful as a preliminary
// step if we need to modify the backup configs on the user's behalf.
func (s *Spec) NormalizeBackupConfig() {
	if s.BackupConfig == nil {
		return
	}
	for i := range s.Nodes {
		if s.Nodes[i].BackupConfig == nil {
			s.Nodes[i].BackupConfig = s.BackupConfig
		}
	}
	s.BackupConfig = nil
}

// RemoveBackupConfigFrom removes backup configuration from the given nodes. It
// normalizes the backup configuration first to ensure that only the given nodes
// are affected.
func (s *Spec) RemoveBackupConfigFrom(nodes ...string) {
	s.NormalizeBackupConfig()

	for i, n := range s.Nodes {
		if slices.Contains(nodes, n.Name) {
			s.Nodes[i].BackupConfig = nil
		}
	}
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
		DatabaseID:       s.DatabaseID,
		TenantID:         utils.ClonePointer(s.TenantID),
		DatabaseName:     s.DatabaseName,
		PostgresVersion:  s.PostgresVersion,
		SpockVersion:     s.SpockVersion,
		Port:             s.Port,
		CPUs:             s.CPUs,
		MemoryBytes:      s.MemoryBytes,
		PostgreSQLConf:   maps.Clone(s.PostgreSQLConf),
		Nodes:            nodes,
		DatabaseUsers:    users,
		BackupConfig:     s.BackupConfig.Clone(),
		RestoreConfig:    s.RestoreConfig.Clone(),
		OrchestratorOpts: s.OrchestratorOpts.Clone(),
		FailoverPolicy:   s.FailoverPolicy,
	}
}

// DefaultSensitiveFieldsFrom will default this spec's sensitive fields to the
// values from the given spec.
func (s *Spec) DefaultSensitiveFieldsFrom(other *Spec) {
	s.defaultSensitiveFieldFromNodes(other.Nodes)
	s.defaultSensitiveFieldFromUsers(other.DatabaseUsers)

	if s.BackupConfig != nil && other.BackupConfig != nil {
		s.BackupConfig.DefaultSensitiveFieldsFrom(other.BackupConfig)
	}
	if s.RestoreConfig != nil && other.RestoreConfig != nil {
		s.RestoreConfig.DefaultSensitiveFieldsFrom(other.RestoreConfig)
	}
}

func (s Spec) defaultSensitiveFieldFromNodes(other []*Node) {
	otherNodesByName := make(map[string]*Node)
	for _, n := range other {
		otherNodesByName[n.Name] = n
	}

	for _, n := range s.Nodes {
		otherNode, ok := otherNodesByName[n.Name]
		if ok {
			n.DefaultSensitiveFieldsFrom(otherNode)
		}
	}
}

func (s Spec) defaultSensitiveFieldFromUsers(other []*User) {
	otherUsersByName := make(map[string]*User)
	for _, u := range other {
		otherUsersByName[u.Username] = u
	}

	for _, u := range s.DatabaseUsers {
		otherUser, ok := otherUsersByName[u.Username]
		if ok {
			u.DefaultSensitiveFieldsFrom(otherUser)
		}
	}
}

func InstanceIDFor(hostID, databaseID, nodeName string) string {
	// We're using a shortened hash of the host ID to strike a compromise
	// between readability and global uniqueness.
	// Example outputs:
	// - Input:
	//   	hostID:     "dbf5779c-492a-11f0-b11a-1b8cb15693a8"
	//		databaseID: "ed2362ea-492a-11f0-956c-9f2171e8b9ab"
	//		nodeName:   "n1"
	//   Output: "ed2362ea-492a-11f0-956c-9f2171e8b9ab-n1-io5979nh"
	// - Input:
	//   	hostID:     "us-east-1"
	//		databaseID: "my-app"
	//		nodeName:   "n1"
	//   Output: "my-app-n1-n5fe2mcy"
	hash := sha1.Sum([]byte(hostID))
	base36 := new(big.Int).
		SetBytes(hash[:]).
		Text(36)

	return databaseID + "-" + nodeName + "-" + base36[:8]
}

type InstanceSpec struct {
	InstanceID       string              `json:"instance_id"`
	TenantID         *string             `json:"tenant_id,omitempty"`
	DatabaseID       string              `json:"database_id"`
	HostID           string              `json:"host_id"`
	DatabaseName     string              `json:"database_name"`
	NodeName         string              `json:"node_name"`
	NodeOrdinal      int                 `json:"node_ordinal"`
	PgEdgeVersion    *host.PgEdgeVersion `json:"pg_edge_version"`
	Port             *int                `json:"port"`
	CPUs             float64             `json:"cpus"`
	MemoryBytes      uint64              `json:"memory"`
	DatabaseUsers    []*User             `json:"database_users"`
	BackupConfig     *BackupConfig       `json:"backup_config"`
	RestoreConfig    *RestoreConfig      `json:"restore_config"`
	PostgreSQLConf   map[string]any      `json:"postgresql_conf"`
	EnableBackups    bool                `json:"enable_backups"`
	ClusterSize      int                 `json:"cluster_size"`
	OrchestratorOpts *OrchestratorOpts   `json:"orchestrator_opts,omitempty"`
	FailoverPolicy   string              `json:"failover_policy"`
}

type NodeInstances struct {
	NodeName  string          `json:"node_name"`
	Instances []*InstanceSpec `json:"instances"`
}

func (n *NodeInstances) InstanceIDs() []string {
	instanceIDs := make([]string, len(n.Instances))
	for i, instance := range n.Instances {
		instanceIDs[i] = instance.InstanceID
	}
	slices.Sort(instanceIDs)
	return instanceIDs
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
		if node.Port != nil {
			port = node.Port
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
		postgresqlConf := maps.Clone(s.PostgreSQLConf)
		if node.PostgreSQLConf != nil {
			postgresqlConf = maps.Clone(node.PostgreSQLConf)
		}
		orchestratorOpts := s.OrchestratorOpts
		if node.OrchestratorOpts != nil {
			orchestratorOpts = node.OrchestratorOpts
		}
		failoverPolicy := s.FailoverPolicy
		if node.FailoverPolicy != "" {
			failoverPolicy = node.FailoverPolicy
		}

		instances := make([]*InstanceSpec, len(node.HostIDs))
		for hostIdx, hostID := range node.HostIDs {
			instances[hostIdx] = &InstanceSpec{
				InstanceID:     InstanceIDFor(hostID, s.DatabaseID, node.Name),
				TenantID:       s.TenantID,
				DatabaseID:     s.DatabaseID,
				HostID:         hostID,
				DatabaseName:   s.DatabaseName,
				NodeName:       node.Name,
				NodeOrdinal:    nodeOrdinal,
				PgEdgeVersion:  nodeVersion,
				Port:           port,
				CPUs:           cpus,
				MemoryBytes:    memoryBytes,
				DatabaseUsers:  s.DatabaseUsers,
				BackupConfig:   backupConfig,
				RestoreConfig:  restoreConfig,
				PostgreSQLConf: postgresqlConf,
				// By default, we'll choose the last host in the list to run
				// backups. We'll want to incorporate the current state of the
				// cluster into this decision when we implement updates.
				EnableBackups:    backupConfig != nil && hostIdx == len(node.HostIDs)-1,
				ClusterSize:      clusterSize,
				OrchestratorOpts: orchestratorOpts.Clone(),
				FailoverPolicy:   failoverPolicy,
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
