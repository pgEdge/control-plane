package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type UpdateDbStateInput struct {
	DatabaseID uuid.UUID              `json:"database_id"`
	State      database.DatabaseState `json:"state"`
}

type UpdateDbStateOutput struct{}

func (a *Activities) ExecuteUpdateDbState(
	ctx workflow.Context,
	input *UpdateDbStateInput,
) workflow.Future[*UpdateDbStateOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdateDbStateOutput](ctx, options, a.UpdateDbState, input)
}

func (a *Activities) UpdateDbState(ctx context.Context, input *UpdateDbStateInput) (*UpdateDbStateOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("updating database state")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = dbSvc.UpdateDatabaseState(ctx, input.DatabaseID, input.State)
	if err != nil {
		return nil, fmt.Errorf("failed to update database state: %w", err)
	}

	return &UpdateDbStateOutput{}, nil
}
