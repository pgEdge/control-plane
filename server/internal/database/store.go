package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Store struct {
	client                *clientv3.Client
	Spec                  *SpecStore
	Database              *DatabaseStore
	Instance              *InstanceStore
	InstanceStatus        *InstanceStatusStore
	ServiceInstance       *ServiceInstanceStore
	ServiceInstanceStatus *ServiceInstanceStatusStore
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client:                client,
		Spec:                  NewSpecStore(client, root),
		Database:              NewDatabaseStore(client, root),
		Instance:              NewInstanceStore(client, root),
		InstanceStatus:        NewInstanceStatusStore(client, root),
		ServiceInstance:       NewServiceInstanceStore(client, root),
		ServiceInstanceStatus: NewServiceInstanceStatusStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
