package workflows

import (
	"errors"
	"time"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RestartInstanceInput struct {
	DatabaseID  string
	InstanceID  string
	TaskID      uuid.UUID
	ScheduledAt time.Time // Optional, if empty, restart immediately
}

type RestartInstanceOutput struct{}

func (w *Workflows) RestartInstance(ctx workflow.Context, input *RestartInstanceInput) (*RestartInstanceOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
		"task_id", input.TaskID.String(),
	)
	logger.Info("restarting instance")

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			updateStateInput := &activities.UpdateDbStateInput{
				DatabaseID: input.DatabaseID,
				State:      database.DatabaseStateFailed,
			}

			_, stateErr := w.Activities.ExecuteUpdateDbState(cleanupCtx, updateStateInput).Get(cleanupCtx)
			if stateErr != nil {
				logger.With("error", stateErr).Error("failed to update database state")
			}
			w.cancelTask(cleanupCtx, input.DatabaseID, input.TaskID, logger)
		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to restart instance")

		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID:    input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
			logger.With("error", err).Error("failed to update task after instance restart failure")
		}

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
		return nil, handleError(err)
	}
	req := activities.RestartInstanceInput{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		TaskID:     input.TaskID,
	}
	_, err := w.Activities.ExecuteRestartInstance(ctx, &req).Get(ctx)
	if err != nil {
		return nil, handleError(err)
	}

	updateTaskInput = &activities.UpdateTaskInput{
		DatabaseID:    input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("successfully requested a restart")
	return &RestartInstanceOutput{}, nil
}
