package host

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Store struct {
	client     storage.EtcdClient
	Host       *HostStore
	HostStatus *HostStatusStore
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client:     client,
		Host:       NewHostStore(client, root),
		HostStatus: NewHostStatusStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
