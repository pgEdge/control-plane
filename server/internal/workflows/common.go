package workflows

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

func (w *Workflows) applyEvents(ctx workflow.Context, databaseID uuid.UUID, state *resource.State, events []*resource.Event) error {
	active := ds.NewSet[resource.Identifier]()
	var futures []workflow.Future[*activities.ApplyEventOutput]
	var futureEvents []*resource.Event // for error reporting
	consumeFutures := func() error {
		var errs []error
		for i, f := range futures {
			out, err := f.Get(ctx)
			if err != nil {
				fEvent := futureEvents[i]
				errs = append(errs, fmt.Errorf("failed to apply %s event to %s: %w", fEvent.Type, fEvent.Resource.Identifier, err))
				continue
			}
			if err := state.Apply(out.Event); err != nil {
				fEvent := futureEvents[i]
				errs = append(errs, fmt.Errorf("failed to apply %s event from %s to state: %w", fEvent.Type, fEvent.Resource.Identifier, err))
			}
		}
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("failed while modifying resources: %w", err)
		}
		futures = nil
		futureEvents = nil
		active = ds.NewSet[resource.Identifier]()

		return nil
	}

	for _, event := range events {
		if slices.ContainsFunc(event.Resource.Dependencies, active.Has) {
			// This resource has dependencies that we're actively modifying.
			// We'll wait for the current futures to complete before continuing.
			if err := consumeFutures(); err != nil {
				return err
			}
		}
		active.Add(event.Resource.Identifier)
		in := &activities.ApplyEventInput{
			DatabaseID: databaseID,
			State:      state,
			Event:      event,
		}
		future, err := w.Activities.ExecuteApplyEvent(ctx, in)
		if errors.Is(err, activities.ErrExecutorNotFound) {
			// The executor is missing from the state, which can happen if a
			// resource was removed outside of control-plane and we've updated
			// our state to reflect that. We'll remove this resource so that it
			// can be recreated.
			// TODO: validate that this is always correct.
			state.Remove(event.Resource)
			continue
		} else if err != nil {
			return fmt.Errorf("failed to queue apply event: %w", err)
		}
		futures = append(futures, future)
		futureEvents = append(futureEvents, event)
	}

	// Loop over remaining futures
	if err := consumeFutures(); err != nil {
		return err
	}

	return nil
}
