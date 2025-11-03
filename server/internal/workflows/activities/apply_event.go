package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
)

type ApplyEventInput struct {
	DatabaseID string          `json:"database_id"`
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
		Queue: queue,
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ApplyEventOutput](ctx, options, a.ApplyEvent, input), nil
}

func (a *Activities) ApplyEvent(ctx context.Context, input *ApplyEventInput) (*ApplyEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID)
	logStart := logger.With(
		"event_type", input.Event.Type,
		"event_resource_type", input.Event.Resource.Identifier.Type,
		"event_resource_id", input.Event.Resource.Identifier.ID,
	)

	if input.Event.Type == resource.EventTypeRefresh {
		// Refresh messages are less helpful during normal operation
		logStart.Debug("applying resource event to state")
	} else {
		logStart.Info("applying resource event to state")
	}

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

	var needsCreate bool

	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan error, 1)

	go func() {
		defer close(resultCh)

		switch input.Event.Type {
		case resource.EventTypeRefresh:
			err := r.Refresh(ctxWithCancel, rc)
			if errors.Is(err, resource.ErrNotFound) {
				needsCreate = true
			} else if err != nil {
				resultCh <- fmt.Errorf("failed to refresh resource %s: %w", r.Identifier().String(), err)
			}
		case resource.EventTypeCreate:
			err := a.logEvent(ctxWithCancel, input.DatabaseID, input.TaskID, "creating", r, func() error {
				return r.Create(ctxWithCancel, rc)
			})
			if err != nil {
				resultCh <- fmt.Errorf("failed to create resource %s: %w", r.Identifier().String(), err)
			}
		case resource.EventTypeUpdate:
			err := a.logEvent(ctxWithCancel, input.DatabaseID, input.TaskID, "updating", r, func() error {
				return r.Update(ctxWithCancel, rc)
			})
			if err != nil {
				resultCh <- fmt.Errorf("failed to update resource %s: %w", r.Identifier().String(), err)
			}
		case resource.EventTypeDelete:
			err := a.logEvent(ctxWithCancel, input.DatabaseID, input.TaskID, "deleting", r, func() error {
				return r.Delete(ctxWithCancel, rc)
			})
			if err != nil {
				resultCh <- fmt.Errorf("failed to delete resource %s: %w", r.Identifier().String(), err)
			}
		default:
			resultCh <- fmt.Errorf("unknown event type: %s", input.Event.Type)
		}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("activity canceled: %w", ctx.Err())
	case err := <-resultCh:
		if err != nil {
			return nil, err
		}
	}
	data, err := resource.ToResourceData(r)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare resource for serialization: %w", err)
	}
	data.NeedsRecreate = needsCreate

	return &ApplyEventOutput{
		Event: &resource.Event{
			Type:     input.Event.Type,
			Resource: data,
		},
	}, nil
}

func (a *Activities) logEvent(
	ctx context.Context,
	databaseID string,
	taskID uuid.UUID,
	verb string,
	resource resource.Resource,
	apply func() error,
) error {
	resourceIdentifier := resource.Identifier()
	fields := map[string]any{
		"resource_type": resourceIdentifier.Type,
		"resource_id":   resourceIdentifier.ID,
		"host_id":       a.Config.HostID,
	}
	// Currying AddLogEntry
	log := func(entry task.LogEntry) error {
		return a.TaskSvc.AddLogEntry(ctx, databaseID, taskID, entry)
	}
	err := log(task.LogEntry{
		Message: fmt.Sprintf("%s resource %s", verb, resourceIdentifier),
		Fields:  fields,
	})
	if err != nil {
		return fmt.Errorf("failed to record event start: %w", err)
	}

	start := time.Now()
	applyErr := apply()
	duration := time.Since(start)

	fields["duration_ms"] = duration.Milliseconds()

	if applyErr != nil {
		fields["success"] = false
		fields["error"] = applyErr.Error()

		err := log(task.LogEntry{
			Message: fmt.Sprintf("error while %s resource %s", verb, resourceIdentifier),
			Fields:  fields,
		})
		if err != nil {
			return errors.Join(
				applyErr,
				fmt.Errorf("failed to record event error: %w", err),
			)
		}
		return applyErr
	}

	fields["success"] = true

	err = log(task.LogEntry{
		Message: fmt.Sprintf("finished %s resource %s (took %s)", verb, resourceIdentifier, duration),
		Fields:  fields,
	})
	if err != nil {
		return fmt.Errorf("failed to record event completion: %w", err)
	}

	return nil
}
