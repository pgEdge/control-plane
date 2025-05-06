package task

import (
	"path"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredTask struct {
	storage.StoredValue
	Task *Task `json:"task"`
}

type TaskStore struct {
	client *clientv3.Client
	root   string
}

func NewTaskStore(client *clientv3.Client, root string) *TaskStore {
	return &TaskStore{
		client: client,
		root:   root,
	}
}

func (s *TaskStore) Prefix() string {
	return path.Join("/", s.root, "tasks")
}

func (s *TaskStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *TaskStore) Key(databaseID, taskID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), taskID.String())
}

func (s *TaskStore) GetByKey(databaseID, taskID uuid.UUID) storage.GetOp[*StoredTask] {
	key := s.Key(databaseID, taskID)
	return storage.NewGetOp[*StoredTask](s.client, key)
}

type SortOrder string

func (s SortOrder) String() string {
	return string(s)
}

const (
	SortAscend  SortOrder = "ascend"
	SortDescend SortOrder = "descend"
)

type TaskListOptions struct {
	Limit       int
	AfterTaskID uuid.UUID
	SortOrder   SortOrder
}

func (s *TaskStore) GetAllByDatabaseID(databaseID uuid.UUID, options TaskListOptions) storage.GetMultipleOp[*StoredTask] {
	rangeStart := s.DatabasePrefix(databaseID)
	rangeEnd := clientv3.GetPrefixRangeEnd(rangeStart)

	var opOptions []clientv3.OpOption
	if options.Limit > 0 {
		opOptions = append(opOptions, clientv3.WithLimit(int64(options.Limit)))
	}
	sortOrder := clientv3.SortDescend
	if options.SortOrder == SortAscend {
		sortOrder = clientv3.SortAscend
	}
	if options.AfterTaskID != uuid.Nil {
		switch sortOrder {
		case clientv3.SortAscend:
			rangeStart = s.Key(databaseID, options.AfterTaskID) + "0"
		case clientv3.SortDescend:
			rangeEnd = s.Key(databaseID, options.AfterTaskID)
		}
	}

	opOptions = append(opOptions, clientv3.WithSort(clientv3.SortByKey, sortOrder))

	return storage.NewGetRangeOp[*StoredTask](s.client, rangeStart, rangeEnd, opOptions...)
}

func (s *TaskStore) Create(item *StoredTask) storage.PutOp[*StoredTask] {
	key := s.Key(item.Task.DatabaseID, item.Task.TaskID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *TaskStore) Update(item *StoredTask) storage.PutOp[*StoredTask] {
	key := s.Key(item.Task.DatabaseID, item.Task.TaskID)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *TaskStore) Delete(databaseID, taskID uuid.UUID) storage.DeleteOp {
	key := s.Key(databaseID, taskID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *TaskStore) DeleteByDatabaseID(databaseID uuid.UUID) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
