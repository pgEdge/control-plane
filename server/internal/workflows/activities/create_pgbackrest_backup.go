package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type CreatePgBackRestBackupInput struct {
	DatabaseID    string                    `json:"database_id"`
	InstanceID    string                    `json:"instance_id"`
	TaskID        uuid.UUID                 `json:"task_id"`
	BackupOptions *pgbackrest.BackupOptions `json:"backup_options"`
}

type CreatePgBackRestBackupOutput struct{}

func (a *Activities) ExecuteCreatePgBackRestBackup(
	ctx workflow.Context,
	hostID string,
	input *CreatePgBackRestBackupInput,
) workflow.Future[*CreatePgBackRestBackupOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CreatePgBackRestBackupOutput](ctx, options, a.CreatePgBackRestBackup, input)
}

func (a *Activities) CreatePgBackRestBackup(ctx context.Context, input *CreatePgBackRestBackupInput) (*CreatePgBackRestBackupOutput, error) {
	logger := activity.Logger(ctx).With("instance_id", input.InstanceID)
	logger.Info("running pgbackrest backup")

	taskSvc, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	originalState, err := a.DatabaseService.GetStoredInstanceState(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current instance state: %w", err)
	}

	err = a.DatabaseService.UpdateInstanceState(ctx, &database.InstanceStateUpdateOptions{
		InstanceID: input.InstanceID,
		DatabaseID: input.DatabaseID,
		State:      database.InstanceStateBackingUp,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update instance: %w", err)
	}

	defer func() {
		// Backing up the database doesn't affect availability, so we always set
		// the instance back to its original state.
		err = a.DatabaseService.UpdateInstanceState(ctx, &database.InstanceStateUpdateOptions{
			InstanceID: input.InstanceID,
			DatabaseID: input.DatabaseID,
			State:      originalState,
		})
		if err != nil {
			logger.With("error", err).Error("failed to restore instance to original state")
		}
	}()

	taskLogWriter := task.NewTaskLogWriter(ctx, taskSvc, task.ScopeDatabase, input.DatabaseID, input.TaskID)
	defer taskLogWriter.Close()

	err = a.DatabaseService.CreatePgBackRestBackup(ctx, taskLogWriter, input.DatabaseID, input.InstanceID, input.BackupOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgBackRest backup: %w", err)
	}

	return &CreatePgBackRestBackupOutput{}, nil
}
