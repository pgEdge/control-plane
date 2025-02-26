package cluster

import (
	"path"
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredCluster struct {
	storage.StoredValue
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	client storage.EtcdClient
	root   string
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) Key() string {
	return path.Join("/", s.root, "cluster")
}

func (s *Store) Exists() storage.ExistsOp {
	key := s.Key()
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) Get() storage.GetOp[*StoredCluster] {
	key := s.Key()
	return storage.NewGetOp[*StoredCluster](s.client, key)
}

func (s *Store) Create(item *StoredCluster) storage.PutOp[*StoredCluster] {
	key := s.Key()
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Delete() storage.DeleteOp {
	key := s.Key()
	return storage.NewDeleteKeyOp(s.client, key)
}
