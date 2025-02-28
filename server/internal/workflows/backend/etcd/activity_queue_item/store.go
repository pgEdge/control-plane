package activity_queue_item

import (
	"path"
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"

	"github.com/cschleiden/go-workflows/backend/history"
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

type Store struct {
	client storage.EtcdClient
	root   string
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) AllQueuesPrefix() string {
	return path.Join("/", s.root, "workflows", "activity_queue_items")
}

func (s *Store) QueuePrefix(queue string) string {
	return path.Join(s.AllQueuesPrefix(), queue)
}

func (s *Store) Key(queue, instanceID, eventID string) string {
	return path.Join(s.QueuePrefix(queue), instanceID, eventID)
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	return storage.NewGetPrefixOp[*Value](s.client, s.AllQueuesPrefix())
}

func (s *Store) GetByKey(queue, instanceID, eventID string) storage.GetOp[*Value] {
	key := s.Key(queue, instanceID, eventID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) GetByQueue(queue string) storage.GetMultipleOp[*Value] {
	prefix := s.QueuePrefix(queue)
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.Queue, item.WorkflowInstanceID, item.Event.ID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.Queue, item.WorkflowInstanceID, item.Event.ID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(queue, instanceID, eventID string) storage.DeleteOp {
	key := s.Key(queue, instanceID, eventID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *Store) DeleteItem(item *Value) storage.DeleteValueOp[*Value] {
	key := s.Key(item.Queue, item.WorkflowInstanceID, item.Event.ID)
	return storage.NewDeleteValueOp(s.client, key, item)
}
