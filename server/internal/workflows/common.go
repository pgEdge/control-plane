package workflows

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func (w *Workflows) applyEvents(ctx workflow.Context, databaseID, taskID uuid.UUID, state *resource.State, phases [][]*resource.Event) error {
	for _, phase := range phases {
		futures := make([]workflow.Future[*activities.ApplyEventOutput], len(phase))
		for i, event := range phase {
			in := &activities.ApplyEventInput{
				DatabaseID: databaseID,
				TaskID:     taskID,
				State:      state,
				Event:      event,
			}
			future, err := w.Activities.ExecuteApplyEvent(ctx, in)
			if errors.Is(err, activities.ErrExecutorNotFound) {
				// The executor is missing from the state, which can happen if a
				// resource was removed outside of control-plane and we've
				// updated our state to reflect that. We'll remove this resource
				// so that it can be recreated.
				// TODO: validate that this is always the right choice.
				state.Remove(event.Resource)
				continue
			} else if err != nil {
				return fmt.Errorf("failed to queue apply event: %w", err)
			}
			futures[i] = future
		}
		var errs []error
		for i, future := range futures {
			if future == nil {
				// This future was nil because the executor was not found. We
				// still add the nil futures to the slice so that we can match
				// the index to the original event.
				continue
			}
			out, err := future.Get(ctx)
			if err != nil {
				event := phase[i]
				errs = append(errs, fmt.Errorf("failed to apply %s event to %s: %w", event.Type, event.Resource.Identifier, err))
				continue
			}
			if err := state.Apply(out.Event); err != nil {
				event := phase[i]
				errs = append(errs, fmt.Errorf("failed to apply %s event from %s to state: %w", event.Type, event.Resource.Identifier, err))
			}
		}
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("failed while modifying resources: %w", err)
		}
	}

	return nil
}

func (w *Workflows) updateTask(
	ctx workflow.Context,
	logger *slog.Logger,
	input *activities.UpdateTaskInput,
) error {
	_, err := w.Activities.
		ExecuteUpdateTask(ctx, input).
		Get(ctx)
	if err != nil {
		logger.With("error", err).Error("failed to update task state")
		return fmt.Errorf("failed to update task state: %w", err)
	}
	return nil
}

func (w *Workflows) logTaskEvent(
	ctx workflow.Context,
	databaseID uuid.UUID,
	taskID uuid.UUID,
	entries ...task.LogEntry,
) error {
	if len(entries) == 0 {
		return nil
	}

	_, err := w.Activities.
		ExecuteLogTaskEvent(ctx, &activities.LogTaskEventInput{
			DatabaseID: databaseID,
			TaskID:     taskID,
			Entries:    entries,
		}).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to log task event: %w", err)
	}

	return nil
}
