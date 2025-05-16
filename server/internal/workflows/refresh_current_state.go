package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type RefreshCurrentStateInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
}

type RefreshCurrentStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteRefreshCurrentState(
	ctx workflow.Context,
	input *RefreshCurrentStateInput,
) workflow.Future[*RefreshCurrentStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*RefreshCurrentStateOutput](ctx, options, w.RefreshCurrentState, input)
}

func (w *Workflows) RefreshCurrentState(ctx workflow.Context, input *RefreshCurrentStateInput) (*RefreshCurrentStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID.String())
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

	if err := w.applyEvents(ctx, input.DatabaseID, current, planRefreshOutput.Plan); err != nil {
		return nil, fmt.Errorf("failed to apply refresh events: %w", err)
	}

	logger.Info("successfully got current state")

	return &RefreshCurrentStateOutput{
		State: current,
	}, nil
}
