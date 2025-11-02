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

type UpdateDbStateInput struct {
	DatabaseID string                 `json:"database_id"`
	State      database.DatabaseState `json:"state"`
}

type UpdateDbStateOutput struct{}

func (a *Activities) ExecuteUpdateDbState(
	ctx workflow.Context,
	input *UpdateDbStateInput,
) workflow.Future[*UpdateDbStateOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateDbStateOutput](ctx, options, a.UpdateDbState, input)
}

func (a *Activities) UpdateDbState(ctx context.Context, input *UpdateDbStateInput) (*UpdateDbStateOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logger.Debug("updating database state")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = dbSvc.UpdateDatabaseState(ctx, input.DatabaseID, "", input.State)
	if err != nil {
		return nil, fmt.Errorf("failed to update database state: %w", err)
	}

	return &UpdateDbStateOutput{}, nil
}
