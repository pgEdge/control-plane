package database

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/encryption"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredDatabase struct {
	storage.StoredValue
	DatabaseID string        `json:"database_id"`
	TenantID   *string       `json:"tenant_id,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	State      DatabaseState `json:"state"`
}

type DatabaseStore struct {
	client    *clientv3.Client
	encryptor encryption.Encryptor
	root      string
}

func NewDatabaseStore(client *clientv3.Client, encryptor encryption.Encryptor, root string) *DatabaseStore {
	return &DatabaseStore{
		client:    client,
		encryptor: encryptor,
		root:      root,
	}
}

func (s *DatabaseStore) Prefix() string {
	return storage.Prefix("/", s.root, "databases")
}

func (s *DatabaseStore) Key(databaseID string) string {
	return storage.Key(s.Prefix(), databaseID)
}

func (s *DatabaseStore) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *DatabaseStore) GetByKey(databaseID string) storage.GetOp[*StoredDatabase] {
	key := s.Key(databaseID)
	return storage.NewGetOpWithEncryption[*StoredDatabase](s.client, s.encryptor, key)
}

func (s *DatabaseStore) GetByKeys(databaseIDs ...string) storage.GetMultipleOp[*StoredDatabase] {
	keys := make([]string, len(databaseIDs))
	for idx, databaseID := range databaseIDs {
		keys[idx] = s.Key(databaseID)
	}
	return storage.NewGetMultipleOpWithEncryption[*StoredDatabase](s.client, s.encryptor, keys)
}

func (s *DatabaseStore) GetAll() storage.GetMultipleOp[*StoredDatabase] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOpWithEncryption[*StoredDatabase](s.client, s.encryptor, prefix)
}

func (s *DatabaseStore) Create(item *StoredDatabase) storage.PutOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewCreateOpWithEncryption(s.client, s.encryptor, key, item)
}

func (s *DatabaseStore) Update(item *StoredDatabase) storage.PutOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewUpdateOpWithEncryption(s.client, s.encryptor, key, item)
}

func (s *DatabaseStore) Delete(item *StoredDatabase) storage.DeleteValueOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewDeleteValueOp(s.client, key, item)
}
