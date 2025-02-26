package certificates

import "github.com/pgEdge/control-plane/server/internal/storage"

type Store struct {
	client    storage.EtcdClient
	CA        *CAStore
	Principal *PrincipalStore
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client:    client,
		CA:        NewCAStore(client, root),
		Principal: NewPrincipalStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
