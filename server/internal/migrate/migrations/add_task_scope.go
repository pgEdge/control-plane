package migrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type AddTaskScope struct{}

func (a *AddTaskScope) Identifier() string {
	return "add_task_scope"
}

func (a *AddTaskScope) Run(ctx context.Context, i *do.Injector) error {
	cfg, err := do.Invoke[config.Config](i)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	logger, err := do.Invoke[zerolog.Logger](i)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	client, err := do.Invoke[*clientv3.Client](i)
	if err != nil {
		return fmt.Errorf("failed to initialize client: %w", err)
	}
	taskStore, err := do.Invoke[*task.Store](i)
	if err != nil {
		return fmt.Errorf("failed to initialize task store: %w", err)
	}

	logger = logger.With().
		Str("component", "migration").
		Str("identifier", a.Identifier()).
		Logger()

	oldTasksPrefix := storage.Prefix("/", cfg.EtcdKeyRoot, "tasks")
	oldTaskRangeOp := storage.NewGetPrefixOp[*oldStoredTask](client, oldTasksPrefix)
	oldTasks, err := oldTaskRangeOp.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for old tasks: %w", err)
	}

	for _, oldTask := range oldTasks {
		err := taskStore.Task.Create(oldTask.convert()).Exec(ctx)
		switch {
		case errors.Is(err, storage.ErrAlreadyExists):
			logger.Info().
				Stringer("task_id", oldTask.Task.TaskID).
				Msg("task has already been migrated, skipping")
		case err != nil:
			return fmt.Errorf("failed to migrate task %s: %w", oldTask.Task.TaskID, err)
		}
	}

	oldTaskLogsPrefix := storage.Prefix("/", cfg.EtcdKeyRoot, "task_log_messages")
	oldTaskLogsRangeOp := storage.NewGetPrefixOp[*oldStoredTaskLogEntry](client, oldTaskLogsPrefix)
	oldTaskLogs, err := oldTaskLogsRangeOp.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for old task logs: %w", err)
	}

	for _, oldTaskLog := range oldTaskLogs {
		err := taskStore.TaskLogMessage.Put(oldTaskLog.convert()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to migrate task log entry %s for task %s: %w", oldTaskLog.EntryID, oldTaskLog.TaskID, err)
		}
	}

	return nil
}

type oldStoredTask struct {
	storage.StoredValue
	Task struct {
		ParentID            uuid.UUID   `json:"parent_id"`
		DatabaseID          string      `json:"database_id"`
		NodeName            string      `json:"node_name"`
		InstanceID          string      `json:"instance_id"`
		HostID              string      `json:"host_id"`
		TaskID              uuid.UUID   `json:"task_id"`
		CreatedAt           time.Time   `json:"created_at"`
		CompletedAt         time.Time   `json:"completed_at"`
		Type                task.Type   `json:"type"`
		WorkflowInstanceID  string      `json:"workflow_id"`
		WorkflowExecutionID string      `json:"workflow_execution_id"`
		Status              task.Status `json:"status"`
		Error               string      `json:"error"`
	} `json:"task"`
}

func (o *oldStoredTask) convert() *task.StoredTask {
	return &task.StoredTask{
		Task: &task.Task{
			Scope:               task.ScopeDatabase,
			EntityID:            o.Task.DatabaseID,
			TaskID:              o.Task.TaskID,
			Type:                o.Task.Type,
			ParentID:            o.Task.ParentID,
			Status:              o.Task.Status,
			Error:               o.Task.Error,
			HostID:              o.Task.HostID,
			DatabaseID:          o.Task.DatabaseID,
			NodeName:            o.Task.NodeName,
			InstanceID:          o.Task.InstanceID,
			CreatedAt:           o.Task.CreatedAt,
			CompletedAt:         o.Task.CompletedAt,
			WorkflowInstanceID:  o.Task.WorkflowInstanceID,
			WorkflowExecutionID: o.Task.WorkflowExecutionID,
		},
	}
}

type oldStoredTaskLogEntry struct {
	storage.StoredValue
	DatabaseID string         `json:"database_id"`
	TaskID     uuid.UUID      `json:"task_id"`
	EntryID    uuid.UUID      `json:"entry_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Message    string         `json:"message"`
	Fields     map[string]any `json:"fields"`
}

func (o *oldStoredTaskLogEntry) convert() *task.StoredTaskLogEntry {
	return &task.StoredTaskLogEntry{
		Scope:      task.ScopeDatabase,
		EntityID:   o.DatabaseID,
		DatabaseID: o.DatabaseID,
		TaskID:     o.TaskID,
		EntryID:    o.EntryID,
		Timestamp:  o.Timestamp,
		Message:    o.Message,
		Fields:     o.Fields,
	}
}
