package workflow_queue_item

import (
	"context"
	"time"

	"github.com/cschleiden/go-workflows/backend/metadata"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstance *workflow.Instance         `json:"workflow_instance"`
	State            core.WorkflowInstanceState `json:"state"`
	CreatedAt        time.Time                  `json:"created_at"`
	Queue            core.Queue                 `json:"queue"`
	Metadata         *metadata.WorkflowMetadata `json:"metadata"`
	LastLocked       *time.Time                 `json:"last_locked"`
}

func (v *Value) UpdateLastLocked() {
	now := time.Now()
	v.LastLocked = &now
}

// Store is a storage implementation for workflow queue items. StartCache must
// be called before the store can be used.
type Store struct {
	client *clientv3.Client
	root   string
	cache  storage.Cache[*Value]
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) AllQueuesPrefix() string {
	return storage.Prefix("/", s.root, "workflows", "workflow_queue_items")
}

func (s *Store) QueuePrefix(queue string) string {
	return storage.Prefix(s.AllQueuesPrefix(), queue)
}

func (s *Store) InstanceIDPrefix(queue, instanceID string) string {
	return storage.Prefix(s.QueuePrefix(queue), instanceID)
}

func (s *Store) Key(queue, instanceID, executionID string) string {
	return storage.Key(s.InstanceIDPrefix(queue, instanceID), executionID)
}

func (s *Store) StartCache(ctx context.Context) error {
	if s.cache != nil {
		return nil
	}
	cache := storage.NewCache(s.client, s.AllQueuesPrefix(), func(item *Value) string {
		return s.Key(
			string(item.Queue),
			item.WorkflowInstance.InstanceID,
			item.WorkflowInstance.ExecutionID,
		)
	})
	if err := cache.Start(ctx); err != nil {
		return err
	}
	s.cache = cache
	return nil
}

func (s *Store) StopCache() {
	if s.cache != nil {
		s.cache.Stop()
		s.cache = nil
	}
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	return s.cache.GetPrefix(s.AllQueuesPrefix())
}

func (s *Store) PropagateErrors(ctx context.Context, ch chan error) {
	s.cache.PropagateErrors(ctx, ch)
}

func (s *Store) GetByKey(queue, instanceID, executionID string) storage.GetOp[*Value] {
	key := s.Key(queue, instanceID, executionID)
	return s.cache.Get(key)
}

func (s *Store) GetByInstanceID(queue, instanceID string) storage.GetMultipleOp[*Value] {
	prefix := s.InstanceIDPrefix(queue, instanceID)
	return s.cache.GetPrefix(prefix)
}

func (s *Store) GetByQueue(queue string) storage.GetMultipleOp[*Value] {
	prefix := s.QueuePrefix(queue)
	return s.cache.GetPrefix(prefix)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	return s.cache.Create(item)
}

func (s *Store) Put(item *Value) storage.PutOp[*Value] {
	return s.cache.Put(item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	return s.cache.Update(item)
}

func (s *Store) DeleteByKey(queue, instanceID, executionID string) storage.DeleteOp {
	key := s.Key(queue, instanceID, executionID)
	return s.cache.DeleteByKey(key)
}
