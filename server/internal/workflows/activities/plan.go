package activities

import (
	"context"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type PlanInput struct {
	DatabaseID  uuid.UUID       `json:"database_id"`
	Current     *resource.State `json:"current"`
	Desired     *resource.State `json:"desired"`
	ForceUpdate bool            `json:"force_update"`
}

type PlanOutput struct {
	Plan [][]*resource.Event `json:"plan"`
}

func (a *Activities) ExecutePlan(
	ctx workflow.Context,
	input *PlanInput,
) workflow.Future[*PlanOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PlanOutput](ctx, options, a.Plan, input)
}

func (a *Activities) Plan(ctx context.Context, input *PlanInput) (*PlanOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("generating update plan")

	// This needs to be in an activity because it's non-deterministic and can
	// produce an error
	plan, err := input.Current.Plan(input.Desired, input.ForceUpdate)
	if err != nil {
		return nil, err
	}

	return &PlanOutput{
		Plan: plan,
	}, nil
}
