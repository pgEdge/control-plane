package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

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
			DatabaseID: "database",
			TaskID:     taskID,
			EntryID:    uuid.New(),
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Put(&task.StoredTaskLogEntry{
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
