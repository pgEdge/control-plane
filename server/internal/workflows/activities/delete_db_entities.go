package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
)

type DeleteDbEntitiesInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
}

type DeleteDbEntitiesOutput struct{}

func (a *Activities) ExecuteDeleteDbEntities(
	ctx workflow.Context,
	input *DeleteDbEntitiesInput,
) workflow.Future[*DeleteDbEntitiesOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*DeleteDbEntitiesOutput](ctx, options, a.DeleteDbEntities, input)
}

func (a *Activities) DeleteDbEntities(ctx context.Context, input *DeleteDbEntitiesInput) (*DeleteDbEntitiesOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.Info("deleting database entities")

	resourceSvc, err := do.Invoke[*resource.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	taskSvc, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = resourceSvc.DeleteState(ctx, input.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete state: %w", err)
	}
	err = dbSvc.DeleteDatabase(ctx, input.DatabaseID)
	if err != nil && !errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, fmt.Errorf("failed to delete database: %w", err)
	}
	err = taskSvc.DeleteAllTasks(ctx, input.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete tasks: %w", err)
	}

	return &DeleteDbEntitiesOutput{}, nil
}
