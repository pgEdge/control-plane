package host

import (
	"path"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredCohort struct {
	Type             CohortType `json:"type"`
	CohortID         string     `json:"cohort_id"`
	MemberID         string     `json:"member_id"`
	ControlAvailable bool       `json:"control_available"`
}

type StoredHost struct {
	storage.StoredValue
	ID                      uuid.UUID           `json:"id"`
	Orchestrator            config.Orchestrator `json:"type"`
	Cohort                  *StoredCohort       `json:"stored_cohort,omitempty"`
	Hostname                string              `json:"hostname"`
	IPv4Address             string              `json:"ipv4_address"`
	CPUs                    int                 `json:"cpus"`
	MemBytes                uint64              `json:"mem_bytes"`
	DefaultPgEdgeVersion    *PgEdgeVersion      `json:"default_version"`
	SupportedPgEdgeVersions []*PgEdgeVersion    `json:"supported_versions"`
}

type HostStore struct {
	client storage.EtcdClient
	root   string
}

func NewHostStore(client storage.EtcdClient, root string) *HostStore {
	return &HostStore{
		client: client,
		root:   root,
	}
}

func (s *HostStore) Prefix() string {
	return path.Join("/", s.root, "hosts")
}

func (s *HostStore) Key(hostID uuid.UUID) string {
	return path.Join(s.Prefix(), hostID.String())
}

func (s *HostStore) GetByKey(hostID uuid.UUID) storage.GetOp[*StoredHost] {
	key := s.Key(hostID)
	return storage.NewGetOp[*StoredHost](s.client, key)
}

func (s *HostStore) GetByKeys(hostIDs ...uuid.UUID) storage.GetMultipleOp[*StoredHost] {
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

func (s *HostStore) DeleteByKey(hostID uuid.UUID) storage.DeleteOp {
	key := s.Key(hostID)
	return storage.NewDeleteKeyOp(s.client, key)
}
