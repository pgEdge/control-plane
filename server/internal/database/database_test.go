package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStoredToDatabaseDegradesWhenInstanceUnavailable(t *testing.T) {
	tests := []struct {
		name          string
		instanceState InstanceState
		wantState     DatabaseState
	}{
		{"available instance keeps database available", InstanceStateAvailable, DatabaseStateAvailable},
		{"degraded instance degrades database", InstanceStateDegraded, DatabaseStateDegraded},
		{"failed instance degrades database", InstanceStateFailed, DatabaseStateDegraded},
		{"unknown instance degrades database", InstanceStateUnknown, DatabaseStateDegraded},
		{"stopped instance degrades database", InstanceStateStopped, DatabaseStateDegraded},
		{"creating instance does not degrade database", InstanceStateCreating, DatabaseStateAvailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := &StoredDatabase{
				DatabaseID: "db1",
				State:      DatabaseStateAvailable,
			}
			storedSpec := &StoredSpec{}
			instances := []*Instance{
				{InstanceID: "i1", DatabaseID: "db1", State: tt.instanceState},
			}

			db := storedToDatabase(stored, storedSpec, instances, nil)

			assert.Equal(t, tt.wantState, db.State)
		})
	}
}

func TestStoredToDatabaseDoesNotDegradeNonAvailableDatabase(t *testing.T) {
	stored := &StoredDatabase{
		DatabaseID: "db1",
		State:      DatabaseStateCreating,
	}
	storedSpec := &StoredSpec{}
	instances := []*Instance{
		{InstanceID: "i1", DatabaseID: "db1", State: InstanceStateFailed},
	}

	db := storedToDatabase(stored, storedSpec, instances, nil)

	assert.Equal(t, DatabaseStateCreating, db.State)
}
