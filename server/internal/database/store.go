package database

import "github.com/pgEdge/control-plane/server/internal/storage"

type Store struct {
	client   storage.EtcdClient
	Spec     *SpecStore
	Database *DatabaseStore
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client:   client,
		Spec:     NewSpecStore(client, root),
		Database: NewDatabaseStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
