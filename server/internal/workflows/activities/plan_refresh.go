package activities

import (
	"context"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type PlanRefreshInput struct {
	DatabaseID string          `json:"database_id"`
	State      *resource.State `json:"current"`
}

type PlanRefreshOutput struct {
	Plan [][]*resource.Event `json:"plan"`
}

func (a *Activities) ExecutePlanRefresh(
	ctx workflow.Context,
	input *PlanRefreshInput,
) workflow.Future[*PlanRefreshOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PlanRefreshOutput](ctx, options, a.PlanRefresh, input)
}

func (a *Activities) PlanRefresh(ctx context.Context, input *PlanRefreshInput) (*PlanRefreshOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Info("generating refresh plan")

	// This needs to be in an activity because it's non-deterministic and can
	// produce an error
	plan, err := input.State.PlanRefresh()
	if err != nil {
		return nil, err
	}

	return &PlanRefreshOutput{
		Plan: plan,
	}, nil
}
