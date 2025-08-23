package workflow_instance

import (
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
	CreatedAt        time.Time                  `json:"created_at"`
	FinishedAt       *time.Time                 `json:"finished_at,omitempty"`
	Queue            core.Queue                 `json:"queue"`
	Metadata         *metadata.WorkflowMetadata `json:"metadata"`
	State            core.WorkflowInstanceState `json:"state"`
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

func (s *Store) InstancesPrefix() string {
	return storage.Prefix("/", s.root, "workflows", "instances")
}

func (s *Store) InstanceIDPrefix(instanceID string) string {
	return storage.Prefix(s.InstancesPrefix(), instanceID)
}

func (s *Store) Key(instanceID, executionID string) string {
	return storage.Key(s.InstanceIDPrefix(instanceID), executionID)
}

func (s *Store) ExistsByKey(instanceID, executionID string) storage.ExistsOp {
	key := s.Key(instanceID, executionID)
	return storage.NewExistsOp(s.client, key)
}

func (s *Store) GetByKey(instanceID, executionID string) storage.GetOp[*Value] {
	key := s.Key(instanceID, executionID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) GetByInstanceID(instanceID string) storage.GetMultipleOp[*Value] {
	prefix := s.InstanceIDPrefix(instanceID)
	return storage.NewGetPrefixOp[*Value](s.client, prefix)
}

func (s *Store) GetAll() storage.GetMultipleOp[*Value] {
	prefix := s.InstancesPrefix()
	return storage.NewGetPrefixOp[*Value](s.client, prefix,
		clientv3.WithSort(clientv3.SortByCreateRevision, clientv3.SortDescend))
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID, executionID string) storage.DeleteOp {
	key := s.Key(instanceID, executionID)
	return storage.NewDeleteKeyOp(s.client, key)
}
