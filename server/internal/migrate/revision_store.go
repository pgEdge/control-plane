package migrate

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// StoredRevision tracks the most recently applied migration.
type StoredRevision struct {
	storage.StoredValue
	Identifier string `json:"identifier"`
}

type RevisionStore struct {
	client *clientv3.Client
	root   string
}

func NewRevisionStore(client *clientv3.Client, root string) *RevisionStore {
	return &RevisionStore{
		client: client,
		root:   root,
	}
}

func (s *RevisionStore) Key() string {
	return storage.Key(s.root, "migrations", "revision")
}

func (s *RevisionStore) Get() storage.GetOp[*StoredRevision] {
	return storage.NewGetOp[*StoredRevision](s.client, s.Key())
}

func (s *RevisionStore) Create(item *StoredRevision) storage.PutOp[*StoredRevision] {
	return storage.NewCreateOp(s.client, s.Key(), item)
}

func (s *RevisionStore) Update(item *StoredRevision) storage.PutOp[*StoredRevision] {
	return storage.NewUpdateOp(s.client, s.Key(), item)
}

func (s *RevisionStore) Watch() storage.WatchOp[*StoredRevision] {
	return storage.NewWatchOp[*StoredRevision](s.client, s.Key())
}
