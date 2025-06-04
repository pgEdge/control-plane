package monitor

import (
	"path"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredInstanceMonitor struct {
	storage.StoredValue
	HostID           uuid.UUID `json:"host_id"`
	DatabaseID       uuid.UUID `json:"database_id"`
	InstanceID       uuid.UUID `json:"instance_id"`
	DatabaseName     string    `json:"database_name"`
	InstanceHostname string    `json:"instance_hostname"`
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

func (s *InstanceMonitorStore) HostPrefix(hostID uuid.UUID) string {
	return path.Join(s.Prefix(), hostID.String())
}

func (s *InstanceMonitorStore) Key(hostID, instanceID uuid.UUID) string {
	return path.Join(s.HostPrefix(hostID), instanceID.String())
}

func (s *InstanceMonitorStore) GetAllByHostID(hostID uuid.UUID) storage.GetMultipleOp[*StoredInstanceMonitor] {
	prefix := s.HostPrefix(hostID)
	return storage.NewGetPrefixOp[*StoredInstanceMonitor](s.client, prefix)
}

func (s *InstanceMonitorStore) GetByKey(hostID, instanceID uuid.UUID) storage.GetOp[*StoredInstanceMonitor] {
	key := s.Key(hostID, instanceID)
	return storage.NewGetOp[*StoredInstanceMonitor](s.client, key)
}

func (s *InstanceMonitorStore) DeleteByHostID(hostID uuid.UUID) storage.DeleteOp {
	prefix := s.HostPrefix(hostID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *InstanceMonitorStore) DeleteByKey(hostID, instanceID uuid.UUID) storage.DeleteOp {
	key := s.Key(hostID, instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *InstanceMonitorStore) Put(item *StoredInstanceMonitor) storage.PutOp[*StoredInstanceMonitor] {
	key := s.Key(item.HostID, item.InstanceID)
	return storage.NewPutOp(s.client, key, item)
}
