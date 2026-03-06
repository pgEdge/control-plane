package ports

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredPortRange struct {
	storage.StoredValue
	Name     string `json:"name"`
	Snapshot []byte `json:"snapshot"`
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
	return storage.Prefix("/", s.root, "ports")
}

func (s *Store) Key(allocatorName string) string {
	return storage.Key(s.Prefix(), allocatorName)
}

func (s *Store) ExistsByKey(allocatorName string) storage.ExistsOp {
	key := s.Key(allocatorName)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(allocatorName string) storage.GetOp[*StoredPortRange] {
	key := s.Key(allocatorName)
	return storage.NewGetOp[*StoredPortRange](s.client, key)
}

func (s *Store) Update(item *StoredPortRange) storage.PutOp[*StoredPortRange] {
	key := s.Key(item.Name)
	return storage.NewUpdateOp(s.client, key, item)
}
