package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func (w *Workflows) applyEvents(ctx workflow.Context, databaseID uuid.UUID, state *resource.State, phases [][]*resource.Event) error {
	for _, phase := range phases {
		futures := make([]workflow.Future[*activities.ApplyEventOutput], len(phase))
		for i, event := range phase {
			in := &activities.ApplyEventInput{
				DatabaseID: databaseID,
				State:      state,
				Event:      event,
			}
			future, err := w.Activities.ExecuteApplyEvent(ctx, in)
			if errors.Is(err, activities.ErrExecutorNotFound) {
				// The executor is missing from the state, which can happen if a
				// resource was removed outside of control-plane and we've
				// updated our state to reflect that. We'll remove this resource
				// so that it can be recreated.
				// TODO: validate that this is always correct.
				state.Remove(event.Resource)
				continue
			} else if err != nil {
				return fmt.Errorf("failed to queue apply event: %w", err)
			}
			futures[i] = future
		}
		var errs []error
		for i, future := range futures {
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
