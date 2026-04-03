package database

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstance struct {
	storage.StoredValue
	InstanceID    string            `json:"instance_id"`
	DatabaseID    string            `json:"database_id"`
	HostID        string            `json:"host_id"`
	NodeName      string            `json:"node_name"`
	State         InstanceState     `json:"state"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdateAt      time.Time         `json:"updated_at"`
	Port          *int              `json:"port"`
	PatroniPort   *int              `json:"patroni_port"`
	PgEdgeVersion *ds.PgEdgeVersion `json:"pgedge_version"`
	Error         string            `json:"error,omitempty"`
}

type InstanceUpdateOptions struct {
	InstanceID    string            `json:"instance_id"`
	DatabaseID    string            `json:"database_id"`
	HostID        string            `json:"host_id"`
	NodeName      string            `json:"node_name"`
	State         InstanceState     `json:"state"`
	Port          *int              `json:"port"`
	PatroniPort   *int              `json:"patroni_port"`
	PgEdgeVersion *ds.PgEdgeVersion `json:"pgedge_version"`
	Error         string            `json:"error,omitempty"`
	Now           time.Time         `json:"now"`
}

func (o *InstanceUpdateOptions) now() time.Time {
	if !o.Now.IsZero() {
		return o.Now
	}
	return time.Now()
}

func NewStoredInstance(opts *InstanceUpdateOptions) *StoredInstance {
	now := opts.now()
	return &StoredInstance{
		InstanceID:    opts.InstanceID,
		DatabaseID:    opts.DatabaseID,
		HostID:        opts.HostID,
		NodeName:      opts.NodeName,
		State:         opts.State,
		Port:          opts.Port,
		PatroniPort:   opts.PatroniPort,
		PgEdgeVersion: opts.PgEdgeVersion,
		CreatedAt:     now,
		UpdateAt:      now,
		Error:         opts.Error,
	}
}

func (i *StoredInstance) Update(opts *InstanceUpdateOptions) {
	i.Port = opts.Port
	i.PatroniPort = opts.PatroniPort
	i.PgEdgeVersion = opts.PgEdgeVersion
	i.State = opts.State
	i.Error = opts.Error
	i.UpdateAt = opts.now()
}

type InstanceStateUpdateOptions struct {
	InstanceID string        `json:"instance_id"`
	DatabaseID string        `json:"database_id"`
	HostID     string        `json:"host_id"`
	NodeName   string        `json:"node_name"`
	State      InstanceState `json:"state"`
	Error      string        `json:"error,omitempty"`
	Now        time.Time     `json:"now"`
}

func (o *InstanceStateUpdateOptions) now() time.Time {
	if !o.Now.IsZero() {
		return o.Now
	}
	return time.Now()
}

func (i *StoredInstance) UpdateState(opts *InstanceStateUpdateOptions) {
	i.State = opts.State
	i.Error = opts.Error
	i.UpdateAt = opts.now()
}

type InstanceStore struct {
	client *clientv3.Client
	root   string
}

func NewInstanceStore(client *clientv3.Client, root string) *InstanceStore {
	return &InstanceStore{
		client: client,
		root:   root,
	}
}

func (s *InstanceStore) Prefix() string {
	return storage.Prefix("/", s.root, "instances")
}

func (s *InstanceStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *InstanceStore) Key(databaseID, instanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), instanceID)
}

func (s *InstanceStore) GetByKey(databaseID, instanceID string) storage.GetOp[*StoredInstance] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstance](s.client, key)
}

func (s *InstanceStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredInstance] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstance](s.client, prefix)
}

func (s *InstanceStore) GetAll() storage.GetMultipleOp[*StoredInstance] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredInstance](s.client, prefix)
}

func (s *InstanceStore) Put(item *StoredInstance) storage.PutOp[*StoredInstance] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *InstanceStore) DeleteByKey(databaseID, instanceID string) storage.DeleteOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *InstanceStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
