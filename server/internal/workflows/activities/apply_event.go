package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
)

type ApplyEventInput struct {
	DatabaseID  string          `json:"database_id"`
	TaskID      uuid.UUID       `json:"task_id"`
	State       *resource.State `json:"state"`
	Event       *resource.Event `json:"event"`
	RemoveHosts []string        `json:"remove_hosts"`
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
	for _, id := range input.RemoveHosts {
		if queue == utils.HostQueue(id) {
			return nil, ErrHostRemoved
		}
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

	rc := &resource.Context{
		State:    input.State,
		Injector: a.Injector,
		Registry: registry,
	}

	event := input.Event
	apply := func() error {
		return event.Apply(ctx, rc)
	}

	switch input.Event.Type {
	case resource.EventTypeCreate:
		apply = func() error {
			return a.logResourceEvent(ctx, input.DatabaseID, input.TaskID, "creating", event, rc)
		}
	case resource.EventTypeUpdate:
		apply = func() error {
			return a.logResourceEvent(ctx, input.DatabaseID, input.TaskID, "updating", event, rc)
		}
	case resource.EventTypeDelete:
		apply = func() error {
			return a.logResourceEvent(ctx, input.DatabaseID, input.TaskID, "deleting", event, rc)
		}
	}

	if err := apply(); err != nil {
		return nil, err
	}

	return &ApplyEventOutput{
		Event: event,
	}, nil
}

func (a *Activities) logResourceEvent(
	ctx context.Context,
	databaseID string,
	taskID uuid.UUID,
	verb string,
	event *resource.Event,
	rc *resource.Context,
) error {
	resourceIdentifier := event.Resource.Identifier
	fields := map[string]any{
		"resource_type": resourceIdentifier.Type,
		"resource_id":   resourceIdentifier.ID,
		"host_id":       a.Config.HostID,
	}
	// Currying AddLogEntry
	log := func(entry task.LogEntry) error {
		return a.TaskSvc.AddLogEntry(ctx, task.ScopeDatabase, databaseID, taskID, entry)
	}
	err := log(task.LogEntry{
		Message: fmt.Sprintf("%s resource %s", verb, resourceIdentifier),
		Fields:  fields,
	})
	if err != nil {
		return fmt.Errorf("failed to record event start: %w", err)
	}

	start := time.Now()
	applyErr := event.Apply(ctx, rc)
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
