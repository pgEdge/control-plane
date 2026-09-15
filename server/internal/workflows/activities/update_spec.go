package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// UpdateSpecInput persists a full spec replacement via
// database.Service.ApplyMajorUpgradeSpecChange — the one privileged path
// that's allowed to change an instance's Spock major version. Used
// exclusively by the MajorVersionUpgrade workflow.
type UpdateSpecInput struct {
	Spec  *database.Spec         `json:"spec"`
	State database.DatabaseState `json:"state"`
}

type UpdateSpecOutput struct{}

func (a *Activities) ExecuteUpdateSpec(
	ctx workflow.Context,
	input *UpdateSpecInput,
) workflow.Future[*UpdateSpecOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateSpecOutput](ctx, options, a.UpdateSpec, input)
}

func (a *Activities) UpdateSpec(ctx context.Context, input *UpdateSpecInput) (*UpdateSpecOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Debug("persisting spec change for major-version upgrade")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	if _, err := dbSvc.ApplyMajorUpgradeSpecChange(ctx, input.Spec, input.State); err != nil {
		return nil, fmt.Errorf("failed to persist spec change: %w", err)
	}

	return &UpdateSpecOutput{}, nil
}
