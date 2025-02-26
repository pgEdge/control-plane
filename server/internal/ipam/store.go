package ipam

import (
	"path"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredSubnetRange struct {
	storage.StoredValue
	Name     string `json:"name"`
	Spec     string `json:"spec"`
	Snapshot []byte `json:"snapshot"`
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

func (s *Store) Prefix() string {
	return path.Join("/", s.root, "ipam")
}

func (s *Store) Key(allocatorName string) string {
	return path.Join(s.Prefix(), allocatorName)
}

func (s *Store) ExistsByKey(allocatorName string) storage.ExistsOp {
	key := s.Key(allocatorName)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(allocatorName string) storage.GetOp[*StoredSubnetRange] {
	key := s.Key(allocatorName)
	return storage.NewGetOp[*StoredSubnetRange](s.client, key)
}

func (s *Store) Put(item *StoredSubnetRange) storage.PutOp[*StoredSubnetRange] {
	key := s.Key(item.Name)
	return storage.NewPutOp(s.client, key, item)
}
