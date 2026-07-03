package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestResolveInstanceImages(t *testing.T) {
	o := &Orchestrator{
		versions: NewVersions(config.Config{
			DockerSwarm: config.DockerSwarm{
				ImageRepositoryHost: "registry.example.com/pgedge",
			},
		}),
	}

	knownVersion := ds.MustParsePgEdgeVersion("17.9", "5")
	unknownVersion := ds.MustParsePgEdgeVersion("99.99", "5")

	t.Run("Image override used directly, manifest not consulted", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{
					Image: "my-registry/pgedge:dev-build",
				},
			},
		}

		images, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		assert.Equal(t, "my-registry/pgedge:dev-build", images.PgEdgeImage)
		// ResolvedImage must not be written when Image is set
		assert.Empty(t, spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("Image override works for unknown version (bypasses manifest)", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: unknownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{
					Image: "my-registry/pgedge:dev-build",
				},
			},
		}

		images, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		assert.Equal(t, "my-registry/pgedge:dev-build", images.PgEdgeImage)
	})

	t.Run("Image takes precedence over ResolvedImage", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{
					Image:         "custom-override:latest",
					ResolvedImage: "previously-resolved:tag",
				},
			},
		}

		images, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		assert.Equal(t, "custom-override:latest", images.PgEdgeImage)
		// ResolvedImage must not be touched when Image wins
		assert.Equal(t, "previously-resolved:tag", spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("ResolvedImage used when Image is empty", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{
				Swarm: &database.SwarmOpts{
					ResolvedImage: "registry.example.com/pgedge:previously-pinned",
				},
			},
		}

		images, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		assert.Equal(t, "registry.example.com/pgedge:previously-pinned", images.PgEdgeImage)
		// ResolvedImage must not be modified
		assert.Equal(t, "registry.example.com/pgedge:previously-pinned", spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: resolves from manifest and writes ResolvedImage", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: knownVersion,
		}

		images, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		assert.NotEmpty(t, images.PgEdgeImage)
		require.NotNil(t, spec.OrchestratorOpts)
		require.NotNil(t, spec.OrchestratorOpts.Swarm)
		assert.Equal(t, images.PgEdgeImage, spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: initialises nil OrchestratorOpts", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion:    knownVersion,
			OrchestratorOpts: nil,
		}

		_, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		require.NotNil(t, spec.OrchestratorOpts)
		require.NotNil(t, spec.OrchestratorOpts.Swarm)
		assert.NotEmpty(t, spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: initialises nil Swarm inside existing OrchestratorOpts", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion:    knownVersion,
			OrchestratorOpts: &database.OrchestratorOpts{Swarm: nil},
		}

		_, err := o.resolveInstanceImages(spec)
		require.NoError(t, err)
		require.NotNil(t, spec.OrchestratorOpts.Swarm)
		assert.NotEmpty(t, spec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: unknown version returns error", func(t *testing.T) {
		spec := &database.InstanceSpec{
			PgEdgeVersion: unknownVersion,
		}

		_, err := o.resolveInstanceImages(spec)
		assert.Error(t, err)
	})

	t.Run("manifest cache not mutated by lazy backfill", func(t *testing.T) {
		manifestImage, err := o.versions.GetImages(knownVersion)
		require.NoError(t, err)
		original := manifestImage.PgEdgeImage

		spec := &database.InstanceSpec{PgEdgeVersion: knownVersion}
		_, err = o.resolveInstanceImages(spec)
		require.NoError(t, err)

		// Calling GetImages again must return the same value — cache untouched
		after, err := o.versions.GetImages(knownVersion)
		require.NoError(t, err)
		assert.Equal(t, original, after.PgEdgeImage)
	})
}
