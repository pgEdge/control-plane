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

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type ApplyEventInput struct {
	DatabaseID uuid.UUID       `json:"database_id"`
	State      *resource.State `json:"state"`
	Event      *resource.Event `json:"event"`
}

type ApplyEventOutput struct {
	Event *resource.Event `json:"event"`
}

func (a *Activities) ExecuteApplyEvent(
	ctx workflow.Context,
	input *ApplyEventInput,
) (workflow.Future[*ApplyEventOutput], error) {
	queue, err := a.ResolveExecutor(input.State, input.Event.Resource.Executor)
	if err != nil {
		identifier := input.Event.Resource.Identifier
		return nil, fmt.Errorf("failed to resolve executor for %s resource %s: %w", identifier.Type, identifier.ID, err)
	}
	options := workflow.ActivityOptions{
		Queue: core.Queue(queue),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ApplyEventOutput](ctx, options, a.ApplyEvent, input), nil
}

func (a *Activities) ApplyEvent(ctx context.Context, input *ApplyEventInput) (*ApplyEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID.String())
	logger.With(
		"event_type", input.Event.Type,
		"event_resource_type", input.Event.Resource.Identifier.Type,
		"event_resource_id", input.Event.Resource.Identifier.ID,
	).Info("applying resource event to state")

	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return nil, err
	}

	r, err := registry.Resource(input.Event.Resource)
	if err != nil {
		return nil, err
	}

	rc := &resource.Context{
		State:    input.State,
		Injector: a.Injector,
		Registry: registry,
	}

	outputEventType := input.Event.Type

	switch input.Event.Type {
	case resource.EventTypeRefresh:
		err := r.Refresh(ctx, rc)
		if errors.Is(err, resource.ErrNotFound) {
			// The resource was deleted in the meantime, so we'll remove it from
			// the state.
			outputEventType = resource.EventTypeDelete
		} else if err != nil {
			return nil, fmt.Errorf("failed to refresh resource %s: %w", r.Identifier().String(), err)
		}
	case resource.EventTypeCreate:
		if err := r.Create(ctx, rc); err != nil {
			return nil, fmt.Errorf("failed to create resource %s: %w", r.Identifier().String(), err)
		}
	case resource.EventTypeUpdate:
		if err := r.Update(ctx, rc); err != nil {
			return nil, fmt.Errorf("failed to update resource %s: %w", r.Identifier().String(), err)
		}
	case resource.EventTypeDelete:
		if err := r.Delete(ctx, rc); err != nil {
			return nil, fmt.Errorf("failed to delete resource %s: %w", r.Identifier().String(), err)
		}
	default:
		return nil, fmt.Errorf("unknown event type: %s", input.Event.Type)
	}

	data, err := resource.ToResourceData(r)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare resource for serialization: %w", err)
	}

	return &ApplyEventOutput{
		Event: &resource.Event{
			Type:     outputEventType,
			Resource: data,
		},
	}, nil
}
