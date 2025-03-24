package database

import (
	"path"
	"time"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstance struct {
	storage.StoredValue
	DatabaseID      uuid.UUID            `json:"database_id"`
	InstanceID      uuid.UUID            `json:"instance_id"`
	HostID          uuid.UUID            `json:"host_id"`
	TenantID        *uuid.UUID           `json:"tenant_id,omitempty"`
	ReplicaOfID     uuid.UUID            `json:"replica_of_id,omitempty"`
	DatabaseName    string               `json:"database_name"`
	NodeName        string               `json:"node_name"`
	ReplicaName     string               `json:"replica_name,omitempty"`
	PostgresVersion string               `json:"postgres_version"`
	SpockVersion    string               `json:"spock_version"`
	Port            int                  `json:"port"`
	State           InstanceState        `json:"state"`
	PatroniState    patroni.State        `json:"patroni_state"`
	Role            patroni.InstanceRole `json:"role"`
	ReadOnly        bool                 `json:"read_only"`
	PendingRestart  bool                 `json:"pending_restart"`
	PatroniPaused   bool                 `json:"patroni_paused"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Interfaces      []*InstanceInterface `json:"interfaces"`
}

type InstanceStore struct {
	client *clientv3.Client
	root   string
}

func NewInstanceStore(client *clientv3.Client, root string) *InstanceStore {
	return &InstanceStore{
		client: client,
		root:   root,
	}
}

func (s *InstanceStore) Prefix() string {
	return path.Join("/", s.root, "instances")
}

func (s *InstanceStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *InstanceStore) Key(databaseID, instanceID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), instanceID.String())
}

func (s *InstanceStore) ExistsByKey(databaseID, instanceID uuid.UUID) storage.ExistsOp {
	key := s.Key(databaseID, instanceID)
	return storage.NewExistsOp(s.client, key)
}

func (s *InstanceStore) GetByKey(databaseID, instanceID uuid.UUID) storage.GetOp[*StoredInstance] {
	key := s.Key(databaseID, instanceID)
	return storage.NewGetOp[*StoredInstance](s.client, key)
}

func (s *InstanceStore) GetByKeys(databaseID uuid.UUID, instanceIDs ...uuid.UUID) storage.GetMultipleOp[*StoredInstance] {
	keys := make([]string, len(instanceIDs))
	for idx, instanceID := range instanceIDs {
		keys[idx] = s.Key(databaseID, instanceID)
	}
	return storage.NewGetMultipleOp[*StoredInstance](s.client, keys)
}

func (s *InstanceStore) GetAll() storage.GetMultipleOp[*StoredInstance] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredInstance](s.client, prefix)
}

func (s *InstanceStore) Create(item *StoredInstance) storage.PutOp[*StoredInstance] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *InstanceStore) Update(item *StoredInstance) storage.PutOp[*StoredInstance] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *InstanceStore) Delete(item *StoredInstance) storage.PutOp[*StoredInstance] {
	key := s.Key(item.DatabaseID, item.InstanceID)
	return storage.NewCreateOp(s.client, key, item)
}
