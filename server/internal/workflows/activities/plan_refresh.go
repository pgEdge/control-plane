package activities

import (
	"context"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type PlanRefreshInput struct {
	DatabaseID uuid.UUID       `json:"database_id"`
	State      *resource.State `json:"current"`
}

type PlanRefreshOutput struct {
	Events []*resource.Event `json:"events"`
}

func (a *Activities) ExecutePlanRefresh(
	ctx workflow.Context,
	input *PlanRefreshInput,
) workflow.Future[*PlanRefreshOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PlanRefreshOutput](ctx, options, a.PlanRefresh, input)
}

func (a *Activities) PlanRefresh(ctx context.Context, input *PlanRefreshInput) (*PlanRefreshOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("generating refresh plan")

	// This needs to be in an activity because it's non-deterministic and can
	// produce an error
	plan, err := input.State.PlanRefresh()
	if err != nil {
		return nil, err
	}

	return &PlanRefreshOutput{
		Events: plan,
	}, nil
}
