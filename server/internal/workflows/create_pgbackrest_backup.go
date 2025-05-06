package workflows

import (
	"fmt"
	"math/rand/v2"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type InstanceHost struct {
	InstanceID uuid.UUID `json:"instance_id"`
	HostID     uuid.UUID `json:"host_id"`
}

type CreatePgBackRestBackupInput struct {
	Task              *task.Task                `json:"task"`
	BackupFromStandby bool                      `json:"backup_from_standby"`
	Instances         []*InstanceHost           `json:"instances"`
	Options           *pgbackrest.BackupOptions `json:"options"`
}

type CreatePgBackRestBackupOutput struct{}

func (w *Workflows) CreatePgBackRestBackup(ctx workflow.Context, input *CreatePgBackRestBackupInput) (*CreatePgBackRestBackupOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.Task.DatabaseID.String(),
		"task_id", input.Task.TaskID.String(),
		"backup_from_standby", input.BackupFromStandby,
	)
	logger.Info("creating pgbackrest backup")

	t := input.Task

	var handleError = func(err error) error {
		logger.With("error", err).Error("failed to create pgbackrest backup")

		t.SetFailed(err)
		updateTaskInput := &activities.UpdateTaskInput{
			Task: t,
		}
		_, taskErr := w.Activities.
			ExecuteUpdateTask(ctx, w.Config.HostID, updateTaskInput).
			Get(ctx)
		if taskErr != nil {
			logger.With("error", taskErr).Error("failed to update task state")
		}
		return err
	}

	wf := workflow.WorkflowInstance(ctx)

	t.SetRunning(wf.InstanceID, wf.ExecutionID)
	updateTaskInput := &activities.UpdateTaskInput{
		Task: t,
	}
	_, err := w.Activities.
		ExecuteUpdateTask(ctx, w.Config.HostID, updateTaskInput).
		Get(ctx)
	if err != nil {
		logger.With("error", err).Error("failed to update task state")
		return nil, err
	}

	instance, err := workflow.SideEffect(ctx, func(_ workflow.Context) *InstanceHost {
		idx := rand.IntN(len(input.Instances))
		return input.Instances[idx]
	}).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get random instance: %w", err))
	}

	getPrimaryInput := &activities.GetPrimaryInstanceInput{
		DatabaseID: input.Task.DatabaseID,
		InstanceID: instance.InstanceID,
	}
	getPrimaryOutput, err := w.Activities.
		ExecuteGetPrimaryInstance(ctx, instance.HostID, getPrimaryInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get primary instance: %w", err))
	}

	var backupInstance *InstanceHost
	for _, instance := range input.Instances {
		if input.BackupFromStandby {
			if instance.InstanceID != getPrimaryOutput.PrimaryInstanceID {
				backupInstance = instance
				break
			}
		} else {
			if instance.InstanceID == getPrimaryOutput.PrimaryInstanceID {
				backupInstance = instance
				break
			}
		}
	}
	if backupInstance == nil {
		return nil, handleError(fmt.Errorf("no suitable instance found to run backup"))
	}

	backupInput := &activities.CreatePgBackRestBackupInput{
		DatabaseID: input.Task.DatabaseID,
		InstanceID: backupInstance.InstanceID,
		TaskID:     input.Task.TaskID,
		Options:    input.Options,
	}
	_, err = w.Activities.
		ExecuteCreatePgBackRestBackup(ctx, backupInstance.HostID, backupInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to create pgbackrest backup: %w", err))
	}

	t.SetCompleted()
	updateTaskInput = &activities.UpdateTaskInput{
		Task: t,
	}
	_, err = w.Activities.
		ExecuteUpdateTask(ctx, w.Config.HostID, updateTaskInput).
		Get(ctx)
	if err != nil {
		logger.With("error", err).Error("failed to update task state")
		return nil, err
	}

	logger.Info("successfully created pgbackrest backup")

	return &CreatePgBackRestBackupOutput{}, nil
}
