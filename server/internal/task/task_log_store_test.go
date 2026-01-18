package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

func TestTaskLogEntryStoreKeyGeneration(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	store := task.NewTaskLogEntryStore(client, "test-root")
	taskID := uuid.New()
	entryID := uuid.New()

	t.Run("EntityPrefix for database scope", func(t *testing.T) {
		prefix := store.EntityPrefix(task.ScopeDatabase, "my-database")
		expected := "/test-root/task_log_entries/database/my-database/"
		assert.Equal(t, expected, prefix)
	})

	t.Run("EntityPrefix for host scope", func(t *testing.T) {
		prefix := store.EntityPrefix(task.ScopeHost, "host-1")
		expected := "/test-root/task_log_entries/host/host-1/"
		assert.Equal(t, expected, prefix)
	})

	t.Run("TaskPrefix for database scope", func(t *testing.T) {
		prefix := store.TaskPrefix(task.ScopeDatabase, "my-database", taskID)
		expected := "/test-root/task_log_entries/database/my-database/" + taskID.String() + "/"
		assert.Equal(t, expected, prefix)
	})

	t.Run("Key for database scope", func(t *testing.T) {
		key := store.Key(task.ScopeDatabase, "my-database", taskID, entryID)
		expected := "/test-root/task_log_entries/database/my-database/" + taskID.String() + "/" + entryID.String()
		assert.Equal(t, expected, key)
	})
}

func TestTaskLogEntryStoreCRUD(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("Put and GetAllByTask with database scope", func(t *testing.T) {
		store := task.NewTaskLogEntryStore(client, uuid.NewString())
		taskID := uuid.New()

		// Create log entries
		for i := 0; i < 3; i++ {
			err := store.Put(&task.StoredTaskLogEntry{
				Scope:      task.ScopeDatabase,
				EntityID:   "my-database",
				DatabaseID: "my-database",
				TaskID:     taskID,
				EntryID:    uuid.New(),
				Message:    "test message",
			}).Exec(t.Context())
			require.NoError(t, err)
		}

		// Get all log entries
		entries, err := store.GetAllByTask(task.ScopeDatabase, "my-database", taskID, task.TaskLogOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, entries, 3)
		for _, entry := range entries {
			assert.Equal(t, task.ScopeDatabase, entry.Scope)
			assert.Equal(t, "my-database", entry.EntityID)
			assert.Equal(t, taskID, entry.TaskID)
		}
	})

	t.Run("Put and GetAllByTask with host scope", func(t *testing.T) {
		store := task.NewTaskLogEntryStore(client, uuid.NewString())
		taskID := uuid.New()

		// Create log entries
		for i := 0; i < 2; i++ {
			err := store.Put(&task.StoredTaskLogEntry{
				Scope:    task.ScopeHost,
				EntityID: "host-1",
				TaskID:   taskID,
				EntryID:  uuid.New(),
				Message:  "test message",
			}).Exec(t.Context())
			require.NoError(t, err)
		}

		// Get all log entries
		entries, err := store.GetAllByTask(task.ScopeHost, "host-1", taskID, task.TaskLogOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, entries, 2)
		for _, entry := range entries {
			assert.Equal(t, task.ScopeHost, entry.Scope)
			assert.Equal(t, "host-1", entry.EntityID)
		}
	})
}

func TestTaskLogEntryStoreDelete(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("DeleteByTask", func(t *testing.T) {
		store := task.NewTaskLogEntryStore(client, uuid.NewString())
		taskID := uuid.New()

		// Create log entries
		err := store.Put(&task.StoredTaskLogEntry{
			Scope:      task.ScopeDatabase,
			EntityID:   "my-database",
			DatabaseID: "my-database",
			TaskID:     taskID,
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		// Delete by task
		_, err = store.DeleteByTask(task.ScopeDatabase, "my-database", taskID).Exec(t.Context())
		require.NoError(t, err)

		// Verify entries are gone
		entries, err := store.GetAllByTask(task.ScopeDatabase, "my-database", taskID, task.TaskLogOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, entries, 0)
	})

	t.Run("DeleteByEntity", func(t *testing.T) {
		store := task.NewTaskLogEntryStore(client, uuid.NewString())

		// Create log entries for two entities
		err := store.Put(&task.StoredTaskLogEntry{
			Scope:      task.ScopeDatabase,
			EntityID:   "database-1",
			DatabaseID: "database-1",
			TaskID:     uuid.New(),
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		taskID2 := uuid.New()
		err = store.Put(&task.StoredTaskLogEntry{
			Scope:      task.ScopeDatabase,
			EntityID:   "database-2",
			DatabaseID: "database-2",
			TaskID:     taskID2,
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		// Delete database-1
		_, err = store.DeleteByEntity(task.ScopeDatabase, "database-1").Exec(t.Context())
		require.NoError(t, err)

		// Verify database-2 entries still exist
		entries, err := store.GetAllByTask(task.ScopeDatabase, "database-2", taskID2, task.TaskLogOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, entries, 1)
	})
}

func TestTaskLogEntryStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when database
		// ID A is a prefix of host ID B.
		store := task.NewTaskLogEntryStore(client, uuid.NewString())

		taskID := uuid.New()
		task2ID := uuid.New()

		err := store.Put(&task.StoredTaskLogEntry{
			Scope:      task.ScopeDatabase,
			EntityID:   "database",
			DatabaseID: "database",
			TaskID:     taskID,
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Put(&task.StoredTaskLogEntry{
			Scope:      task.ScopeDatabase,
			EntityID:   "database2",
			DatabaseID: "database2",
			TaskID:     task2ID,
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		res, err := store.
			GetAllByTaskID("database", taskID, task.TaskLogOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database", res[0].DatabaseID)

		// Delete by prefix and ensure only the 'database' tasks are deleted.
		_, err = store.DeleteByDatabaseID("database").Exec(t.Context())
		require.NoError(t, err)

		res, err = store.
			GetAllByTaskID("database2", task2ID, task.TaskLogOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database2", res[0].DatabaseID)
	})
}
