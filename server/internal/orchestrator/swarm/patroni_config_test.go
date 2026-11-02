package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestPatroniConfigEtcdHostsOutdatedDiff(t *testing.T) {
	newConfig := func(outdated bool) *PatroniConfig {
		return &PatroniConfig{
			DatabaseID:          "database-1",
			DatabaseNetworkName: "database-1-database",
			Base: &common.PatroniConfig{
				InstanceID:        "database-1-n1-host-1",
				HostID:            "host-1",
				NodeName:          "n1",
				ParentID:          "database-1-n1-host-1-configs",
				Generator:         &common.PatroniConfigGenerator{},
				EtcdHostsOutdated: outdated,
			},
		}
	}

	desired, err := resource.ToResourceData(newConfig(false))
	require.NoError(t, err)

	t.Run("current", func(t *testing.T) {
		current, err := resource.ToResourceData(newConfig(false))
		require.NoError(t, err)

		diff, err := current.Diff(desired)
		require.NoError(t, err)
		assert.Empty(t, diff)
	})

	t.Run("outdated", func(t *testing.T) {
		current, err := resource.ToResourceData(newConfig(true))
		require.NoError(t, err)

		diff, err := current.Diff(desired)
		require.NoError(t, err)
		assert.NotEmpty(t, diff)
	})
}
