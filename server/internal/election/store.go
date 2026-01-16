package election

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Name string

func (n Name) String() string {
	return string(n)
}

type StoredElection struct {
	storage.StoredValue
	Name      Name      `json:"name"`
	LeaderID  string    `json:"leader_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ElectionStore struct {
	client *clientv3.Client
	root   string
}

func NewElectionStore(client *clientv3.Client, root string) *ElectionStore {
	return &ElectionStore{
		client: client,
		root:   root,
	}
}

func (s *ElectionStore) Key(name Name) string {
	return storage.Key("/", s.root, "elections", name.String())
}

func (s *ElectionStore) GetByKey(name Name) storage.GetOp[*StoredElection] {
	key := s.Key(name)
	return storage.NewGetOp[*StoredElection](s.client, key)
}

func (s *ElectionStore) Create(item *StoredElection) storage.PutOp[*StoredElection] {
	key := s.Key(item.Name)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *ElectionStore) Update(item *StoredElection) storage.PutOp[*StoredElection] {
	key := s.Key(item.Name)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *ElectionStore) Delete(item *StoredElection) storage.DeleteValueOp[*StoredElection] {
	key := s.Key(item.Name)
	return storage.NewDeleteValueOp(s.client, key, item)
}

func (s *ElectionStore) Watch(name Name) storage.WatchOp[*StoredElection] {
	key := s.Key(name)
	return storage.NewWatchOp[*StoredElection](s.client, key)
}
