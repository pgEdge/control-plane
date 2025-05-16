package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/task"
)

type UpdateTaskStatusInput struct {
	DatabaseID uuid.UUID   `json:"database_id"`
	TaskID     uuid.UUID   `json:"task_id"`
	Status     task.Status `json:"status"`
}

type UpdateTaskStatusOutput struct{}

func (a *Activities) ExecuteUpdateTaskStatus(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *UpdateTaskStatusInput,
) workflow.Future[*UpdateTaskStatusOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateTaskStatusOutput](ctx, options, a.UpdateTaskStatus, input)
}

func (a *Activities) UpdateTaskStatus(ctx context.Context, input *UpdateTaskStatusInput) (*UpdateTaskStatusOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID.String(),
		"task_id", input.TaskID.String(),
		"task_status", input.Status.String(),
	)
	logger.Info("updating task status")

	service, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	t, err := service.GetTask(ctx, input.DatabaseID, input.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	t.Status = input.Status

	err = service.UpdateTask(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return &UpdateTaskStatusOutput{}, nil
}
