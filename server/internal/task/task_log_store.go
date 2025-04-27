package task

import (
	"path"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredTaskLogLine struct {
	storage.StoredValue
	DatabaseID uuid.UUID `json:"database_id"`
	TaskID     uuid.UUID `json:"task_id"`
	LineID     uuid.UUID `json:"line_id"`
	Line       string    `json:"line"`
}

type TaskLogLineStore struct {
	client *clientv3.Client
	root   string
}

func NewTaskLogLineStore(client *clientv3.Client, root string) *TaskLogLineStore {
	return &TaskLogLineStore{
		client: client,
		root:   root,
	}
}

func (s *TaskLogLineStore) Prefix() string {
	return path.Join("/", s.root, "task_log_lines")
}

func (s *TaskLogLineStore) DatabasePrefix(databaseID uuid.UUID) string {
	return path.Join(s.Prefix(), databaseID.String())
}

func (s *TaskLogLineStore) TaskPrefix(databaseID, taskID uuid.UUID) string {
	return path.Join(s.DatabasePrefix(databaseID), taskID.String())
}

func (s *TaskLogLineStore) Key(databaseID, taskID, lineID uuid.UUID) string {
	return path.Join(s.TaskPrefix(databaseID, taskID), lineID.String())
}

type TaskLogOptions struct {
	Limit       int
	AfterLineID uuid.UUID
}

func (s *TaskLogLineStore) GetAllByTaskID(databaseID, taskID uuid.UUID, options TaskLogOptions) storage.GetMultipleOp[*StoredTaskLogLine] {
	rangeStart := s.TaskPrefix(databaseID, taskID)
	rangeEnd := clientv3.GetPrefixRangeEnd(rangeStart)

	var opOptions []clientv3.OpOption
	if options.Limit > 0 {
		opOptions = append(opOptions, clientv3.WithLimit(int64(options.Limit)))
	}
	if options.AfterLineID != uuid.Nil {
		rangeStart = s.Key(databaseID, taskID, options.AfterLineID) + "0"
	}
	opOptions = append(
		opOptions,
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend), // simulate tail behavior
		clientv3.WithSerializable(),
	)

	return storage.NewGetRangeOp[*StoredTaskLogLine](s.client, rangeStart, rangeEnd, opOptions...)
}

func (s *TaskLogLineStore) Put(item *StoredTaskLogLine) storage.PutOp[*StoredTaskLogLine] {
	key := s.Key(item.DatabaseID, item.TaskID, item.LineID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *TaskLogLineStore) DeleteByTaskID(databaseID, taskID uuid.UUID) storage.DeleteOp {
	prefix := s.TaskPrefix(databaseID, taskID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *TaskLogLineStore) DeleteByDatabaseID(databaseID uuid.UUID) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
