package workflows

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestCheckPrimaryNotRemoved(t *testing.T) {
	const databaseID = "test-db"
	instanceID := func(hostID string) string {
		return database.InstanceIDFor(hostID, databaseID, "n1")
	}

	// n1 currently runs on host-1 (primary) and host-2 (replica).
	newState := func(t *testing.T, primaryHostID string) *resource.State {
		t.Helper()
		state := resource.NewState()
		var ids []string
		for _, hostID := range []string{"host-1", "host-2"} {
			id := instanceID(hostID)
			ids = append(ids, id)
			require.NoError(t, state.AddResource(&database.InstanceResource{
				Spec: &database.InstanceSpec{
					InstanceID: id,
					DatabaseID: databaseID,
					HostID:     hostID,
					NodeName:   "n1",
				},
			}))
		}
		var primaryID string
		if primaryHostID != "" {
			primaryID = instanceID(primaryHostID)
		}
		require.NoError(t, state.AddResource(&database.NodeResource{
			Name:              "n1",
			InstanceIDs:       ids,
			PrimaryInstanceID: primaryID,
		}))
		return state
	}

	newSpec := func(nodes ...*database.Node) *database.Spec {
		return &database.Spec{DatabaseID: databaseID, Nodes: nodes}
	}

	for _, tc := range []struct {
		name        string
		primaryHost string
		spec        *database.Spec
		removeHosts []string
		expectedErr string
	}{
		{
			name:        "no instances removed",
			primaryHost: "host-1",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-1", "host-2"}}),
		},
		{
			name:        "replica removed",
			primaryHost: "host-1",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-1"}}),
		},
		{
			name:        "primary removed",
			primaryHost: "host-1",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-2"}}),
			expectedErr: "cannot remove host host-1 from node n1 because it runs the node's primary instance",
		},
		{
			name:        "primary removed after switchover",
			primaryHost: "host-2",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-2"}}),
		},
		{
			name:        "primary on forcibly removed host",
			primaryHost: "host-1",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-2"}}),
			removeHosts: []string{"host-1"},
		},
		{
			name:        "whole node removed",
			primaryHost: "host-1",
			spec:        newSpec(&database.Node{Name: "n2", HostIDs: []string{"host-3"}}),
		},
		{
			name:        "unknown primary",
			primaryHost: "",
			spec:        newSpec(&database.Node{Name: "n1", HostIDs: []string{"host-2"}}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newState(t, tc.primaryHost)

			err := checkPrimaryNotRemoved(tc.spec, state, tc.removeHosts)
			if tc.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectedErr)
			}
		})
	}
}
