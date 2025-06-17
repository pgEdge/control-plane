package workflows

import (
	"fmt"
	"math/rand/v2"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type InstanceHost struct {
	InstanceID string `json:"instance_id"`
	HostID     string `json:"host_id"`
}

type CreatePgBackRestBackupInput struct {
	DatabaseID        string                    `json:"database_id"`
	TaskID            uuid.UUID                 `json:"task_id"`
	NodeName          string                    `json:"node_name"`
	BackupFromStandby bool                      `json:"backup_from_standby"`
	Instances         []*InstanceHost           `json:"instances"`
	BackupOptions     *pgbackrest.BackupOptions `json:"backup_options"`
}

type CreatePgBackRestBackupOutput struct{}

func (w *Workflows) CreatePgBackRestBackup(ctx workflow.Context, input *CreatePgBackRestBackupInput) (*CreatePgBackRestBackupOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"task_id", input.TaskID.String(),
		"backup_from_standby", input.BackupFromStandby,
	)
	logger.Info("creating pgbackrest backup")

	var handleError = func(cause error) error {
		logger.With("error", cause).Error("failed to create pgbackrest backup")

		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID:    input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_ = w.updateTask(ctx, logger, updateTaskInput)

		return cause
	}

	instance, err := workflow.SideEffect(ctx, func(_ workflow.Context) *InstanceHost {
		idx := rand.IntN(len(input.Instances))
		return input.Instances[idx]
	}).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get random instance: %w", err))
	}

	updateOptions := task.UpdateStart()
	updateOptions.InstanceID = utils.PointerTo(instance.InstanceID)
	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: updateOptions,
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	getPrimaryInput := &activities.GetPrimaryInstanceInput{
		DatabaseID: input.DatabaseID,
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
		DatabaseID:    input.DatabaseID,
		InstanceID:    backupInstance.InstanceID,
		TaskID:        input.TaskID,
		BackupOptions: input.BackupOptions,
	}
	_, err = w.Activities.
		ExecuteCreatePgBackRestBackup(ctx, backupInstance.HostID, backupInput).
		Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to create pgbackrest backup: %w", err))
	}

	updateTaskInput = &activities.UpdateTaskInput{
		DatabaseID:    input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully created pgbackrest backup")

	return &CreatePgBackRestBackupOutput{}, nil
}
