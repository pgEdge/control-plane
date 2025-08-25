package resource

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type StoredState struct {
	storage.StoredValue
	DatabaseID string `json:"database_id"`
	State      *State `json:"state"`
}

type Store struct {
	client *clientv3.Client
	root   string
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) Prefix() string {
	return storage.Prefix("/", s.root, "resource_state")
}

func (s *Store) Key(databaseID string) string {
	return storage.Key(s.Prefix(), databaseID)
}

func (s *Store) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(databaseID string) storage.GetOp[*StoredState] {
	key := s.Key(databaseID)
	return storage.NewGetOp[*StoredState](s.client, key)
}

func (s *Store) Put(item *StoredState) storage.PutOp[*StoredState] {
	key := s.Key(item.DatabaseID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *Store) DeleteByKey(databaseID string) storage.DeleteOp {
	key := s.Key(databaseID)
	return storage.NewDeleteKeyOp(s.client, key)
}
