package database

import (
	"testing"

	"github.com/samber/do"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestSlotWaitTimedOut(t *testing.T) {
	rc := &resource.Context{Injector: do.New()}
	newReplica := func() *InstanceResource {
		return &InstanceResource{Spec: &InstanceSpec{InstanceID: "db-n1-b"}}
	}

	t.Run("without a complete check", func(t *testing.T) {
		r := newReplica()
		err := r.slotWaitTimedOut(rc, false, nil, false)
		require.ErrorContains(t, err, "timed out checking")
		assert.Nil(t, r.SpockSlotCopiesPending)
	})
	t.Run("copies from spock's own worker", func(t *testing.T) {
		r := newReplica()
		err := r.slotWaitTimedOut(rc, true, []string{"n1→n2"}, false)
		require.ErrorContains(t, err, "timed out waiting for a copy of the replication slot for n1→n2")
		assert.Nil(t, r.SpockSlotCopiesPending)
	})
	t.Run("native slot sync records the missing copies", func(t *testing.T) {
		r := newReplica()
		require.NoError(t, r.slotWaitTimedOut(rc, true, []string{"n1→n2", "n1→n3"}, true))
		assert.Equal(t, []string{"n1→n2", "n1→n3"}, r.SpockSlotCopiesPending)
	})
}

func TestRefreshSpockSlotCopiesPendingWithoutNativeSlotSync(t *testing.T) {
	rc := &resource.Context{Injector: do.New(), State: resource.NewState()}

	t.Run("primary", func(t *testing.T) {
		r := &InstanceResource{
			Spec:                   &InstanceSpec{InstanceID: "db-n1-a"},
			PrimaryInstanceID:      "db-n1-a",
			SpockSlotCopiesPending: []string{"n1→n2"},
		}
		r.refreshSpockSlotCopiesPending(t.Context(), rc)
		assert.Nil(t, r.SpockSlotCopiesPending)
	})
	t.Run("replica without native slot sync", func(t *testing.T) {
		r := &InstanceResource{
			Spec:                   &InstanceSpec{InstanceID: "db-n1-b"},
			PrimaryInstanceID:      "db-n1-a",
			SpockSlotCopiesPending: []string{"n1→n2"},
		}
		r.refreshSpockSlotCopiesPending(t.Context(), rc)
		assert.Nil(t, r.SpockSlotCopiesPending)
	})
}
