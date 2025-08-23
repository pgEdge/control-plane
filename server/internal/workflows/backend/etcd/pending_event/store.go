package pending_event

import (
	"github.com/cschleiden/go-workflows/backend/history"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstanceID  string         `json:"workflow_instance_id"`
	WorkflowExecutionID string         `json:"workflow_execution_id"`
	Event               *history.Event `json:"event"`
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

func (s *Store) AllEventsPrefix() string {
	return storage.Prefix("/", s.root, "workflows", "pending_events")
}

func (s *Store) InstanceExecutionPrefix(instanceID, executionID string) string {
	return storage.Prefix(s.AllEventsPrefix(), instanceID, executionID)
}

func (s *Store) Key(instanceID, executionID, eventID string) string {
	return storage.Key(s.InstanceExecutionPrefix(instanceID, executionID), eventID)
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
