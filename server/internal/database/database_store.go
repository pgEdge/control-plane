package database

import (
	"path"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

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
	client *clientv3.Client
	root   string
}

func NewDatabaseStore(client *clientv3.Client, root string) *DatabaseStore {
	return &DatabaseStore{
		client: client,
		root:   root,
	}
}

func (s *DatabaseStore) Prefix() string {
	return path.Join("/", s.root, "databases")
}

func (s *DatabaseStore) Key(databaseID string) string {
	return path.Join(s.Prefix(), databaseID)
}

func (s *DatabaseStore) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *DatabaseStore) GetByKey(databaseID string) storage.GetOp[*StoredDatabase] {
	key := s.Key(databaseID)
	return storage.NewGetOp[*StoredDatabase](s.client, key)
}

func (s *DatabaseStore) GetByKeys(databaseIDs ...string) storage.GetMultipleOp[*StoredDatabase] {
	keys := make([]string, len(databaseIDs))
	for idx, databaseID := range databaseIDs {
		keys[idx] = s.Key(databaseID)
	}
	return storage.NewGetMultipleOp[*StoredDatabase](s.client, keys)
}

func (s *DatabaseStore) GetAll() storage.GetMultipleOp[*StoredDatabase] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredDatabase](s.client, prefix)
}

func (s *DatabaseStore) Create(item *StoredDatabase) storage.PutOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *DatabaseStore) Update(item *StoredDatabase) storage.PutOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *DatabaseStore) Delete(item *StoredDatabase) storage.DeleteValueOp[*StoredDatabase] {
	key := s.Key(item.DatabaseID)
	return storage.NewDeleteValueOp(s.client, key, item)
}
