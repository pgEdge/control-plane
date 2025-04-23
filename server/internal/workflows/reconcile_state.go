package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ReconcileStateInput struct {
	DatabaseID  uuid.UUID       `json:"database_id"`
	Current     *resource.State `json:"current"`
	Desired     *resource.State `json:"desired"`
	ForceUpdate bool            `json:"force_update"`
}

type ReconcileStateOutput struct {
	Updated *resource.State `json:"current"`
}

func (w *Workflows) ExecuteReconcileState(
	ctx workflow.Context,
	input *ReconcileStateInput,
) workflow.Future[*ReconcileStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*ReconcileStateOutput](ctx, options, w.ReconcileState, input)
}

func (w *Workflows) ReconcileState(ctx workflow.Context, input *ReconcileStateInput) (*ReconcileStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("reconciling database state")

	planInput := &PlanInput{
		DatabaseID:  input.DatabaseID,
		Current:     input.Current,
		Desired:     input.Desired,
		ForceUpdate: input.ForceUpdate,
	}
	planOutput, err := w.ExecutePlan(ctx, planInput).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	current := planOutput.Current

	// We always want to persist the updated state
	defer func() {
		in := &activities.PersistStateInput{
			DatabaseID: input.DatabaseID,
			State:      current,
		}
		_, err := w.Activities.ExecutePersistState(ctx, in).Get(ctx)
		if err != nil {
			logger.Error("failed to persist state", "error", err)
		}
	}()

	if err := w.applyEvents(ctx, input.DatabaseID, current, planOutput.Plan); err != nil {
		return nil, fmt.Errorf("failed to apply events: %w", err)
	}

	logger.Info("successfully reconciled database state")

	return &ReconcileStateOutput{
		Updated: current,
	}, nil
}
