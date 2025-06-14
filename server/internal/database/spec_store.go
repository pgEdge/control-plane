package database

import (
	"path"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredSpec struct {
	storage.StoredValue
	*Spec
}

type SpecStore struct {
	client *clientv3.Client
	root   string
}

func NewSpecStore(client *clientv3.Client, root string) *SpecStore {
	return &SpecStore{
		client: client,
		root:   root,
	}
}

func (s *SpecStore) Prefix() string {
	return path.Join("/", s.root, "database_specs")
}

func (s *SpecStore) Key(databaseID string) string {
	return path.Join(s.Prefix(), databaseID)
}

func (s *SpecStore) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *SpecStore) GetByKey(databaseID string) storage.GetOp[*StoredSpec] {
	key := s.Key(databaseID)
	return storage.NewGetOp[*StoredSpec](s.client, key)
}

func (s *SpecStore) GetByKeys(databaseIDs ...string) storage.GetMultipleOp[*StoredSpec] {
	keys := make([]string, len(databaseIDs))
	for idx, databaseID := range databaseIDs {
		keys[idx] = s.Key(databaseID)
	}
	return storage.NewGetMultipleOp[*StoredSpec](s.client, keys)
}

func (s *SpecStore) GetAll() storage.GetMultipleOp[*StoredSpec] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredSpec](s.client, prefix)
}

func (s *SpecStore) Create(item *StoredSpec) storage.PutOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *SpecStore) Update(item *StoredSpec) storage.PutOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *SpecStore) Delete(item *StoredSpec) storage.DeleteValueOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewDeleteValueOp(s.client, key, item)
}
