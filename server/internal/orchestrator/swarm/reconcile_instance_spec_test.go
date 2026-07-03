package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestReconcileInstanceSpec(t *testing.T) {
	o := &Orchestrator{
		versions: newTestVersions(t, config.Config{
			DockerSwarm: config.DockerSwarm{
				ImageRepositoryHost: "registry.example.com/pgedge",
			},
		}),
	}

	knownVersion := ds.MustParsePgEdgeVersion("17.9", "5")
	newKnownVersion := ds.MustParsePgEdgeVersion("17.10", "5")

	manifestImage, err := o.versions.GetImages(knownVersion)
	require.NoError(t, err)
	pinnedImage := manifestImage.PgEdgeImage

	t.Run("first creation: old nil, ResolvedImage written from manifest", func(t *testing.T) {
		spec := &database.InstanceSpec{PgEdgeVersion: knownVersion}
		require.NoError(t, o.ReconcileInstanceSpec(nil, spec))
		require.NotNil(t, spec.OrchestratorOpts)
		require.NotNil(t, spec.OrchestratorOpts.Swarm)
		assert.Equal(t, pinnedImage, spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("same version: old.ResolvedImage carried forward, manifest not re-consulted", func(t *testing.T) {
		old := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:pinned-tag"},
			},
		}
		newSpec := &database.InstanceSpec{PgEdgeVersion: knownVersion}

		require.NoError(t, o.ReconcileInstanceSpec(old, newSpec))
		require.NotNil(t, newSpec.OrchestratorOpts)
		require.NotNil(t, newSpec.OrchestratorOpts.Swarm)
		// Must use the stored pin, not re-derive from manifest.
		assert.Equal(t, "registry.example.com/pgedge:pinned-tag", newSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("same version, old has no ResolvedImage: manifest lookup runs", func(t *testing.T) {
		old := &database.InstanceSpec{PgEdgeVersion: knownVersion}
		newSpec := &database.InstanceSpec{PgEdgeVersion: knownVersion}

		require.NoError(t, o.ReconcileInstanceSpec(old, newSpec))
		require.NotNil(t, newSpec.OrchestratorOpts)
		assert.Equal(t, pinnedImage, newSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("version changed: old ResolvedImage cleared, manifest re-consulted", func(t *testing.T) {
		newVersionImage, err := o.versions.GetImages(newKnownVersion)
		require.NoError(t, err)

		old := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:old-tag"},
			},
		}
		newSpec := &database.InstanceSpec{
			PgEdgeVersion: newKnownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:old-tag"},
			},
		}

		require.NoError(t, o.ReconcileInstanceSpec(old, newSpec))
		// Must use the new version's manifest image, not the stale pin.
		assert.Equal(t, newVersionImage.PgEdgeImage, newSpec.OrchestratorOpts.Swarm.ResolvedImage)
		assert.NotEqual(t, "registry.example.com/pgedge:old-tag", newSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("user Image override: preserved, ResolvedImage from old still copied but case 1 wins", func(t *testing.T) {
		old := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:pinned-tag"},
			},
		}
		newSpec := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{Image: "my-custom/image:dev"},
			},
		}

		require.NoError(t, o.ReconcileInstanceSpec(old, newSpec))
		// Image override must win; resolveInstanceImages case 1 returns it directly.
		assert.Equal(t, "my-custom/image:dev", newSpec.OrchestratorOpts.Swarm.Image)
		// ResolvedImage is copied but resolveInstanceImages does not overwrite it in case 1.
		assert.Equal(t, "registry.example.com/pgedge:pinned-tag", newSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})
}
