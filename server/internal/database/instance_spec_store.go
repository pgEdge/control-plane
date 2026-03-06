package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstanceSpec struct {
	storage.StoredValue
	Spec *InstanceSpec `json:"spec"`
}

type InstanceSpecStore struct {
	client *clientv3.Client
	root   string
}

func NewInstanceSpecStore(client *clientv3.Client, root string) *InstanceSpecStore {
	return &InstanceSpecStore{
		client: client,
		root:   root,
	}
}

func (s *InstanceSpecStore) Prefix() string {
	return storage.Prefix("/", s.root, "instance_specs")
}

func (s *InstanceSpecStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *InstanceSpecStore) Key(databaseID, instanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), instanceID)
}

func (s *InstanceSpecStore) GetByKey(databaseID, instanceID string) storage.GetOp[*StoredInstanceSpec] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstanceSpec](s.client, key)
}

func (s *InstanceSpecStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredInstanceSpec] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstanceSpec](s.client, prefix)
}

func (s *InstanceSpecStore) GetAll() storage.GetMultipleOp[*StoredInstanceSpec] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredInstanceSpec](s.client, prefix)
}

func (s *InstanceSpecStore) Update(item *StoredInstanceSpec) storage.PutOp[*StoredInstanceSpec] {
	key := s.Key(item.Spec.DatabaseID, item.Spec.InstanceID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *InstanceSpecStore) DeleteByKey(databaseID, instanceID string) storage.DeleteOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *InstanceSpecStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
