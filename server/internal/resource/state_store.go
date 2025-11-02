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

type StateStore struct {
	client *clientv3.Client
	root   string
}

func NewStateStore(client *clientv3.Client, root string) *StateStore {
	return &StateStore{
		client: client,
		root:   root,
	}
}

func (s *StateStore) Prefix() string {
	return storage.Prefix("/", s.root, "resource_state")
}

func (s *StateStore) Key(databaseID string) string {
	return storage.Key(s.Prefix(), databaseID)
}

func (s *StateStore) ExistsByKey(databaseID string) storage.ExistsOp {
	key := s.Key(databaseID)
	return storage.NewExistsOp(s.client, key)
}

func (s *StateStore) GetByKey(databaseID string) storage.GetOp[*StoredState] {
	key := s.Key(databaseID)
	return storage.NewGetOp[*StoredState](s.client, key)
}

func (s *StateStore) Put(item *StoredState) storage.PutOp[*StoredState] {
	key := s.Key(item.DatabaseID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *StateStore) DeleteByKey(databaseID string) storage.DeleteOp {
	key := s.Key(databaseID)
	return storage.NewDeleteKeyOp(s.client, key)
}
