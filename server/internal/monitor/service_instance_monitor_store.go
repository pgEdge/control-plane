package monitor

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredServiceInstanceMonitor struct {
	storage.StoredValue
	HostID            string `json:"host_id"`
	DatabaseID        string `json:"database_id"`
	ServiceInstanceID string `json:"service_instance_id"`
}

type ServiceInstanceMonitorStore struct {
	client *clientv3.Client
	root   string
}

func NewServiceInstanceMonitorStore(client *clientv3.Client, root string) *ServiceInstanceMonitorStore {
	return &ServiceInstanceMonitorStore{
		client: client,
		root:   root,
	}
}

func (s *ServiceInstanceMonitorStore) Prefix() string {
	return storage.Prefix("/", s.root, "service_instance_monitors")
}

func (s *ServiceInstanceMonitorStore) HostPrefix(hostID string) string {
	return storage.Prefix(s.Prefix(), hostID)
}

func (s *ServiceInstanceMonitorStore) Key(hostID, serviceInstanceID string) string {
	return storage.Key(s.HostPrefix(hostID), serviceInstanceID)
}

func (s *ServiceInstanceMonitorStore) GetAllByHostID(hostID string) storage.GetMultipleOp[*StoredServiceInstanceMonitor] {
	prefix := s.HostPrefix(hostID)
	return storage.NewGetPrefixOp[*StoredServiceInstanceMonitor](s.client, prefix)
}

func (s *ServiceInstanceMonitorStore) GetByKey(hostID, serviceInstanceID string) storage.GetOp[*StoredServiceInstanceMonitor] {
	key := s.Key(hostID, serviceInstanceID)
	return storage.NewGetOp[*StoredServiceInstanceMonitor](s.client, key)
}

func (s *ServiceInstanceMonitorStore) DeleteByHostID(hostID string) storage.DeleteOp {
	prefix := s.HostPrefix(hostID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *ServiceInstanceMonitorStore) DeleteByKey(hostID, serviceInstanceID string) storage.DeleteOp {
	key := s.Key(hostID, serviceInstanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *ServiceInstanceMonitorStore) Put(item *StoredServiceInstanceMonitor) storage.PutOp[*StoredServiceInstanceMonitor] {
	key := s.Key(item.HostID, item.ServiceInstanceID)
	return storage.NewPutOp(s.client, key, item)
}
