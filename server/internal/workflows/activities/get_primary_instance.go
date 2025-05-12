package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
)

type GetPrimaryInstanceInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
	InstanceID uuid.UUID `json:"instance_id"`
}

type GetPrimaryInstanceOutput struct {
	PrimaryInstanceID uuid.UUID `json:"instance_id"`
}

func (a *Activities) ExecuteGetPrimaryInstance(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *GetPrimaryInstanceInput,
) workflow.Future[*GetPrimaryInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetPrimaryInstanceOutput](ctx, options, a.GetPrimaryInstance, input)
}

func (a *Activities) GetPrimaryInstance(ctx context.Context, input *GetPrimaryInstanceInput) (*GetPrimaryInstanceOutput, error) {
	logger := activity.Logger(ctx).With("instance_id", input.InstanceID.String())
	logger.Info("determining primary instance")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	primaryInstanceID, err := database.GetPrimaryInstanceID(ctx, orch, input.DatabaseID, input.InstanceID, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to get primary instance ID: %w", err)
	}

	return &GetPrimaryInstanceOutput{
		PrimaryInstanceID: primaryInstanceID,
	}, nil
}
