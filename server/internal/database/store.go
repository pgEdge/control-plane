package database

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/encryption"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Store struct {
	client         *clientv3.Client
	encryptor      encryption.Encryptor
	Spec           *SpecStore
	Database       *DatabaseStore
	Instance       *InstanceStore
	InstanceStatus *InstanceStatusStore
}

func NewStore(client *clientv3.Client, encryptor encryption.Encryptor, root string) *Store {
	return &Store{
		client:         client,
		encryptor:      encryptor,
		Spec:           NewSpecStore(client, encryptor, root),
		Database:       NewDatabaseStore(client, encryptor, root),
		Instance:       NewInstanceStore(client, encryptor, root),
		InstanceStatus: NewInstanceStatusStore(client, encryptor, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
