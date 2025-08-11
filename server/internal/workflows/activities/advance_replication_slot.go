package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type AdvanceReplicationSlotInput struct {
	TaskID               uuid.UUID      `json:"task_id"`
	Spec                 *database.Spec `json:"spec"`
	ProviderInstanceID   string         `json:"provider_instance_id"`
	SubscriberInstanceID string         `json:"subscriber_instance_id"`
	LSN                  string         `json:"lsn"`
}

type AdvanceReplicationSlotOutput struct{}

func (a *Activities) ExecuteAdvanceReplicationSlot(
	ctx workflow.Context,
	hostID string,
	input *AdvanceReplicationSlotInput,
) workflow.Future[*AdvanceReplicationSlotOutput] {
	options := workflow.ActivityOptions{
		Queue:        core.Queue(hostID),
		RetryOptions: workflow.RetryOptions{MaxAttempts: 1},
	}
	return workflow.ExecuteActivity[*AdvanceReplicationSlotOutput](ctx, options, a.AdvanceReplicationSlot, input)
}

func (a *Activities) AdvanceReplicationSlot(
	ctx context.Context,
	input *AdvanceReplicationSlotInput,
) (*AdvanceReplicationSlotOutput, error) {
	logger := activity.Logger(ctx)

	if input == nil {
		return nil, errors.New("input is nil")
	}

	logger = logger.With(
		"task_id", input.TaskID,
		"database_id", input.Spec.DatabaseID,
		"provider_instance_id", input.ProviderInstanceID,
		"subscriber_instance_id", input.SubscriberInstanceID,
	)
	logger.Info("advancing replication slot")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get database service: %w", err)
	}

	stmt, err := dbSvc.AdvanceReplicationSlot(
		ctx,
		input.Spec,
		input.ProviderInstanceID,
		input.SubscriberInstanceID,
		input.LSN,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to advance replication slot: %w", err)
	}
	logger.Info("AdvancedReplicationSlot", "statement", stmt)
	logger.Info("replication slot advanced successfully")
	return &AdvanceReplicationSlotOutput{}, nil
}
