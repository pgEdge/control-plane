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

type CreateDisabledSubscriptionInput struct {
	TaskID               uuid.UUID      `json:"task_id"`
	Spec                 *database.Spec `json:"spec"`
	SubscriberInstanceID string         `json:"subscriber_instance_id"`
	ProviderInstanceID   string         `json:"provider_instance_id"`
}

type CreateDisabledSubscriptionOutput struct{}

func (a *Activities) ExecuteCreateDisabledSubscription(
	ctx workflow.Context,
	hostID string,
	input *CreateDisabledSubscriptionInput,
) workflow.Future[*CreateDisabledSubscriptionOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CreateDisabledSubscriptionOutput](ctx, options, a.CreateDisabledSubscription, input)
}

func (a *Activities) CreateDisabledSubscription(
	ctx context.Context,
	input *CreateDisabledSubscriptionInput,
) (*CreateDisabledSubscriptionOutput, error) {
	logger := activity.Logger(ctx)

	if input == nil {
		return nil, errors.New("input is nil")
	}

	logger = logger.With(
		"task_id", input.TaskID,
		"database_id", input.Spec.DatabaseID,
		"subscriber_instance_id", input.SubscriberInstanceID,
		"provider_instance_id", input.ProviderInstanceID,
	)
	logger.Info("creating disabled subscription")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get database service: %w", err)
	}

	stmt, err := dbSvc.CreateDisabledSubscription(
		ctx,
		input.Spec,
		input.SubscriberInstanceID,
		input.ProviderInstanceID,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create disabled subscription: %w", err)
	}
	logger.Info("CreateDisabledSubscription", "statement", stmt)
	logger.Info("disabled subscription created successfully")
	return &CreateDisabledSubscriptionOutput{}, nil
}
