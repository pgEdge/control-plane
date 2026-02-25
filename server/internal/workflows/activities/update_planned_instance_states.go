package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type UpdatePlannedInstanceStatesInput struct {
	DatabaseID string        `json:"database_id"`
	Plan       resource.Plan `json:"plan"`
}

type UpdatePlannedInstanceStatesOutput struct{}

func (a *Activities) ExecuteUpdatePlannedInstanceStates(
	ctx workflow.Context,
	input *UpdatePlannedInstanceStatesInput,
) workflow.Future[*UpdatePlannedInstanceStatesOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.AnyQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*UpdatePlannedInstanceStatesOutput](ctx, options, a.UpdatePlannedInstanceStates, input)
}

func (a *Activities) UpdatePlannedInstanceStates(ctx context.Context, input *UpdatePlannedInstanceStatesInput) (*UpdatePlannedInstanceStatesOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
	)
	logger.Info("updating planned instance states")

	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for _, phase := range input.Plan {
		for _, event := range phase {
			if event.Resource.Identifier.Type != database.ResourceTypeInstance {
				continue
			}
			instance, err := resource.TypedFromRegistry[*database.InstanceResource](registry, event.Resource)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize instance resource: %w", err)
			}
			update := &database.InstanceStateUpdateOptions{
				InstanceID: instance.Spec.InstanceID,
				DatabaseID: instance.Spec.DatabaseID,
				HostID:     instance.Spec.HostID,
				NodeName:   instance.Spec.NodeName,
				Now:        now,
			}
			switch event.Type {
			case resource.EventTypeCreate:
				update.State = database.InstanceStateCreating
			case resource.EventTypeDelete:
				update.State = database.InstanceStateDeleting
			case resource.EventTypeUpdate:
				update.State = database.InstanceStateModifying
			default:
				// Other event types don't require an update
				continue
			}
			if err := a.DatabaseService.UpdateInstanceState(ctx, update); err != nil {
				return nil, fmt.Errorf("failed to update database instance '%s': %w", instance.Spec.InstanceID, err)
			}
		}
	}

	return &UpdatePlannedInstanceStatesOutput{}, nil
}
