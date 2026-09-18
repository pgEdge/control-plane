package workflows

import (
	"errors"
	"time"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RestartInstanceInput struct {
	HostID      string    `json:"host_id"`
	DatabaseID  string    `json:"database_id"`
	InstanceID  string    `json:"instance_id"`
	TaskID      uuid.UUID `json:"task_id"`
	ScheduledAt time.Time `json:"scheduled_at"` // Optional, if empty, restart immediately
}

type RestartInstanceOutput struct{}

func (w *Workflows) RestartInstance(ctx workflow.Context, input *RestartInstanceInput) (*RestartInstanceOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
		"task_id", input.TaskID.String(),
		"scheduled", input.ScheduledAt,
	)
	logger.Info("restarting instance")

	var restartScheduled bool

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow was canceled")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)

			if restartScheduled {
				cancelIn := &activities.CancelRestartInput{
					DatabaseID: input.DatabaseID,
					InstanceID: input.InstanceID,
					TaskID:     input.TaskID,
				}
				if _, err := w.Activities.ExecuteCancelRestart(cleanupCtx, input.HostID, cancelIn).Get(cleanupCtx); err != nil {
					logger.Warn("cancel restart activity failed", "err", err)
				} else {
					logger.Info("cancel restart activity dispatched")
				}
			}

			w.cancelTask(cleanupCtx, task.ScopeDatabase, input.DatabaseID, input.TaskID, logger)
		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to restart instance")

		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
			logger.With("error", err).Error("failed to update task after instance restart failure")
		}

		return cause
	}

	updateTaskInput := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
		if errors.Is(err, workflow.Canceled) {
			return nil, err
		}
		return nil, handleError(err)
	}
	req := activities.RestartInstanceInput{
		DatabaseID:  input.DatabaseID,
		InstanceID:  input.InstanceID,
		TaskID:      input.TaskID,
		ScheduledAt: input.ScheduledAt,
	}
	restartOut, err := w.Activities.ExecuteRestartInstance(ctx, input.HostID, &req).Get(ctx)
	if err != nil {
		if errors.Is(err, workflow.Canceled) {
			return nil, err
		}
		return nil, handleError(err)
	}
	restartScheduled = !input.ScheduledAt.IsZero()

	if !input.ScheduledAt.IsZero() {
		if d := input.ScheduledAt.Sub(workflow.Now(ctx)); d > 0 {
			logger.Info("sleeping until scheduled restart time", "duration", d.String())
			if err := workflow.Sleep(ctx, d); err != nil {
				if errors.Is(err, workflow.Canceled) {
					return nil, err
				}
				return nil, handleError(err)
			}
		}
	}

	waitIn := &activities.WaitForRestartCompleteInput{
		DatabaseID:                  input.DatabaseID,
		InstanceID:                  input.InstanceID,
		TaskID:                      input.TaskID,
		BaselinePostmasterStartTime: restartOut.PostmasterStartTime,
	}
	if _, err := w.Activities.ExecuteWaitForRestartComplete(ctx, input.HostID, waitIn).Get(ctx); err != nil {
		if errors.Is(err, workflow.Canceled) {
			return nil, err
		}
		return nil, handleError(err)
	}

	updateTaskInput = &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      input.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		if errors.Is(err, workflow.Canceled) {
			return nil, err
		}
		return nil, handleError(err)
	}

	logger.Info("instance restart completed")
	return &RestartInstanceOutput{}, nil
}
