package database

import (
	"path"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstanceStatus struct {
	storage.StoredValue
	DatabaseID uuid.UUID       `json:"database_id"`
	InstanceID uuid.UUID       `json:"instance_id"`
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
	return path.Join("/", s.root, "instance_statuses")
}

func (s *InstanceStatusStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *InstanceStatusStore) Key(databaseID, instanceID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), instanceID.String())
}

func (s *InstanceStatusStore) GetByKey(databaseID, instanceID uuid.UUID) storage.GetOp[*StoredInstanceStatus] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstanceStatus](s.client, key)
}

func (s *InstanceStatusStore) GetByDatabaseID(databaseID uuid.UUID) storage.GetMultipleOp[*StoredInstanceStatus] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstanceStatus](s.client, prefix)
}

func (s *InstanceStatusStore) Put(item *StoredInstanceStatus) storage.PutOp[*StoredInstanceStatus] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *InstanceStatusStore) DeleteByKey(databaseID, instanceID uuid.UUID) storage.DeleteOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
