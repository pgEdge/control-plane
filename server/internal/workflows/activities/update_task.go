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

type UpdateTaskInput struct {
	Task *task.Task `json:"task"`
}

type UpdateTaskOutput struct{}

func (a *Activities) ExecuteUpdateTask(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *UpdateTaskInput,
) workflow.Future[*UpdateTaskOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateTaskOutput](ctx, options, a.UpdateTask, input)
}

func (a *Activities) UpdateTask(ctx context.Context, input *UpdateTaskInput) (*UpdateTaskOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.Task.DatabaseID.String(),
		"task_id", input.Task.TaskID.String(),
		"task_type", input.Task.Type.String(),
		"task_status", input.Task.Status.String(),
	)
	logger.Info("updating task")

	service, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	err = service.UpdateTask(ctx, input.Task)
	if err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return &UpdateTaskOutput{}, nil
}
