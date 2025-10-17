package cluster

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredCluster struct {
	storage.StoredValue
	ID string `json:"id"`
}

type ClusterStore struct {
	client *clientv3.Client
	root   string
}

func NewClusterStore(client *clientv3.Client, root string) *ClusterStore {
	return &ClusterStore{
		client: client,
		root:   root,
	}
}

func (s *ClusterStore) Key() string {
	return storage.Key("/", s.root, "cluster")
}

func (s *ClusterStore) Get() storage.GetOp[*StoredCluster] {
	key := s.Key()
	return storage.NewGetOp[*StoredCluster](s.client, key)
}

func (s *ClusterStore) Create(item *StoredCluster) storage.PutOp[*StoredCluster] {
	key := s.Key()
	return storage.NewCreateOp(s.client, key, item)
}
