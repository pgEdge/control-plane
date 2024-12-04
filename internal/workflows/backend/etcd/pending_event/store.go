package pending_event

import (
	"path"

	"github.com/cschleiden/go-workflows/backend/history"

	"github.com/pgEdge/control-plane/internal/storage"
)

type Value struct {
	version             int64          `json:"-"`
	WorkflowInstanceID  string         `json:"workflow_instance_id"`
	WorkflowExecutionID string         `json:"workflow_execution_id"`
	Event               *history.Event `json:"event"`
}

func (v *Value) Version() int64 {
	return v.version
}

func (v *Value) SetVersion(version int64) {
	v.version = version
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

func (s *Store) AllEventsPrefix() string {
	return path.Join("/", s.root, "workflows", "pending_events")
}

func (s *Store) InstanceExecutionPrefix(instanceID, executionID string) string {
	return path.Join(s.AllEventsPrefix(), instanceID, executionID)
}

func (s *Store) Key(instanceID, executionID, eventID string) string {
	return path.Join(s.InstanceExecutionPrefix(instanceID, executionID), eventID)
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	prefix := s.AllEventsPrefix()
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) GetByKey(instanceID, executionID, eventID string) storage.GetOp[*Value] {
	key := s.Key(instanceID, executionID, eventID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) GetByInstanceExecution(instanceID, executionID string) storage.GetMultipleOp[*Value] {
	prefix := s.InstanceExecutionPrefix(instanceID, executionID)
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.WorkflowExecutionID, item.Event.ID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Put(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.WorkflowExecutionID, item.Event.ID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID, executionID, eventID string) storage.DeleteOp {
	key := s.Key(instanceID, executionID, eventID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *Store) DeleteByInstanceExecution(instanceID, executionID string) storage.DeleteOp {
	prefix := s.InstanceExecutionPrefix(instanceID, executionID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
