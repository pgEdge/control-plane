package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredServiceInstanceStatus struct {
	storage.StoredValue
	DatabaseID        string                 `json:"database_id"`
	ServiceInstanceID string                 `json:"service_instance_id"`
	Status            *ServiceInstanceStatus `json:"status"`
}

type ServiceInstanceStatusStore struct {
	client *clientv3.Client
	root   string
}

func NewServiceInstanceStatusStore(client *clientv3.Client, root string) *ServiceInstanceStatusStore {
	return &ServiceInstanceStatusStore{
		client: client,
		root:   root,
	}
}

func (s *ServiceInstanceStatusStore) Prefix() string {
	return storage.Prefix("/", s.root, "service_instance_statuses")
}

func (s *ServiceInstanceStatusStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *ServiceInstanceStatusStore) Key(databaseID, serviceInstanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), serviceInstanceID)
}

func (s *ServiceInstanceStatusStore) GetByKey(databaseID, serviceInstanceID string) storage.GetOp[*StoredServiceInstanceStatus] {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewGetOp[*StoredServiceInstanceStatus](s.client, key)
}

func (s *ServiceInstanceStatusStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredServiceInstanceStatus] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredServiceInstanceStatus](s.client, prefix)
}

func (s *ServiceInstanceStatusStore) GetAll() storage.GetMultipleOp[*StoredServiceInstanceStatus] {
	prefix := s.Prefix()
	return storage.NewGetPrefixOp[*StoredServiceInstanceStatus](s.client, prefix)
}

func (s *ServiceInstanceStatusStore) Put(item *StoredServiceInstanceStatus) storage.PutOp[*StoredServiceInstanceStatus] {
	key := s.Key(item.DatabaseID, item.ServiceInstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *ServiceInstanceStatusStore) DeleteByKey(databaseID, serviceInstanceID string) storage.DeleteOp {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
