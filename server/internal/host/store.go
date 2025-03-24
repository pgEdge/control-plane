package host

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Store struct {
	client     *clientv3.Client
	Host       *HostStore
	HostStatus *HostStatusStore
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client:     client,
		Host:       NewHostStore(client, root),
		HostStatus: NewHostStatusStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
