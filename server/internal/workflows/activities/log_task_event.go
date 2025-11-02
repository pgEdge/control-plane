package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type LogTaskEventInput struct {
	DatabaseID string          `json:"database_id"`
	TaskID     uuid.UUID       `json:"task_id"`
	Entries    []task.LogEntry `json:"messages"`
}

type LogTaskEventOutput struct{}

func (a *Activities) ExecuteLogTaskEvent(
	ctx workflow.Context,
	input *LogTaskEventInput,
) workflow.Future[*LogTaskEventOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*LogTaskEventOutput](ctx, options, a.LogTaskEvent, input)
}

func (a *Activities) LogTaskEvent(ctx context.Context, input *LogTaskEventInput) (*LogTaskEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Debug("logging task event")

	for _, entry := range input.Entries {
		err := a.TaskSvc.AddLogEntry(ctx, input.DatabaseID, input.TaskID, entry)
		if err != nil {
			return nil, fmt.Errorf("failed to add task log entry: %w", err)
		}
	}

	return &LogTaskEventOutput{}, nil
}
