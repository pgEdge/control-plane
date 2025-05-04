package database

import (
	"path"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type StoredInstanceSpec struct {
	storage.StoredValue
	InstanceSpec
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
	return path.Join("/", s.root, "instance_specs")
}

func (s *InstanceSpecStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *InstanceSpecStore) Key(databaseID, instanceID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), instanceID.String())
}

func (s *InstanceSpecStore) GetByKey(databaseID, instanceID uuid.UUID) storage.GetOp[*StoredInstanceSpec] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstanceSpec](s.client, key)
}

func (s *InstanceSpecStore) GetByKeys(databaseID uuid.UUID, instanceIDs ...uuid.UUID) storage.GetMultipleOp[*StoredInstanceSpec] {
	keys := make([]string, len(instanceIDs))
	for idx, instanceID := range instanceIDs {
		keys[idx] = s.Key(databaseID, instanceID)
	}
	return storage.NewGetMultipleOp[*StoredInstanceSpec](s.client, keys)
}

func (s *InstanceSpecStore) GetByDatabase(databaseID uuid.UUID) storage.GetMultipleOp[*StoredInstanceSpec] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredInstanceSpec](s.client, prefix)
}

func (s *InstanceSpecStore) GetAll() storage.GetMultipleOp[*StoredInstanceSpec] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredInstanceSpec](s.client, prefix)
}

func (s *InstanceSpecStore) Create(item *StoredInstanceSpec) storage.PutOp[*StoredInstanceSpec] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *InstanceSpecStore) Update(item *StoredInstanceSpec) storage.PutOp[*StoredInstanceSpec] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *InstanceSpecStore) DeleteByDatabaseID(databaseID uuid.UUID) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *InstanceSpecStore) Delete(item *StoredInstanceSpec) storage.PutOp[*StoredInstanceSpec] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}
