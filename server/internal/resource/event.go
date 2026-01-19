package resource

import (
	"context"
	"errors"
	"fmt"

	"github.com/wI2L/jsondiff"
)

type EventType string

const (
	EventTypeRefresh EventType = "refresh"
	EventTypeCreate  EventType = "create"
	EventTypeUpdate  EventType = "update"
	EventTypeDelete  EventType = "delete"
)

type EventReason string

const (
	EventReasonDoesNotExist      EventReason = "does_not_exist"
	EventReasonNeedsRecreate     EventReason = "needs_recreate"
	EventReasonHasDiff           EventReason = "has_diff"
	EventReasonForceUpdate       EventReason = "force_update"
	EventReasonDependencyUpdated EventReason = "dependency_updated"
	EventReasonHasError          EventReason = "has_error"
)

type Event struct {
	Type     EventType      `json:"type"`
	Resource *ResourceData  `json:"resource"`
	Reason   EventReason    `json:"reason,omitempty"`
	Diff     jsondiff.Patch `json:"diff,omitempty"`
}

// Apply applies this event to its resource. It does not modify the state in the
// given Context.
func (e *Event) Apply(ctx context.Context, rc *Context) error {
	resource, err := rc.Registry.Resource(e.Resource)
	if err != nil {
		return err
	}

	switch e.Type {
	case EventTypeRefresh:
		return e.refresh(ctx, rc, resource)
	case EventTypeCreate:
		return e.create(ctx, rc, resource)
	case EventTypeUpdate:
		return e.update(ctx, rc, resource)
	case EventTypeDelete:
		return e.delete(ctx, rc, resource)
	default:
		return fmt.Errorf("unknown event type: %s", e.Type)
	}
}

func (e *Event) refresh(ctx context.Context, rc *Context, resource Resource) error {
	var needsRecreate bool

	err := resource.Refresh(ctx, rc)
	if errors.Is(err, ErrNotFound) {
		needsRecreate = true
	} else if err != nil {
		return fmt.Errorf("failed to refresh resource %s: %w", resource.Identifier(), err)
	}

	updated, err := ToResourceData(resource)
	if err != nil {
		return err
	}
	updated.NeedsRecreate = needsRecreate

	e.Resource = updated

	return nil
}

func (e *Event) create(ctx context.Context, rc *Context, resource Resource) error {
	if err := resource.Create(ctx, rc); err != nil {
		return fmt.Errorf("failed to create resource %s: %w", resource.Identifier(), err)
	}

	updated, err := ToResourceData(resource)
	if err != nil {
		return err
	}

	e.Resource = updated

	return nil
}

func (e *Event) update(ctx context.Context, rc *Context, resource Resource) error {
	if err := resource.Update(ctx, rc); err != nil {
		return fmt.Errorf("failed to update resource %s: %w", resource.Identifier(), err)
	}

	updated, err := ToResourceData(resource)
	if err != nil {
		return err
	}

	e.Resource = updated

	return nil
}

func (e *Event) delete(ctx context.Context, rc *Context, resource Resource) error {
	if err := resource.Delete(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete resource %s: %w", resource.Identifier(), err)
	}

	updated, err := ToResourceData(resource)
	if err != nil {
		return err
	}

	e.Resource = updated

	return nil
}
