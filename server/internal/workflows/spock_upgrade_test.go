package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestDetectSpockUpgrade(t *testing.T) {
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
		return &database.Spec{
			DatabaseID:   databaseID,
			DatabaseName: "app",
			SpockVersion: spockVersion,
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1", "host-2"}},
				{Name: "n2", HostIDs: []string{"host-3", "host-4"}},
			},
		}
	}

	// Added out of order to check that the result is sorted.
	spock5 := newState(t,
		inst{node: "n2", instanceID: "test-db-n2-b", hostID: "host-4", postgres: "17.12", spock: "5"},
		inst{node: "n1", instanceID: "test-db-n1-b", hostID: "host-2", postgres: "17.12", spock: "5"},
		inst{node: "n2", instanceID: "test-db-n2-a", hostID: "host-3", postgres: "17.12", spock: "5"},
		inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12", spock: "5"},
	)
	spock6 := newState(t,
		inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12", spock: "6"},
		inst{node: "n2", instanceID: "test-db-n2-a", hostID: "host-3", postgres: "17.12", spock: "6"},
	)

	t.Run("spock major upgrade", func(t *testing.T) {
		upgrade, err := detectSpockUpgrade(spock5, spec("6"), nil, false)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.True(t, upgrade.RaisesMajor)
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
		upgrade, err := detectSpockUpgrade(state, spec("6"), nil, true)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.True(t, upgrade.RaisesMajor)
		assert.Len(t, upgrade.Instances, 2)
	})

	t.Run("pending upgrade with every instance on the new major", func(t *testing.T) {
		upgrade, err := detectSpockUpgrade(spock6, spec("6"), nil, true)
		require.NoError(t, err)
		require.NotNil(t, upgrade, "the remaining steps must run again")
		assert.False(t, upgrade.RaisesMajor)
		assert.Len(t, upgrade.Instances, 2)
	})

	t.Run("instances on removed hosts are left out", func(t *testing.T) {
		upgrade, err := detectSpockUpgrade(spock5, spec("6"), []string{"host-2", "host-4"}, false)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.Equal(t, []spockUpgradeInstance{
			{NodeName: "n1", InstanceID: "test-db-n1-a", HostID: "host-1"},
			{NodeName: "n2", InstanceID: "test-db-n2-a", HostID: "host-3"},
		}, upgrade.Instances)
	})

	t.Run("instances the update removes are left out", func(t *testing.T) {
		withoutN2 := spec("6")
		withoutN2.Nodes = withoutN2.Nodes[:1]
		withoutN2.Nodes[0].HostIDs = []string{"host-1"}
		upgrade, err := detectSpockUpgrade(spock5, withoutN2, nil, true)
		require.NoError(t, err)
		require.NotNil(t, upgrade)
		assert.Equal(t, []spockUpgradeInstance{
			{NodeName: "n1", InstanceID: "test-db-n1-a", HostID: "host-1"},
		}, upgrade.Instances)
	})

	for _, tc := range []struct {
		name    string
		state   *resource.State
		spec    *database.Spec
		pending bool
	}{
		{name: "finished upgrade", state: spock6, spec: spec("6")},
		{name: "same spock 5 major", state: spock5, spec: spec("5")},
		{name: "spock minor version", state: spock5, spec: spec("5.0.14")},
		{name: "spec without spock", state: spock5, spec: spec("")},
		{name: "no instances yet", state: resource.NewState(), spec: spec("6"), pending: true},
		{
			name: "instances without spock",
			state: newState(t,
				inst{node: "n1", instanceID: "test-db-n1-a", hostID: "host-1", postgres: "17.12"},
			),
			spec: spec("6"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upgrade, err := detectSpockUpgrade(tc.state, tc.spec, nil, tc.pending)
			require.NoError(t, err)
			assert.Nil(t, upgrade)
		})
	}
}

func TestNodesUpdatedByPlan(t *testing.T) {
	nodes := &spockInstances{
		Instances: []spockUpgradeInstance{
			{NodeName: "n1", InstanceID: "db-n1-a", HostID: "host-1"},
			{NodeName: "n1", InstanceID: "db-n1-b", HostID: "host-2"},
			{NodeName: "n2", InstanceID: "db-n2-a", HostID: "host-3"},
			{NodeName: "n3", InstanceID: "db-n3-a", HostID: "host-5"},
		},
	}
	event := func(typ resource.Type, id string) *resource.Event {
		return &resource.Event{
			Type:     resource.EventTypeUpdate,
			Resource: &resource.ResourceData{Identifier: resource.Identifier{Type: typ, ID: id}},
		}
	}
	instance := func(id string) *resource.Event {
		return event(database.ResourceTypeInstance, id)
	}
	// A rolling update: n1's replica, then n1's primary, then n2.
	plans := []resource.Plan{
		{{instance("db-n1-b")}},
		{{event("swarm.postgres_service", "db-n1-a")}, {instance("db-n1-a")}},
		{{instance("db-n2-a"), event(database.ResourceTypeNode, "n1")}},
	}

	assert.Equal(t, [][]spockUpgradeInstance{
		nil,
		{{NodeName: "n1", InstanceID: "db-n1-a", HostID: "host-1"}},
		{{NodeName: "n2", InstanceID: "db-n2-a", HostID: "host-3"}},
		// No plan changes n3.
		{{NodeName: "n3", InstanceID: "db-n3-a", HostID: "host-5"}},
	}, nodes.nodesUpdatedByPlan(plans))

	// Every node is in the last group when there are no plans.
	groups := nodes.nodesUpdatedByPlan(nil)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], 3)
}

func TestFixesSpockBeta1Catalog(t *testing.T) {
	for spockVersion, expected := range map[string]bool{
		"":      false,
		"5":     false,
		"6":     true,
		"6.0.0": true,
		"7":     false,
	} {
		assert.Equal(t, expected, fixesSpockBeta1Catalog(&database.Spec{SpockVersion: spockVersion}), spockVersion)
	}
}
