package etcd

import (
	"context"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/activity_lock"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/activity_queue_item"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/history_event"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/pending_event"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance_lock"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_instance_sticky"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd/workflow_queue_item"
)

type Store struct {
	client                 *clientv3.Client
	errCh                  chan error
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
		errCh:                  make(chan error, 1),
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

func (s *Store) StartCaches(ctx context.Context) error {
	if err := s.WorkflowQueueItem.StartCache(ctx); err != nil {
		return fmt.Errorf("failed to start workflow queue item cache: %w", err)
	}
	if err := s.ActivityQueueItem.StartCache(ctx); err != nil {
		return fmt.Errorf("failed to start activity queue item cache: %w", err)
	}
	s.WorkflowQueueItem.PropagateErrors(ctx, s.errCh)
	s.ActivityQueueItem.PropagateErrors(ctx, s.errCh)

	return nil
}

func (s *Store) StopCaches() {
	s.WorkflowQueueItem.StopCache()
	s.ActivityQueueItem.StopCache()
}

func (s *Store) Error() <-chan error {
	return s.errCh
}
