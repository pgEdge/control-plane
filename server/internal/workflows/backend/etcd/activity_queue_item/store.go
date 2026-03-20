package activity_queue_item

import (
	"context"
	"time"

	"github.com/cschleiden/go-workflows/backend/history"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstanceID  string         `json:"workflow_instance_id"`
	WorkflowExecutionID string         `json:"workflow_execution_id"`
	Queue               string         `json:"queue"`
	Event               *history.Event `json:"event"`
	LastLocked          *time.Time     `json:"last_locked"`
}

func (v *Value) UpdateLastLocked() {
	now := time.Now()
	v.LastLocked = &now
}

// Store is a storage implementation for activity queue items. StartCache must
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
	return storage.Prefix("/", s.root, "workflows", "activity_queue_items")
}

func (s *Store) QueuePrefix(queue string) string {
	return storage.Prefix(s.AllQueuesPrefix(), queue)
}

func (s *Store) Key(queue, instanceID, eventID string) string {
	return storage.Key(s.QueuePrefix(queue), instanceID, eventID)
}

func (s *Store) StartCache(ctx context.Context) error {
	if s.cache != nil {
		return nil
	}
	s.cache = storage.NewCache(s.client, s.AllQueuesPrefix(), func(item *Value) string {
		return s.Key(item.Queue, item.WorkflowInstanceID, item.Event.ID)
	})
	return s.cache.Start(ctx)
}

func (s *Store) StopCache() {
	if s.cache != nil {
		s.cache.Stop()
	}
}

func (s *Store) PropagateErrors(ctx context.Context, ch chan error) {
	s.cache.PropagateErrors(ctx, ch)
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	return s.cache.GetPrefix(s.AllQueuesPrefix())
}

func (s *Store) GetByKey(queue, instanceID, eventID string) storage.GetOp[*Value] {
	key := s.Key(queue, instanceID, eventID)
	return s.cache.Get(key)
}

func (s *Store) GetByQueue(queue string) storage.GetMultipleOp[*Value] {
	prefix := s.QueuePrefix(queue)
	return s.cache.GetPrefix(prefix)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	return s.cache.Create(item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	return s.cache.Update(item)
}

func (s *Store) DeleteByKey(queue, instanceID, eventID string) storage.DeleteOp {
	key := s.Key(queue, instanceID, eventID)
	return s.cache.DeleteByKey(key)
}

func (s *Store) DeleteItem(item *Value) storage.DeleteValueOp[*Value] {
	return s.cache.DeleteValue(item)
}
