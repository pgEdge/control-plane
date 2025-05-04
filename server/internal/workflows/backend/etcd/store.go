package etcd

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/activity_lock"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/activity_queue_item"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/history_event"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/pending_event"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance_lock"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance_sticky"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_queue_item"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Store struct {
	client                 *clientv3.Client
	ActivityLock           *activity_lock.Store
	ActivityQueueItem      *activity_queue_item.Store
	HistoryEvent           *history_event.Store
	PendingEvent           *pending_event.Store
	WorkflowInstance       *workflow_instance.Store
	WorkflowInstanceLock   *workflow_instance_lock.Store
	WorkflowInstanceSticky *workflow_instance_sticky.Store
	WorkflowQueueItem      *workflow_queue_item.Store
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client:                 client,
		ActivityLock:           activity_lock.NewStore(client, root),
		ActivityQueueItem:      activity_queue_item.NewStore(client, root),
		HistoryEvent:           history_event.NewStore(client, root),
		PendingEvent:           pending_event.NewStore(client, root),
		WorkflowInstance:       workflow_instance.NewStore(client, root),
		WorkflowInstanceLock:   workflow_instance_lock.NewStore(client, root),
		WorkflowInstanceSticky: workflow_instance_sticky.NewStore(client, root),
		WorkflowQueueItem:      workflow_queue_item.NewStore(client, root),
	}
}

func (s *Store) Txn(ops ...storage.TxnOperation) storage.Txn {
	return storage.NewTxn(s.client, ops...)
}
