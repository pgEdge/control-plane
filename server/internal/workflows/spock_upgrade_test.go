package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestDetectSpockMajorUpgrade(t *testing.T) {
	const databaseID = "test-db"

	type inst struct {
		node, instanceID, hostID, postgres, spock string
	}
	newState := func(t *testing.T, instances ...inst) *resource.State {
		t.Helper()
		state := resource.NewState()
		for _, i := range instances {
			var version *ds.PgEdgeVersion
			if i.postgres != "" {
				version = ds.MustParsePgEdgeVersion(i.postgres, i.spock)
			}
			require.NoError(t, state.AddResource(&database.InstanceResource{
				Spec: &database.InstanceSpec{
					InstanceID:    i.instanceID,
					DatabaseID:    databaseID,
					HostID:        i.hostID,
					NodeName:      i.node,
					PgEdgeVersion: version,
				},
			}))
		}
		return state
	}
	spec := func(spockVersion string) *database.Spec {
		return &database.Spec{DatabaseID: databaseID, DatabaseName: "app", SpockVersion: spockVersion}
	}

	// Added out of order to check that the result is sorted.
	spock5 := newState(t,
		inst{node: "n2", instanceID: "test-db-n2-b", hostID: "host-4", postgres: "17.12", spock: "5"},
		inst{node: "n1", instanceID: "test-db-n1-b", hostID: "host-2", postgres: "17.12", spock: "5"},
		inst{node: "n2", instanceID: "test-db-n2-a", hostID: "host-3", postgres: "17.12", spock: "5"},
		inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12", spock: "5"},
	)

	t.Run("spock major upgrade", func(t *testing.T) {
		upgrade, err := detectSpockMajorUpgrade(spock5, spec("6"), nil)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.Equal(t, databaseID, upgrade.DatabaseID)
		assert.Equal(t, "app", upgrade.DatabaseName)
		assert.Equal(t, []spockUpgradeInstance{
			{NodeName: "n1", InstanceID: "test-db-n1-a", HostID: "host-1"},
			{NodeName: "n1", InstanceID: "test-db-n1-b", HostID: "host-2"},
			{NodeName: "n2", InstanceID: "test-db-n2-a", HostID: "host-3"},
			{NodeName: "n2", InstanceID: "test-db-n2-b", HostID: "host-4"},
		}, upgrade.Instances)
		assert.Equal(t, []spockUpgradeInstance{
			{NodeName: "n1", InstanceID: "test-db-n1-a", HostID: "host-1"},
			{NodeName: "n2", InstanceID: "test-db-n2-a", HostID: "host-3"},
		}, upgrade.nodes())

		primary, ok := upgrade.instance("test-db-n2-b")
		assert.True(t, ok)
		assert.Equal(t, "n2", primary.NodeName)
		_, ok = upgrade.instance("other")
		assert.False(t, ok)
	})

	t.Run("resumed upgrade with some instances already on the new major", func(t *testing.T) {
		state := newState(t,
			inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12", spock: "6"},
			inst{node: "n2", instanceID: "test-db-n2-a", hostID: "host-3", postgres: "17.12", spock: "5"},
		)
		upgrade, err := detectSpockMajorUpgrade(state, spec("6"), nil)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.Len(t, upgrade.Instances, 2)
	})

	for _, tc := range []struct {
		name  string
		state *resource.State
		spec  *database.Spec
	}{
		{name: "same spock major", state: spock5, spec: spec("5")},
		{name: "spock minor version", state: spock5, spec: spec("5.0.14")},
		{name: "spec without spock", state: spock5, spec: spec("")},
		{name: "no instances yet", state: resource.NewState(), spec: spec("6")},
		{
			name: "instances without spock",
			state: newState(t,
				inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12"},
			),
			spec: spec("6"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upgrade, err := detectSpockMajorUpgrade(tc.state, tc.spec, nil)
			require.NoError(t, err)
			assert.Nil(t, upgrade)
		})
	}
}
