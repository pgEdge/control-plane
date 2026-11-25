package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestPendingSlotCopyWarnings(t *testing.T) {
	state := resource.NewState()
	for _, instance := range []*database.InstanceResource{
		{Spec: &database.InstanceSpec{InstanceID: "db-n1-a"}},
		{
			Spec:                   &database.InstanceSpec{InstanceID: "db-n1-b"},
			SpockSlotCopiesPending: []string{"n1→n2", "n1→n3"},
		},
		{
			// Not changed by the plans, so not reported.
			Spec:                   &database.InstanceSpec{InstanceID: "db-n2-b"},
			SpockSlotCopiesPending: []string{"n2→n1"},
		},
	} {
		require.NoError(t, state.AddResource(instance))
	}

	event := func(typ resource.EventType, id string) *resource.Event {
		return &resource.Event{
			Type: typ,
			Resource: &resource.ResourceData{
				Identifier: database.InstanceResourceIdentifier(id),
			},
		}
	}
	plans := []resource.Plan{
		{{event(resource.EventTypeUpdate, "db-n1-b")}},
		{{event(resource.EventTypeUpdate, "db-n1-a"), event(resource.EventTypeRefresh, "db-n2-b")}},
		// Deleted, so no longer in the state.
		{{event(resource.EventTypeCreate, "db-n3-a")}},
	}

	warnings := pendingSlotCopyWarnings(state, plans)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "replica db-n1-b doesn't hold permanent copies")
	assert.Contains(t, warnings[0], "n1→n2, n1→n3")

	assert.Empty(t, pendingSlotCopyWarnings(state, nil))
}
