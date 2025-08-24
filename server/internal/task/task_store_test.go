package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

func TestTaskStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when database
		// ID A is a prefix of host ID B.
		store := task.NewTaskStore(client, uuid.NewString())

		err := store.Create(&task.StoredTask{
			Task: &task.Task{
				DatabaseID: "database",
				TaskID:     uuid.New(),
			},
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Create(&task.StoredTask{
			Task: &task.Task{
				DatabaseID: "database2",
				TaskID:     uuid.New(),
			},
		}).Exec(t.Context())
		require.NoError(t, err)

		res, err := store.
			GetAllByDatabaseID("database", task.TaskListOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database", res[0].Task.DatabaseID)

		// Delete by prefix and ensure only the 'database' tasks are deleted.
		_, err = store.DeleteByDatabaseID("database").Exec(t.Context())
		require.NoError(t, err)

		res, err = store.
			GetAllByDatabaseID("database2", task.TaskListOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database2", res[0].Task.DatabaseID)
	})
}
