package workflow_queue_item

import (
	"path"
	"time"

	"github.com/cschleiden/go-workflows/backend/metadata"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	version          int64                      `json:"-"`
	WorkflowInstance *workflow.Instance         `json:"workflow_instance"`
	State            core.WorkflowInstanceState `json:"state"`
	CreatedAt        time.Time                  `json:"created_at"`
	Queue            core.Queue                 `json:"queue"`
	Metadata         *metadata.WorkflowMetadata `json:"metadata"`
	LastLocked       *time.Time                 `json:"last_locked"`
}

func (v *Value) Version() int64 {
	return v.version
}

func (v *Value) SetVersion(version int64) {
	v.version = version
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
	return path.Join("/", s.root, "workflows", "workflow_queue_items")
}

func (s *Store) QueuePrefix(queue string) string {
	return path.Join(s.AllQueuesPrefix(), queue)
}

func (s *Store) InstanceIDPrefix(queue, instanceID string) string {
	return path.Join(s.QueuePrefix(queue), instanceID)
}

func (s *Store) Key(queue, instanceID, executionID string) string {
	return path.Join(s.InstanceIDPrefix(queue, instanceID), executionID)
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	return storage.NewGetPrefixOp[*Value](s.client, s.AllQueuesPrefix())
}

func (s *Store) GetByKey(queue, instanceID, executionID string) storage.GetOp[*Value] {
	key := s.Key(queue, instanceID, executionID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) GetByInstanceID(queue, instanceID string) storage.GetMultipleOp[*Value] {
	prefix := s.InstanceIDPrefix(queue, instanceID)
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) GetByQueue(queue string) storage.GetMultipleOp[*Value] {
	prefix := s.QueuePrefix(queue)
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(string(item.Queue), item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Put(item *Value) storage.PutOp[*Value] {
	key := s.Key(string(item.Queue), item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(string(item.Queue), item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(queue, instanceID, executionID string) storage.DeleteOp {
	key := s.Key(queue, instanceID, executionID)
	return storage.NewDeleteKeyOp(s.client, key)
}
