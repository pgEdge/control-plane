package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type CreateReplicationSlotInput struct {
	Spec                 *database.Spec `json:"spec"`
	ProviderInstanceID   string         `json:"provider_instance_id"`
	SubscriberInstanceID string         `json:"subscriber_instance_id"`
}

type CreateReplicationSlotOutput struct{}

func (a *Activities) ExecuteCreateReplicationSlot(
	ctx workflow.Context,
	hostID string,
	input *CreateReplicationSlotInput,
) workflow.Future[*CreateReplicationSlotOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CreateReplicationSlotOutput](ctx, options, a.CreateReplicationSlot, input)
}

func (a *Activities) CreateReplicationSlot(
	ctx context.Context,
	input *CreateReplicationSlotInput,
) (*CreateReplicationSlotOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.Spec.DatabaseID,
		"provider_instance_id", input.ProviderInstanceID,
		"subscriber_instance_id", input.SubscriberInstanceID,
	)
	logger.Info("creating replication slot")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get database service: %w", err)
	}

	stmt, err := dbSvc.CreateReplicationSlot(ctx, input.Spec, input.ProviderInstanceID, input.SubscriberInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create replication slot: %w", err)
	}
	logger.Info("CreateReplicationSlot", "statement", stmt)
	logger.Info("replication slot created successfully")
	return &CreateReplicationSlotOutput{}, nil
}
