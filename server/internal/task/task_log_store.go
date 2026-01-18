package task

import (
	"time"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredTaskLogEntry struct {
	storage.StoredValue
	Scope      Scope          `json:"scope"`
	EntityID   string         `json:"entity_id"`
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

func NewTaskLogEntryStore(client *clientv3.Client, root string) *TaskLogEntryStore {
	return &TaskLogEntryStore{
		client: client,
		root:   root,
	}
}

func (s *TaskLogEntryStore) Prefix() string {
	return storage.Prefix("/", s.root, "task_log_entries")
}

// EntityPrefix returns the prefix for all task log entries for a given scope and entity.
func (s *TaskLogEntryStore) EntityPrefix(scope Scope, entityID string) string {
	return storage.Prefix(s.Prefix(), scope.String(), entityID)
}

// DatabasePrefix returns the prefix for all task log entries for a given database.
// Deprecated: Use EntityPrefix(ScopeDatabase, databaseID) instead.
func (s *TaskLogEntryStore) DatabasePrefix(databaseID string) string {
	return s.EntityPrefix(ScopeDatabase, databaseID)
}

// TaskPrefix returns the prefix for all log entries for a specific task.
func (s *TaskLogEntryStore) TaskPrefix(scope Scope, entityID string, taskID uuid.UUID) string {
	return storage.Prefix(s.EntityPrefix(scope, entityID), taskID.String())
}

// TaskPrefixDeprecated returns the prefix for all log entries for a specific task.
// Deprecated: Use TaskPrefix(ScopeDatabase, databaseID, taskID) instead.
func (s *TaskLogEntryStore) TaskPrefixDeprecated(databaseID string, taskID uuid.UUID) string {
	return s.TaskPrefix(ScopeDatabase, databaseID, taskID)
}

// Key returns the storage key for a task log entry.
func (s *TaskLogEntryStore) Key(scope Scope, entityID string, taskID, entryID uuid.UUID) string {
	return storage.Key(s.TaskPrefix(scope, entityID, taskID), entryID.String())
}

// KeyDeprecated returns the storage key for a task log entry.
// Deprecated: Use Key(ScopeDatabase, databaseID, taskID, entryID) instead.
func (s *TaskLogEntryStore) KeyDeprecated(databaseID string, taskID, entryID uuid.UUID) string {
	return s.Key(ScopeDatabase, databaseID, taskID, entryID)
}

type TaskLogOptions struct {
	Limit        int
	AfterEntryID uuid.UUID
}

func (s *TaskLogEntryStore) GetAllByTask(scope Scope, entityID string, taskID uuid.UUID, options TaskLogOptions) storage.GetMultipleOp[*StoredTaskLogEntry] {
	rangeStart := s.TaskPrefix(scope, entityID, taskID)
	rangeEnd := clientv3.GetPrefixRangeEnd(rangeStart)

	var opOptions []clientv3.OpOption
	if options.Limit > 0 {
		opOptions = append(opOptions, clientv3.WithLimit(int64(options.Limit)))
	}
	if options.AfterEntryID != uuid.Nil {
		// We intentionally treat this as inclusive so that we still return an
		// entry when AfterEntryID is the last entry. Callers must ignore the
		// entry with EntryID == AfterEntryID.
		rangeStart = s.Key(scope, entityID, taskID, options.AfterEntryID)
	}
	opOptions = append(
		opOptions,
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend), // simulate tail behavior
		clientv3.WithSerializable(),
	)

	return storage.NewGetRangeOp[*StoredTaskLogEntry](s.client, rangeStart, rangeEnd, opOptions...)
}

// GetAllByTaskID retrieves all log entries for a task.
// Deprecated: Use GetAllByTask(ScopeDatabase, databaseID, taskID, options) instead.
func (s *TaskLogEntryStore) GetAllByTaskID(databaseID string, taskID uuid.UUID, options TaskLogOptions) storage.GetMultipleOp[*StoredTaskLogEntry] {
	return s.GetAllByTask(ScopeDatabase, databaseID, taskID, options)
}

func (s *TaskLogEntryStore) Put(item *StoredTaskLogEntry) storage.PutOp[*StoredTaskLogEntry] {
	key := s.Key(item.Scope, item.EntityID, item.TaskID, item.EntryID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *TaskLogEntryStore) DeleteByTask(scope Scope, entityID string, taskID uuid.UUID) storage.DeleteOp {
	prefix := s.TaskPrefix(scope, entityID, taskID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

// DeleteByTaskID deletes all log entries for a task.
// Deprecated: Use DeleteByTask(ScopeDatabase, databaseID, taskID) instead.
func (s *TaskLogEntryStore) DeleteByTaskID(databaseID string, taskID uuid.UUID) storage.DeleteOp {
	return s.DeleteByTask(ScopeDatabase, databaseID, taskID)
}

func (s *TaskLogEntryStore) DeleteByEntity(scope Scope, entityID string) storage.DeleteOp {
	prefix := s.EntityPrefix(scope, entityID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}

// DeleteByDatabaseID deletes all log entries for a database.
// Deprecated: Use DeleteByEntity(ScopeDatabase, databaseID) instead.
func (s *TaskLogEntryStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	return s.DeleteByEntity(ScopeDatabase, databaseID)
}
