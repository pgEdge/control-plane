package database

import (
	"path"
	"time"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstance struct {
	storage.StoredValue
	InstanceID uuid.UUID     `json:"instance_id"`
	DatabaseID uuid.UUID     `json:"database_id"`
	HostID     uuid.UUID     `json:"host_id"`
	NodeName   string        `json:"node_name"`
	State      InstanceState `json:"state"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdateAt   time.Time     `json:"updated_at"`
	Error      string        `json:"error,omitempty"`
}

type InstanceUpdateOptions struct {
	InstanceID uuid.UUID     `json:"instance_id"`
	DatabaseID uuid.UUID     `json:"database_id"`
	HostID     uuid.UUID     `json:"host_id"`
	NodeName   string        `json:"node_name"`
	State      InstanceState `json:"state"`
	Error      string        `json:"error,omitempty"`
}

func NewStoredInstance(opts *InstanceUpdateOptions) *StoredInstance {
	now := time.Now()
	return &StoredInstance{
		InstanceID: opts.InstanceID,
		DatabaseID: opts.DatabaseID,
		HostID:     opts.HostID,
		NodeName:   opts.NodeName,
		State:      opts.State,
		CreatedAt:  now,
		UpdateAt:   now,
		Error:      opts.Error,
	}
}

func (i *StoredInstance) Update(opts *InstanceUpdateOptions) {
	i.State = opts.State
	i.Error = opts.Error
	i.UpdateAt = time.Now()
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
	return path.Join("/", s.root, "instances")
}

func (s *InstanceStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *InstanceStore) Key(databaseID, instanceID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), instanceID.String())
}

func (s *InstanceStore) GetByKey(databaseID, instanceID uuid.UUID) storage.GetOp[*StoredInstance] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstance](s.client, key)
}

func (s *InstanceStore) GetByDatabaseID(databaseID uuid.UUID) storage.GetMultipleOp[*StoredInstance] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstance](s.client, prefix)
}

func (s *InstanceStore) Put(item *StoredInstance) storage.PutOp[*StoredInstance] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *InstanceStore) DeleteByKey(databaseID, instanceID uuid.UUID) storage.DeleteOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
