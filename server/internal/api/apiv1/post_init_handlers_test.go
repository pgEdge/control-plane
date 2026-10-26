package apiv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

func apiErrMessage(t *testing.T, err error) string {
	t.Helper()
	apiErr, ok := err.(*api.APIError)
	require.True(t, ok, "expected *api.APIError, got %T", err)
	return apiErr.Message
}

func newTestInstance(nodeName string, state database.InstanceState, role patroni.InstanceRole) *database.Instance {
	r := role
	return &database.Instance{
		NodeName: nodeName,
		State:    state,
		Status:   &database.InstanceStatus{Role: &r},
	}
}

func TestIsNodeAvailable(t *testing.T) {
	t.Run("replica listed before primary", func(t *testing.T) {
		db := &database.Database{
			Instances: []*database.Instance{
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRoleReplica),
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRolePrimary),
			},
		}

		available, err := IsNodeAvailable(db, "n1")
		require.NoError(t, err)
		require.True(t, available)
	})

	t.Run("primary listed before replica", func(t *testing.T) {
		db := &database.Database{
			Instances: []*database.Instance{
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRolePrimary),
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRoleReplica),
			},
		}

		available, err := IsNodeAvailable(db, "n1")
		require.NoError(t, err)
		require.True(t, available)
	})

	t.Run("node has no available primary instance", func(t *testing.T) {
		db := &database.Database{
			Instances: []*database.Instance{
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRoleReplica),
			},
		}

		available, err := IsNodeAvailable(db, "n1")
		require.Error(t, err)
		require.False(t, available)
		msg := apiErrMessage(t, err)
		require.Contains(t, msg, "n1")
		require.NotContains(t, msg, "available)")
	})

	t.Run("node not found", func(t *testing.T) {
		db := &database.Database{
			Instances: []*database.Instance{
				newTestInstance("n1", database.InstanceStateAvailable, patroni.InstanceRolePrimary),
			},
		}

		available, err := IsNodeAvailable(db, "n2")
		require.Error(t, err)
		require.False(t, available)
		require.Contains(t, apiErrMessage(t, err), "not found")
	})

	t.Run("available instance with nil status does not panic", func(t *testing.T) {
		db := &database.Database{
			Instances: []*database.Instance{
				{NodeName: "n1", State: database.InstanceStateAvailable, Status: nil},
			},
		}

		available, err := IsNodeAvailable(db, "n1")
		require.Error(t, err)
		require.False(t, available)
	})
}
