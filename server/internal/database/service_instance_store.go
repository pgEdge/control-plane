package database

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredServiceInstance struct {
	storage.StoredValue
	ServiceInstanceID string               `json:"service_instance_id"`
	ServiceID         string               `json:"service_id"`
	DatabaseID        string               `json:"database_id"`
	HostID            string               `json:"host_id"`
	State             ServiceInstanceState `json:"state"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	Error             string               `json:"error,omitempty"`
}

type ServiceInstanceUpdateOptions struct {
	ServiceInstanceID string               `json:"service_instance_id"`
	ServiceID         string               `json:"service_id"`
	DatabaseID        string               `json:"database_id"`
	HostID            string               `json:"host_id"`
	State             ServiceInstanceState `json:"state"`
	Error             string               `json:"error,omitempty"`
}

func NewStoredServiceInstance(opts *ServiceInstanceUpdateOptions) *StoredServiceInstance {
	now := time.Now()
	return &StoredServiceInstance{
		ServiceInstanceID: opts.ServiceInstanceID,
		ServiceID:         opts.ServiceID,
		DatabaseID:        opts.DatabaseID,
		HostID:            opts.HostID,
		State:             opts.State,
		CreatedAt:         now,
		UpdatedAt:         now,
		Error:             opts.Error,
	}
}

func (s *StoredServiceInstance) Update(opts *ServiceInstanceUpdateOptions) {
	s.State = opts.State
	s.Error = opts.Error
	s.UpdatedAt = time.Now()
}

type ServiceInstanceStore struct {
	client *clientv3.Client
	root   string
}

func NewServiceInstanceStore(client *clientv3.Client, root string) *ServiceInstanceStore {
	return &ServiceInstanceStore{
		client: client,
		root:   root,
	}
}

func (s *ServiceInstanceStore) Prefix() string {
	return storage.Prefix("/", s.root, "service_instances")
}

func (s *ServiceInstanceStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *ServiceInstanceStore) Key(databaseID, serviceInstanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), serviceInstanceID)
}

func (s *ServiceInstanceStore) GetByKey(databaseID, serviceInstanceID string) storage.GetOp[*StoredServiceInstance] {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewGetOp[*StoredServiceInstance](s.client, key)
}

func (s *ServiceInstanceStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredServiceInstance] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredServiceInstance](s.client, prefix)
}

func (s *ServiceInstanceStore) GetAll() storage.GetMultipleOp[*StoredServiceInstance] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredServiceInstance](s.client, prefix)
}

func (s *ServiceInstanceStore) Put(item *StoredServiceInstance) storage.PutOp[*StoredServiceInstance] {
	key := s.Key(item.DatabaseID, item.ServiceInstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *ServiceInstanceStore) DeleteByKey(databaseID, serviceInstanceID string) storage.DeleteOp {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
