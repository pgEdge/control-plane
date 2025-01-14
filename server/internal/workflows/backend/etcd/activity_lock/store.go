package activity_lock

import (
	"path"
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Value struct {
	version            int64     `json:"-"`
	WorkflowInstanceID string    `json:"workflow_instance_id"`
	EventID            string    `json:"event_id"`
	CreatedAt          time.Time `json:"created_at"`
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

func (s *Store) Key(instanceID, eventID string) string {
	return path.Join("/", s.root, "workflows", "activity_locks", instanceID, eventID)
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
