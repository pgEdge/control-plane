package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

type ApplyEventInput struct {
	DatabaseID uuid.UUID       `json:"database_id"`
	TaskID     uuid.UUID       `json:"task_id"`
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
		err := a.logEvent(ctx, input.DatabaseID, input.TaskID, "creating", r, func() error {
			return r.Create(ctx, rc)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create resource %s: %w", r.Identifier().String(), err)
		}
	case resource.EventTypeUpdate:
		err := a.logEvent(ctx, input.DatabaseID, input.TaskID, "updating", r, func() error {
			return r.Update(ctx, rc)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update resource %s: %w", r.Identifier().String(), err)
		}
	case resource.EventTypeDelete:
		err := a.logEvent(ctx, input.DatabaseID, input.TaskID, "deleting", r, func() error {
			return r.Delete(ctx, rc)
		})
		if err != nil {
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

func (a *Activities) logEvent(
	ctx context.Context,
	databaseID uuid.UUID,
	taskID uuid.UUID,
	verb string,
	resource resource.Resource,
	apply func() error,
) error {
	// Currying AddLogLine
	log := func(msg string) error {
		return a.TaskSvc.AddLogLine(ctx, databaseID, taskID, msg)
	}

	msg := fmt.Sprintf("%s %s on host %s", verb, resource.Identifier(), a.Config.HostID)
	err := log(msg)
	if err != nil {
		return fmt.Errorf("failed to record event start: %w", err)
	}

	start := time.Now()
	applyErr := apply()
	duration := time.Since(start)

	if applyErr != nil {
		msg := fmt.Sprintf("error while %s %s: %s", verb, resource.Identifier(), applyErr)
		err := log(msg)
		if err != nil {
			return errors.Join(
				applyErr,
				fmt.Errorf("failed to record event error: %w", err),
			)
		}
		return applyErr
	}

	msg = fmt.Sprintf("finished %s %s (took %s)", verb, resource.Identifier(), duration)
	err = log(msg)
	if err != nil {
		return fmt.Errorf("failed to record event completion: %w", err)
	}

	return nil
}
