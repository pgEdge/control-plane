package host

import (
	"path"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredHostStatus struct {
	storage.StoredValue
	HostID     string                            `json:"host_id"`
	UpdatedAt  time.Time                         `json:"updated_at"`
	State      HostState                         `json:"state"`
	Components map[string]common.ComponentStatus `json:"components"`
}

type HostStatusStore struct {
	client *clientv3.Client
	root   string
}

func NewHostStatusStore(client *clientv3.Client, root string) *HostStatusStore {
	return &HostStatusStore{
		client: client,
		root:   root,
	}
}

func (s *HostStatusStore) Prefix() string {
	return path.Join("/", s.root, "host_statuses")
}

func (s *HostStatusStore) Key(hostID string) string {
	return path.Join(s.Prefix(), hostID)
}

func (s *HostStatusStore) GetByKey(hostID string) storage.GetOp[*StoredHostStatus] {
	key := s.Key(hostID)
	return storage.NewGetOp[*StoredHostStatus](s.client, key)
}

func (s *HostStatusStore) GetByKeys(hostIDs ...string) storage.GetMultipleOp[*StoredHostStatus] {
	keys := make([]string, len(hostIDs))
	for idx, hostID := range hostIDs {
		keys[idx] = s.Key(hostID)
	}
	return storage.NewGetMultipleOp[*StoredHostStatus](s.client, keys)
}

func (s *HostStatusStore) GetAll() storage.GetMultipleOp[*StoredHostStatus] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredHostStatus](s.client, prefix)
}

func (s *HostStatusStore) Create(item *StoredHostStatus) storage.PutOp[*StoredHostStatus] {
	key := s.Key(item.HostID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *HostStatusStore) Put(item *StoredHostStatus) storage.PutOp[*StoredHostStatus] {
	key := s.Key(item.HostID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *HostStatusStore) DeleteByKey(hostID string) storage.DeleteOp {
	key := s.Key(hostID)
	return storage.NewDeleteKeyOp(s.client, key)
}
