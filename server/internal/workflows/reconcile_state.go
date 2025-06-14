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
	DatabaseID  string          `json:"database_id"`
	TaskID      uuid.UUID       `json:"task_id"`
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
		Queue: core.Queue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*ReconcileStateOutput](ctx, options, w.ReconcileState, input)
}

func (w *Workflows) ReconcileState(ctx workflow.Context, input *ReconcileStateInput) (*ReconcileStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Info("reconciling database state")

	planInput := &activities.PlanInput{
		DatabaseID:  input.DatabaseID,
		Current:     input.Current,
		Desired:     input.Desired,
		ForceUpdate: input.ForceUpdate,
	}
	planOutput, err := w.Activities.ExecutePlan(ctx, planInput).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	current := input.Current

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

	if len(planOutput.Plan) > 0 {
		if err := w.applyEvents(ctx, input.DatabaseID, input.TaskID, current, planOutput.Plan); err != nil {
			return nil, fmt.Errorf("failed to apply events: %w", err)
		}

		logger.Info("successfully reconciled database state")
	} else {
		logger.Info("no changes to apply")
	}

	return &ReconcileStateOutput{
		Updated: current,
	}, nil
}
