package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RefreshCurrentStateInput struct {
	DatabaseID string    `json:"database_id"`
	TaskID     uuid.UUID `json:"task_id"`
}

type RefreshCurrentStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteRefreshCurrentState(
	ctx workflow.Context,
	input *RefreshCurrentStateInput,
) workflow.Future[*RefreshCurrentStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: utils.HostQueue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*RefreshCurrentStateOutput](ctx, options, w.RefreshCurrentState, input)
}

func (w *Workflows) RefreshCurrentState(ctx workflow.Context, input *RefreshCurrentStateInput) (*RefreshCurrentStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Info("getting current database state")

	getCurrentInput := &activities.GetCurrentStateInput{
		DatabaseID: input.DatabaseID,
	}
	getCurrentOutput, err := w.Activities.
		ExecuteGetCurrentState(ctx, getCurrentInput).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current state: %w", err)
	}

	current := getCurrentOutput.State

	planRefreshInput := &activities.PlanRefreshInput{
		DatabaseID: input.DatabaseID,
		State:      current,
	}
	planRefreshOutput, err := w.Activities.
		ExecutePlanRefresh(ctx, planRefreshInput).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to plan refresh: %w", err)
	}

	start := workflow.Now(ctx)
	err = w.logTaskEvent(ctx,
		input.DatabaseID,
		input.TaskID,
		task.LogEntry{
			Message: "refreshing current state",
		},
	)
	if err != nil {
		return nil, err
	}
	if err := w.applyEvents(ctx, input.DatabaseID, input.TaskID, current, planRefreshOutput.Plan); err != nil {
		return nil, fmt.Errorf("failed to apply refresh events: %w", err)
	}
	duration := workflow.Now(ctx).Sub(start)
	err = w.logTaskEvent(ctx,
		input.DatabaseID,
		input.TaskID,
		task.LogEntry{
			Message: fmt.Sprintf("finished refreshing current state (took %s)", duration),
			Fields: map[string]any{
				"duration_ms": duration.Milliseconds(),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	logger.Info("successfully got current state")

	return &RefreshCurrentStateOutput{
		State: current,
	}, nil
}
