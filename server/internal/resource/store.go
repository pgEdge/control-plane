package resource

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Store struct {
	client        *clientv3.Client
	State         *StateStore
	PlanSummaries *PlanSummaryStore
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client:        client,
		State:         NewStateStore(client, root),
		PlanSummaries: NewPlanSummaryStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
