package workflow_instance_sticky

import (
	"path"
	"time"

	"github.com/pgEdge/control-plane/internal/storage"
)

type Value struct {
	version            int64     `json:"-"`
	WorkflowInstanceID string    `json:"workflow_instance_id"`
	CreatedAt          time.Time `json:"created_at"`
	WorkerID           string    `json:"worker_id"`
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

func (s *Store) Key(instanceID string) string {
	return path.Join("/", s.root, "workflows", "workflow_instance_stickies", instanceID)
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
