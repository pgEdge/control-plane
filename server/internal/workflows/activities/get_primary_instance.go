package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetPrimaryInstanceInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
}

type GetPrimaryInstanceOutput struct {
	PrimaryInstanceID string `json:"instance_id"`
}

func (a *Activities) ExecuteGetPrimaryInstance(
	ctx workflow.Context,
	hostID string,
	input *GetPrimaryInstanceInput,
) workflow.Future[*GetPrimaryInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetPrimaryInstanceOutput](ctx, options, a.GetPrimaryInstance, input)
}

func (a *Activities) GetPrimaryInstance(ctx context.Context, input *GetPrimaryInstanceInput) (*GetPrimaryInstanceOutput, error) {
	logger := activity.Logger(ctx).With("instance_id", input.InstanceID)
	logger.Info("determining primary instance")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	primaryInstanceID, err := database.GetPrimaryInstanceID(ctx, patroniClient, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to get primary instance ID: %w", err)
	}

	return &GetPrimaryInstanceOutput{
		PrimaryInstanceID: primaryInstanceID,
	}, nil
}
