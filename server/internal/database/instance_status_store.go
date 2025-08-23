package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstanceStatus struct {
	storage.StoredValue
	DatabaseID string          `json:"database_id"`
	InstanceID string          `json:"instance_id"`
	Status     *InstanceStatus `json:"status"`
}

type InstanceStatusStore struct {
	client *clientv3.Client
	root   string
}

func NewInstanceStatusStore(client *clientv3.Client, root string) *InstanceStatusStore {
	return &InstanceStatusStore{
		client: client,
		root:   root,
	}
}

func (s *InstanceStatusStore) Prefix() string {
	return storage.Prefix("/", s.root, "instance_statuses")
}

func (s *InstanceStatusStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *InstanceStatusStore) Key(databaseID, instanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), instanceID)
}

func (s *InstanceStatusStore) GetByKey(databaseID, instanceID string) storage.GetOp[*StoredInstanceStatus] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstanceStatus](s.client, key)
}

func (s *InstanceStatusStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredInstanceStatus] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstanceStatus](s.client, prefix)
}

func (s *InstanceStatusStore) GetAll() storage.GetMultipleOp[*StoredInstanceStatus] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredInstanceStatus](s.client, prefix)
}

func (s *InstanceStatusStore) Put(item *StoredInstanceStatus) storage.PutOp[*StoredInstanceStatus] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *InstanceStatusStore) DeleteByKey(databaseID, instanceID string) storage.DeleteOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
