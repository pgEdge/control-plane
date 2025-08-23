package workflow_instance_sticky

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	storage.StoredValue
	WorkflowInstanceID string    `json:"workflow_instance_id"`
	CreatedAt          time.Time `json:"created_at"`
	WorkerID           string    `json:"worker_id"`
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

func (s *Store) Key(instanceID string) string {
	return storage.Key("/", s.root, "workflows", "workflow_instance_stickies", instanceID)
}

func (s *Store) GetByKey(instanceID string) storage.GetOp[*Value] {
	key := s.Key(instanceID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) Update(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *Store) Put(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID string) storage.DeleteOp {
	key := s.Key(instanceID)
	return storage.NewDeleteKeyOp(s.client, key)
}
