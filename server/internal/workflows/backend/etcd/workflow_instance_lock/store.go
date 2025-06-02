package workflow_instance_lock

import (
	"path"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstanceID  string    `json:"workflow_instance_id"`
	WorkflowExecutionID string    `json:"workflow_execution_id"`
	CreatedAt           time.Time `json:"created_at"`
	WorkerID            string    `json:"worker_id"`
	WorkerInstanceID    string    `json:"worker_instance_id"`
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

func (s *Store) Key(instanceID, executionID string) string {
	return path.Join("/", s.root, "workflows", "workflow_instance_locks", instanceID, executionID)
}

func (s *Store) ExistsByKey(instanceID, executionID string) storage.ExistsOp {
	key := s.Key(instanceID, executionID)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(instanceID, executionID string) storage.GetOp[*Value] {
	key := s.Key(instanceID, executionID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.WorkflowExecutionID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.WorkflowExecutionID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID, executionID string) storage.DeleteOp {
	key := s.Key(instanceID, executionID)
	return storage.NewDeleteKeyOp(s.client, key)
}
