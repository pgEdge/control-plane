package host

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredCohort struct {
	Type             CohortType `json:"type"`
	MemberID         string     `json:"member_id"`
	ControlAvailable bool       `json:"control_available"`
}

type StoredHost struct {
	storage.StoredValue
	ID                      string              `json:"id"`
	Orchestrator            config.Orchestrator `json:"type"`
	Cohort                  *StoredCohort       `json:"stored_cohort,omitempty"`
	DataDir                 string              `json:"data_dir"`
	Hostname                string              `json:"hostname"`
	IPv4Address             string              `json:"ipv4_address"`
	HTTPPort                int                 `json:"http_port"`
	CPUs                    int                 `json:"cpus"`
	MemBytes                uint64              `json:"mem_bytes"`
	DefaultPgEdgeVersion    *PgEdgeVersion      `json:"default_version"`
	SupportedPgEdgeVersions []*PgEdgeVersion    `json:"supported_versions"`
}

type HostStore struct {
	client *clientv3.Client
	root   string
}

func NewHostStore(client *clientv3.Client, root string) *HostStore {
	return &HostStore{
		client: client,
		root:   root,
	}
}

func (s *HostStore) Prefix() string {
	return storage.Prefix("/", s.root, "hosts")
}

func (s *HostStore) Key(hostID string) string {
	return storage.Key(s.Prefix(), hostID)
}

func (s *HostStore) GetByKey(hostID string) storage.GetOp[*StoredHost] {
	key := s.Key(hostID)
	return storage.NewGetOp[*StoredHost](s.client, key)
}

func (s *HostStore) GetByKeys(hostIDs ...string) storage.GetMultipleOp[*StoredHost] {
	keys := make([]string, len(hostIDs))
	for idx, hostID := range hostIDs {
		keys[idx] = s.Key(hostID)
	}
	return storage.NewGetMultipleOp[*StoredHost](s.client, keys)
}

func (s *HostStore) GetAll() storage.GetMultipleOp[*StoredHost] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredHost](s.client, prefix)
}

func (s *HostStore) Create(item *StoredHost) storage.PutOp[*StoredHost] {
	key := s.Key(item.ID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *HostStore) Put(item *StoredHost) storage.PutOp[*StoredHost] {
	key := s.Key(item.ID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *HostStore) DeleteByKey(hostID string) storage.DeleteOp {
	key := s.Key(hostID)
	return storage.NewDeleteKeyOp(s.client, key)
}
