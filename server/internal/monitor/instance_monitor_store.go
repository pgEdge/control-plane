package monitor

import (
	"path"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstanceMonitor struct {
	storage.StoredValue
	HostID           string `json:"host_id"`
	DatabaseID       string `json:"database_id"`
	InstanceID       string `json:"instance_id"`
	DatabaseName     string `json:"database_name"`
	InstanceHostname string `json:"instance_hostname"`
}

type InstanceMonitorStore struct {
	client *clientv3.Client
	root   string
}

func NewInstanceMonitorStore(client *clientv3.Client, root string) *InstanceMonitorStore {
	return &InstanceMonitorStore{
		client: client,
		root:   root,
	}
}

func (s *InstanceMonitorStore) Prefix() string {
	return path.Join("/", s.root, "instance_monitors")
}

func (s *InstanceMonitorStore) HostPrefix(hostID string) string {
	return path.Join(s.Prefix(), hostID)
}

func (s *InstanceMonitorStore) Key(hostID, instanceID string) string {
	return path.Join(s.HostPrefix(hostID), instanceID)
}

func (s *InstanceMonitorStore) GetAllByHostID(hostID string) storage.GetMultipleOp[*StoredInstanceMonitor] {
	prefix := s.HostPrefix(hostID)
	return storage.NewGetPrefixOp[*StoredInstanceMonitor](s.client, prefix)
}

func (s *InstanceMonitorStore) GetByKey(hostID, instanceID string) storage.GetOp[*StoredInstanceMonitor] {
	key := s.Key(hostID, instanceID)
	return storage.NewGetOp[*StoredInstanceMonitor](s.client, key)
}

func (s *InstanceMonitorStore) DeleteByHostID(hostID string) storage.DeleteOp {
	prefix := s.HostPrefix(hostID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *InstanceMonitorStore) DeleteByKey(hostID, instanceID string) storage.DeleteOp {
	key := s.Key(hostID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *InstanceMonitorStore) Put(item *StoredInstanceMonitor) storage.PutOp[*StoredInstanceMonitor] {
	key := s.Key(item.HostID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}
