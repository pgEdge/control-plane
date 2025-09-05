package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type UpdateTaskInput struct {
	DatabaseID    string             `json:"database_id"`
	TaskID        uuid.UUID          `json:"task_id"`
	UpdateOptions task.UpdateOptions `json:"update_options,omitempty"`
}

type UpdateTaskOutput struct{}

func (a *Activities) ExecuteUpdateTask(
	ctx workflow.Context,
	input *UpdateTaskInput,
) workflow.Future[*UpdateTaskOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateTaskOutput](ctx, options, a.UpdateTask, input)
}

func (a *Activities) UpdateTask(ctx context.Context, input *UpdateTaskInput) (*UpdateTaskOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"task_id", input.TaskID.String(),
	)
	logger.Info("updating task")

	service, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	t, err := service.GetTask(ctx, input.DatabaseID, input.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	t.Update(input.UpdateOptions)

	if err := service.UpdateTask(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return &UpdateTaskOutput{}, nil
}
