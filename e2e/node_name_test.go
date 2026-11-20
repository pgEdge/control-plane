//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// TestUnparseableNodeName checks that node names that the control plane can't
// derive a node ordinal from are rejected as invalid input. PLAT-706: these
// used to fail with a 500 from the spec validation workflow.
func TestUnparseableNodeName(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	spec := func(nodeNames ...string) *controlplane.DatabaseSpec {
		nodes := make([]*controlplane.DatabaseNodeSpec, len(nodeNames))
		for i, name := range nodeNames {
			nodes[i] = &controlplane.DatabaseNodeSpec{
				Name:    name,
				HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
			}
		}
		return &controlplane.DatabaseSpec{
			DatabaseName: "test_node_name",
			Port:         pointerTo(0),
			PatroniPort:  pointerTo(0),
			Nodes:        nodes,
		}
	}

	requireNotCreated := func(t *testing.T, id string) {
		_, err := fixture.Client.GetDatabase(ctx, &controlplane.GetDatabasePayload{
			DatabaseID: controlplane.Identifier(id),
		})
		require.ErrorIs(t, err, client.ErrNotFound, "the rejected database must not be created")
	}

	t.Run("create rejects a name that doesn't match the pattern", func(t *testing.T) {
		id := "test-node-name-pattern"
		_, err := fixture.Client.CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
			ID:   pointerTo(controlplane.Identifier(id)),
			Spec: spec("n1-east"),
		})
		require.Error(t, err)
		assert.NotErrorIs(t, err, client.ErrServerError)
		assert.ErrorContains(t, err, `must match the regexp "^n[0-9]+$" but got value "n1-east"`)
		requireNotCreated(t, id)
	})

	t.Run("create rejects a name whose number can't be parsed", func(t *testing.T) {
		// This name matches the pattern, so it's caught by the server-side
		// validation instead.
		id := "test-node-name-overflow"
		_, err := fixture.Client.CreateDatabase(ctx, &controlplane.CreateDatabaseRequest{
			ID:   pointerTo(controlplane.Identifier(id)),
			Spec: spec("n99999999999999999999"),
		})
		require.ErrorIs(t, err, client.ErrInvalidInput)
		assert.ErrorContains(t, err, `nodes[0].name: invalid node name "n99999999999999999999": node names must be "n" followed by a number, such as "n1"`)
		requireNotCreated(t, id)
	})

	t.Run("update rejects an unparseable name and leaves the database unchanged", func(t *testing.T) {
		// Names that parse still deploy.
		db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
			Spec: spec("n1"),
		})
		require.Equal(t, "available", db.State)

		err := db.Update(ctx, UpdateOptions{
			Spec: spec("n1", "n99999999999999999999"),
		})
		require.ErrorIs(t, err, client.ErrInvalidInput)
		assert.ErrorContains(t, err, `nodes[1].name: invalid node name "n99999999999999999999"`)

		require.NoError(t, db.Refresh(ctx))
		assert.Equal(t, "available", db.State)
		require.Len(t, db.Spec.Nodes, 1)
		assert.Equal(t, "n1", db.Spec.Nodes[0].Name)
	})
}
