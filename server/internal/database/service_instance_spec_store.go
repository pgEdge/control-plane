package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredServiceInstanceSpec struct {
	storage.StoredValue
	Spec *ServiceInstanceSpec `json:"spec"`
}

type ServiceInstanceSpecStore struct {
	client *clientv3.Client
	root   string
}

func NewServiceInstanceSpecStore(client *clientv3.Client, root string) *ServiceInstanceSpecStore {
	return &ServiceInstanceSpecStore{
		client: client,
		root:   root,
	}
}

func (s *ServiceInstanceSpecStore) Prefix() string {
	return storage.Prefix("/", s.root, "service_instance_specs")
}

func (s *ServiceInstanceSpecStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *ServiceInstanceSpecStore) Key(databaseID, serviceInstanceID string) string {
	return storage.Key(s.DatabasePrefix(databaseID), serviceInstanceID)
}

func (s *ServiceInstanceSpecStore) GetByKey(databaseID, serviceInstanceID string) storage.GetOp[*StoredServiceInstanceSpec] {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewGetOp[*StoredServiceInstanceSpec](s.client, key)
}

func (s *ServiceInstanceSpecStore) GetByDatabaseID(databaseID string) storage.GetMultipleOp[*StoredServiceInstanceSpec] {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewGetPrefixOp[*StoredServiceInstanceSpec](s.client, prefix)
}

func (s *ServiceInstanceSpecStore) Update(item *StoredServiceInstanceSpec) storage.PutOp[*StoredServiceInstanceSpec] {
	key := s.Key(item.Spec.DatabaseID, item.Spec.ServiceInstanceID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *ServiceInstanceSpecStore) DeleteByKey(databaseID, serviceInstanceID string) storage.DeleteOp {
	key := s.Key(databaseID, serviceInstanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *ServiceInstanceSpecStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
