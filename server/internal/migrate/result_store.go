package migrate

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/version"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// StoredResult tracks the outcome of a specific migration.
type StoredResult struct {
	storage.StoredValue
	Identifier       string        `json:"identifier"`
	Successful       bool          `json:"successful"`
	StartedAt        time.Time     `json:"started_at"`
	CompletedAt      time.Time     `json:"completed_at"`
	RunByHostID      string        `json:"run_by_host_id"`
	RunByVersionInfo *version.Info `json:"run_by_version_info"`
	Error            string        `json:"error,omitempty"`
}

type ResultStore struct {
	client *clientv3.Client
	root   string
}

func NewResultStore(client *clientv3.Client, root string) *ResultStore {
	return &ResultStore{
		client: client,
		root:   root,
	}
}

func (s *ResultStore) Prefix() string {
	return storage.Prefix(s.root, "migrations", "results")
}

func (s *ResultStore) Key(identifier string) string {
	return storage.Key(s.Prefix(), identifier)
}

func (s *ResultStore) Get(identifier string) storage.GetOp[*StoredResult] {
	return storage.NewGetOp[*StoredResult](s.client, s.Key(identifier))
}

func (s *ResultStore) Put(item *StoredResult) storage.PutOp[*StoredResult] {
	return storage.NewPutOp(s.client, s.Key(item.Identifier), item)
}
