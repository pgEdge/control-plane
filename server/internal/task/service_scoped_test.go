package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

func TestServiceScoped(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("Create and get database task", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create database task
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		assert.Equal(t, task.ScopeDatabase, tsk.Scope)
		assert.Equal(t, "database-1", tsk.EntityID)
		assert.Equal(t, "database-1", tsk.DatabaseID)
		assert.Equal(t, task.TypeCreate, tsk.Type)
		assert.Equal(t, task.StatusPending, tsk.Status)

		// Get task by scope and entity ID
		retrieved, err := svc.GetTask(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID)
		require.NoError(t, err)
		assert.Equal(t, tsk.TaskID, retrieved.TaskID)
		assert.Equal(t, task.ScopeDatabase, retrieved.Scope)
		assert.Equal(t, "database-1", retrieved.EntityID)
	})

	t.Run("Create and get host task", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create host task
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:  task.ScopeHost,
			HostID: "host-1",
			Type:   task.TypeRemoveHost,
		})
		require.NoError(t, err)
		assert.Equal(t, task.ScopeHost, tsk.Scope)
		assert.Equal(t, "host-1", tsk.EntityID)
		assert.Equal(t, "host-1", tsk.HostID)
		assert.Equal(t, task.TypeRemoveHost, tsk.Type)

		// Get task by scope and entity ID
		retrieved, err := svc.GetTask(t.Context(), task.ScopeHost, "host-1", tsk.TaskID)
		require.NoError(t, err)
		assert.Equal(t, tsk.TaskID, retrieved.TaskID)
		assert.Equal(t, task.ScopeHost, retrieved.Scope)
		assert.Equal(t, "host-1", retrieved.EntityID)
	})

	t.Run("Get tasks by entity", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create multiple database tasks
		for i := 0; i < 3; i++ {
			_, err := svc.CreateTask(t.Context(), task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "database-1",
				Type:       task.TypeCreate,
			})
			require.NoError(t, err)
		}

		// Create task for different database
		_, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-2",
			Type:       task.TypeUpdate,
		})
		require.NoError(t, err)

		// Create host task
		_, err = svc.CreateTask(t.Context(), task.Options{
			Scope:  task.ScopeHost,
			HostID: "host-1",
			Type:   task.TypeRemoveHost,
		})
		require.NoError(t, err)

		// Get tasks for database-1
		tasks, err := svc.GetTasks(t.Context(), task.ScopeDatabase, "database-1", task.TaskListOptions{})
		require.NoError(t, err)
		assert.Len(t, tasks, 3)
		for _, tsk := range tasks {
			assert.Equal(t, task.ScopeDatabase, tsk.Scope)
			assert.Equal(t, "database-1", tsk.EntityID)
		}

		// Get tasks for database-2
		tasks, err = svc.GetTasks(t.Context(), task.ScopeDatabase, "database-2", task.TaskListOptions{})
		require.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, "database-2", tasks[0].EntityID)

		// Get tasks for host-1
		tasks, err = svc.GetTasks(t.Context(), task.ScopeHost, "host-1", task.TaskListOptions{})
		require.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, task.ScopeHost, tasks[0].Scope)
		assert.Equal(t, "host-1", tasks[0].EntityID)
	})

	t.Run("Add and get task log", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create database task
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)

		// Add log entry
		err = svc.AddLogEntry(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.LogEntry{
			Message: "Starting task",
			Fields:  map[string]any{"step": 1},
		})
		require.NoError(t, err)

		// Get task log
		log, err := svc.GetTaskLog(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.TaskLogOptions{})
		require.NoError(t, err)
		assert.Equal(t, tsk.TaskID, log.TaskID)
		assert.Len(t, log.Entries, 1)
		assert.Equal(t, "Starting task", log.Entries[0].Message)
		assert.Equal(t, float64(1), log.Entries[0].Fields["step"]) // JSON encoding converts int to float64

		// Add more log entries
		err = svc.AddLogEntry(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.LogEntry{
			Message: "Task completed",
			Fields:  map[string]any{"step": 2},
		})
		require.NoError(t, err)

		// Get updated log
		log, err = svc.GetTaskLog(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.TaskLogOptions{})
		require.NoError(t, err)
		assert.Len(t, log.Entries, 2)
	})

	t.Run("Update task", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create task
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)
		assert.Equal(t, task.StatusPending, tsk.Status)

		// Update task
		tsk.Start()
		err = svc.UpdateTask(t.Context(), tsk)
		require.NoError(t, err)

		// Verify update
		retrieved, err := svc.GetTask(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID)
		require.NoError(t, err)
		assert.Equal(t, task.StatusRunning, retrieved.Status)
	})

	t.Run("Delete task", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create task
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)

		// Delete task
		err = svc.DeleteTask(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID)
		require.NoError(t, err)

		// Verify deletion
		_, err = svc.GetTask(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID)
		assert.ErrorIs(t, err, task.ErrTaskNotFound)
	})

	t.Run("Delete all tasks", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create multiple tasks
		for i := 0; i < 3; i++ {
			_, err := svc.CreateTask(t.Context(), task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "database-1",
				Type:       task.TypeCreate,
			})
			require.NoError(t, err)
		}

		// Delete all tasks
		err := svc.DeleteAllTasks(t.Context(), task.ScopeDatabase, "database-1")
		require.NoError(t, err)

		// Verify deletion
		tasks, err := svc.GetTasks(t.Context(), task.ScopeDatabase, "database-1", task.TaskListOptions{})
		require.NoError(t, err)
		assert.Len(t, tasks, 0)
	})

	t.Run("Delete task logs", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create task with logs
		tsk, err := svc.CreateTask(t.Context(), task.Options{
			Scope:      task.ScopeDatabase,
			DatabaseID: "database-1",
			Type:       task.TypeCreate,
		})
		require.NoError(t, err)

		// Add log entries
		for i := 0; i < 3; i++ {
			err = svc.AddLogEntry(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.LogEntry{
				Message: "Log entry",
			})
			require.NoError(t, err)
		}

		// Delete logs
		err = svc.DeleteTaskLogs(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID)
		require.NoError(t, err)

		// Verify deletion
		log, err := svc.GetTaskLog(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.TaskLogOptions{})
		require.NoError(t, err)
		assert.Len(t, log.Entries, 0)
	})

	t.Run("Delete all task logs", func(t *testing.T) {
		store := task.NewStore(client, uuid.NewString())
		svc := task.NewService(store)

		// Create multiple tasks with logs
		for i := 0; i < 2; i++ {
			tsk, err := svc.CreateTask(t.Context(), task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "database-1",
				Type:       task.TypeCreate,
			})
			require.NoError(t, err)

			err = svc.AddLogEntry(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.LogEntry{
				Message: "Log entry",
			})
			require.NoError(t, err)
		}

		// Delete all logs
		err := svc.DeleteAllTaskLogs(t.Context(), task.ScopeDatabase, "database-1")
		require.NoError(t, err)

		// Verify all logs deleted
		tasks, err := svc.GetTasks(t.Context(), task.ScopeDatabase, "database-1", task.TaskListOptions{})
		require.NoError(t, err)
		for _, tsk := range tasks {
			log, err := svc.GetTaskLog(t.Context(), task.ScopeDatabase, "database-1", tsk.TaskID, task.TaskLogOptions{})
			require.NoError(t, err)
			assert.Len(t, log.Entries, 0)
		}
	})
}
