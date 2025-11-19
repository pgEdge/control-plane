package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/encryption"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredSpec struct {
	storage.StoredValue
	*Spec
}

type SpecStore struct {
	client    *clientv3.Client
	encryptor encryption.Encryptor
	root      string
}

func NewSpecStore(client *clientv3.Client, encryptor encryption.Encryptor, root string) *SpecStore {
	return &SpecStore{
		client:    client,
		encryptor: encryptor,
		root:      root,
	}
}

func (s *SpecStore) Prefix() string {
	return storage.Prefix("/", s.root, "database_specs")
}

func (s *SpecStore) Key(databaseID string) string {
	return storage.Key(s.Prefix(), databaseID)
}

func (s *SpecStore) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *SpecStore) GetByKey(databaseID string) storage.GetOp[*StoredSpec] {
	key := s.Key(databaseID)
	return storage.NewGetOpWithEncryption[*StoredSpec](s.client, s.encryptor, key)
}

func (s *SpecStore) GetByKeys(databaseIDs ...string) storage.GetMultipleOp[*StoredSpec] {
	keys := make([]string, len(databaseIDs))
	for idx, databaseID := range databaseIDs {
		keys[idx] = s.Key(databaseID)
	}
	return storage.NewGetMultipleOpWithEncryption[*StoredSpec](s.client, s.encryptor, keys)
}

func (s *SpecStore) GetAll() storage.GetMultipleOp[*StoredSpec] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOpWithEncryption[*StoredSpec](s.client, s.encryptor, prefix)
}

func (s *SpecStore) Create(item *StoredSpec) storage.PutOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewCreateOpWithEncryption(s.client, s.encryptor, key, item)
}

func (s *SpecStore) Update(item *StoredSpec) storage.PutOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewUpdateOpWithEncryption(s.client, s.encryptor, key, item)
}

func (s *SpecStore) Delete(item *StoredSpec) storage.DeleteValueOp[*StoredSpec] {
	key := s.Key(item.DatabaseID)
	return storage.NewDeleteValueOp(s.client, key, item)
}
