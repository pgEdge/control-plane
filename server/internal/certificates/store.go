package certificates

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Store struct {
	client    *clientv3.Client
	CA        *CAStore
	Principal *PrincipalStore
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client:    client,
		CA:        NewCAStore(client, root),
		Principal: NewPrincipalStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
