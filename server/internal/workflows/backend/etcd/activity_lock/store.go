package activity_lock

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstanceID string    `json:"workflow_instance_id"`
	EventID            string    `json:"event_id"`
	CreatedAt          time.Time `json:"created_at"`
	WorkerID           string    `json:"worker_id"`
	WorkerInstanceID   string    `json:"worker_instance_id"`
}

func (v *Value) CanBeReassignedTo(workerID, workerInstanceID string) bool {
	// This lock can be reassigned if it belongs to an old instance of the given
	// worker.
	return v.WorkerID == workerID && v.WorkerInstanceID != workerInstanceID
}

type Store struct {
	client *clientv3.Client
	root   string
}

func NewStore(client *clientv3.Client, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) Key(instanceID, eventID string) string {
	return storage.Key("/", s.root, "workflows", "activity_locks", instanceID, eventID)
}

func (s *Store) ExistsByKey(instanceID, eventID string) storage.ExistsOp {
	key := s.Key(instanceID, eventID)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(instanceID, eventID string) storage.GetOp[*Value] {
	key := s.Key(instanceID, eventID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.EventID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.EventID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID, eventID string) storage.DeleteOp {
	key := s.Key(instanceID, eventID)
	return storage.NewDeleteKeyOp(s.client, key)
}
