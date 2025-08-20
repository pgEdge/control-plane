package workflows

import (
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type StartInstanceInput struct {
	DatabaseID string
	InstanceID string
	HostID     string
	Cohort     *host.Cohort
	TaskID     uuid.UUID
}

type StartInstanceOutput struct{}

func (w *Workflows) StartInstance(ctx workflow.Context, input *StartInstanceInput) (*StartInstanceOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
		"task_id", input.TaskID.String(),
	)
	logger.Info("starting instance")

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failed to start instance")

		updateTaskInput := &activities.UpdateTaskInput{
			DatabaseID:    input.DatabaseID,
			TaskID:        input.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		if _, err := w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx); err != nil {
			logger.With("error", err).Error("failed to update task after instance start failure")
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

	req := activities.StartInstanceInput{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		HostID:     input.HostID,
		Cohort:     input.Cohort,
		TaskID:     input.TaskID,
	}
	_, err := w.Activities.ExecuteStartInstance(ctx, &req).Get(ctx)
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

	logger.Info("successfully requested a start instance")
	return &StartInstanceOutput{}, nil
}
