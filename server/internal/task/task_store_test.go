package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

func TestTaskStoreKeyGeneration(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	store := task.NewTaskStore(client, "test-root")
	taskID := uuid.New()

	t.Run("EntityPrefix for database scope", func(t *testing.T) {
		prefix := store.EntityPrefix(task.ScopeDatabase, "my-database")
		expected := "/test-root/tasks_v2/database/my-database/"
		assert.Equal(t, expected, prefix)
	})

	t.Run("EntityPrefix for host scope", func(t *testing.T) {
		prefix := store.EntityPrefix(task.ScopeHost, "host-1")
		expected := "/test-root/tasks_v2/host/host-1/"
		assert.Equal(t, expected, prefix)
	})

	t.Run("Key for database scope", func(t *testing.T) {
		key := store.Key(task.ScopeDatabase, "my-database", taskID)
		expected := "/test-root/tasks_v2/database/my-database/" + taskID.String()
		assert.Equal(t, expected, key)
	})

	t.Run("Key for host scope", func(t *testing.T) {
		key := store.Key(task.ScopeHost, "host-1", taskID)
		expected := "/test-root/tasks_v2/host/host-1/" + taskID.String()
		assert.Equal(t, expected, key)
	})
}

func TestTaskStoreCRUD(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("Create and GetByKey with database scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		tsk, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "my-database",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)

		err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
		require.NoError(t, err)

		stored, err := store.GetByKey(task.ScopeDatabase, "my-database", tsk.TaskID).Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, tsk.TaskID, stored.Task.TaskID)
		assert.Equal(t, task.ScopeDatabase, stored.Task.Scope)
		assert.Equal(t, "my-database", stored.Task.EntityID)
	})

	t.Run("Create and GetByKey with host scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		tsk, err := task.NewTask(task.Options{
			Scope:  task.ScopeHost,
			HostID: "host-1",
			Type:   task.TypeRemoveHost,
		})
		require.NoError(t, err)

		err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
		require.NoError(t, err)

		stored, err := store.GetByKey(task.ScopeHost, "host-1", tsk.TaskID).Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, tsk.TaskID, stored.Task.TaskID)
		assert.Equal(t, task.ScopeHost, stored.Task.Scope)
		assert.Equal(t, "host-1", stored.Task.EntityID)
	})

	t.Run("Update task", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		tsk, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "my-database",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)

		err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
		require.NoError(t, err)

		// Get the stored task to update it
		stored, err := store.GetByKey(task.ScopeDatabase, "my-database", tsk.TaskID).Exec(t.Context())
		require.NoError(t, err)

		// Update the task
		stored.Task.Start()
		err = store.Update(stored).Exec(t.Context())
		require.NoError(t, err)

		// Verify update
		updated, err := store.GetByKey(task.ScopeDatabase, "my-database", tsk.TaskID).Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, task.StatusRunning, updated.Task.Status)
	})
}

func TestTaskStoreGetAllByEntity(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("GetAllByEntity with database scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		// Create tasks for two different databases
		for i := 0; i < 3; i++ {
			tsk, err := task.NewTask(task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "database-1",
				Type:       task.TypeCreate,
			})
			require.NoError(t, err)
			err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
			require.NoError(t, err)
		}

		for i := 0; i < 2; i++ {
			tsk, err := task.NewTask(task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "database-2",
				Type:       task.TypeCreate,
			})
			require.NoError(t, err)
			err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
			require.NoError(t, err)
		}

		// Get all tasks for database-1
		tasks, err := store.GetAllByEntity(task.ScopeDatabase, "database-1", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 3)
		for _, stored := range tasks {
			assert.Equal(t, "database-1", stored.Task.EntityID)
			assert.Equal(t, task.ScopeDatabase, stored.Task.Scope)
		}

		// Get all tasks for database-2
		tasks, err = store.GetAllByEntity(task.ScopeDatabase, "database-2", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 2)
		for _, stored := range tasks {
			assert.Equal(t, "database-2", stored.Task.EntityID)
		}
	})

	t.Run("GetAllByEntity with host scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		// Create tasks for a host
		for i := 0; i < 2; i++ {
			tsk, err := task.NewTask(task.Options{
				Scope:  task.ScopeHost,
				HostID: "host-1",
				Type:   task.TypeRemoveHost,
			})
			require.NoError(t, err)
			err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
			require.NoError(t, err)
		}

		// Get all tasks for host-1
		tasks, err := store.GetAllByEntity(task.ScopeHost, "host-1", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 2)
		for _, stored := range tasks {
			assert.Equal(t, "host-1", stored.Task.EntityID)
			assert.Equal(t, task.ScopeHost, stored.Task.Scope)
		}
	})
}

func TestTaskStoreDelete(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("DeleteByEntity with database scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		// Create tasks
		tsk1, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		err = store.Create(&task.StoredTask{Task: tsk1}).Exec(t.Context())
		require.NoError(t, err)

		tsk2, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-2",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		err = store.Create(&task.StoredTask{Task: tsk2}).Exec(t.Context())
		require.NoError(t, err)

		// Delete database-1 tasks
		_, err = store.DeleteByEntity(task.ScopeDatabase, "database-1").Exec(t.Context())
		require.NoError(t, err)

		// Verify database-1 tasks are gone
		tasks, err := store.GetAllByEntity(task.ScopeDatabase, "database-1", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 0)

		// Verify database-2 tasks still exist
		tasks, err = store.GetAllByEntity(task.ScopeDatabase, "database-2", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 1)
	})

	t.Run("DeleteByEntity with host scope", func(t *testing.T) {
		store := task.NewTaskStore(client, uuid.NewString())

		tsk, err := task.NewTask(task.Options{
			Scope:  task.ScopeHost,
			HostID: "host-1",
			Type:   task.TypeRemoveHost,
		})
		require.NoError(t, err)
		err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
		require.NoError(t, err)

		// Delete host tasks
		_, err = store.DeleteByEntity(task.ScopeHost, "host-1").Exec(t.Context())
		require.NoError(t, err)

		// Verify tasks are gone
		tasks, err := store.GetAllByEntity(task.ScopeHost, "host-1", task.TaskListOptions{}).Exec(t.Context())
		require.NoError(t, err)
		assert.Len(t, tasks, 0)
	})
}

func TestTaskStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when database
		// ID A is a prefix of host ID B.
		store := task.NewTaskStore(client, uuid.NewString())

		tsk, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		err = store.Create(&task.StoredTask{Task: tsk}).Exec(t.Context())
		require.NoError(t, err)

		tsk2, err := task.NewTask(task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database2",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		err = store.Create(&task.StoredTask{Task: tsk2}).Exec(t.Context())
		require.NoError(t, err)

		res, err := store.
			GetAllByEntity(task.ScopeDatabase, "database", task.TaskListOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database", res[0].Task.DatabaseID)

		// Delete by prefix and ensure only the 'database' tasks are deleted.
		_, err = store.DeleteByEntity(task.ScopeDatabase, "database").Exec(t.Context())
		require.NoError(t, err)

		res, err = store.
			GetAllByEntity(task.ScopeDatabase, "database2", task.TaskListOptions{}).
			Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database2", res[0].Task.DatabaseID)
	})
}
