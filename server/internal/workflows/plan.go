package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type PlanInput struct {
	DatabaseID  uuid.UUID       `json:"database_id"`
	Current     *resource.State `json:"current"`
	Desired     *resource.State `json:"desired"`
	ForceUpdate bool            `json:"force_update"`
}

type PlanOutput struct {
	Current *resource.State   `json:"current"`
	Desired *resource.State   `json:"desired"`
	Events  []*resource.Event `json:"events"`
}

func (w *Workflows) ExecutePlan(
	ctx workflow.Context,
	input *PlanInput,
) workflow.Future[*PlanOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID.String()),
	}
	return workflow.CreateSubWorkflowInstance[*PlanOutput](ctx, options, w.Plan, input)
}

func (w *Workflows) Plan(ctx workflow.Context, input *PlanInput) (*PlanOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("planning database update")

	current := input.Current
	desired := input.Desired

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

	if err := w.applyEvents(ctx, input.DatabaseID, current, planRefreshOutput.Events); err != nil {
		return nil, fmt.Errorf("failed to apply refresh events: %w", err)
	}

	planOutput, err := w.Activities.ExecutePlan(ctx, &activities.PlanInput{
		DatabaseID:  input.DatabaseID,
		Current:     current,
		Desired:     desired,
		ForceUpdate: input.ForceUpdate,
	}).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	logger.Info("successfully generated plan")

	return &PlanOutput{
		Current: current,
		Desired: desired,
		Events:  planOutput.Events,
	}, nil
}
