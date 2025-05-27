package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
)

type LogTaskEventInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
	TaskID     uuid.UUID `json:"task_id"`
	Messages   []string  `json:"messages"`
}

type LogTaskEventOutput struct{}

func (a *Activities) ExecuteLogTaskEvent(
	ctx workflow.Context,
	input *LogTaskEventInput,
) workflow.Future[*LogTaskEventOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*LogTaskEventOutput](ctx, options, a.LogTaskEvent, input)
}

func (a *Activities) LogTaskEvent(ctx context.Context, input *LogTaskEventInput) (*LogTaskEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("updating database state")

	for _, msg := range input.Messages {
		err := a.TaskSvc.AddLogLine(ctx, input.DatabaseID, input.TaskID, msg)
		if err != nil {
			return nil, fmt.Errorf("failed to add task log line: %w", err)
		}
	}

	return &LogTaskEventOutput{}, nil
}
