package task

import (
	"time"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredTaskLogEntry struct {
	storage.StoredValue
	DatabaseID string         `json:"database_id"`
	TaskID     uuid.UUID      `json:"task_id"`
	EntryID    uuid.UUID      `json:"entry_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Message    string         `json:"message"`
	Fields     map[string]any `json:"fields"`
}

type TaskLogEntryStore struct {
	client *clientv3.Client
	root   string
}

func NewTaskLogMessageStore(client *clientv3.Client, root string) *TaskLogEntryStore {
	return &TaskLogEntryStore{
		client: client,
		root:   root,
	}
}

func (s *TaskLogEntryStore) Prefix() string {
	return storage.Prefix("/", s.root, "task_log_messages")
}

func (s *TaskLogEntryStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *TaskLogEntryStore) TaskPrefix(databaseID string, taskID uuid.UUID) string {
	return storage.Prefix(s.DatabasePrefix(databaseID), taskID.String())
}

func (s *TaskLogEntryStore) Key(databaseID string, taskID, entryID uuid.UUID) string {
	return storage.Key(s.TaskPrefix(databaseID, taskID), entryID.String())
}

type TaskLogOptions struct {
	Limit        int
	AfterEntryID uuid.UUID
}

func (s *TaskLogEntryStore) GetAllByTaskID(databaseID string, taskID uuid.UUID, options TaskLogOptions) storage.GetMultipleOp[*StoredTaskLogEntry] {
	rangeStart := s.TaskPrefix(databaseID, taskID)
	rangeEnd := clientv3.GetPrefixRangeEnd(rangeStart)

	var opOptions []clientv3.OpOption
	if options.Limit > 0 {
		opOptions = append(opOptions, clientv3.WithLimit(int64(options.Limit)))
	}
	if options.AfterEntryID != uuid.Nil {
		rangeStart = s.Key(databaseID, taskID, options.AfterEntryID) + "0"
	}
	opOptions = append(
		opOptions,
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend), // simulate tail behavior
		clientv3.WithSerializable(),
	)

	return storage.NewGetRangeOp[*StoredTaskLogEntry](s.client, rangeStart, rangeEnd, opOptions...)
}

func (s *TaskLogEntryStore) Put(item *StoredTaskLogEntry) storage.PutOp[*StoredTaskLogEntry] {
	key := s.Key(item.DatabaseID, item.TaskID, item.EntryID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *TaskLogEntryStore) DeleteByTaskID(databaseID string, taskID uuid.UUID) storage.DeleteOp {
	prefix := s.TaskPrefix(databaseID, taskID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

func (s *TaskLogEntryStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
