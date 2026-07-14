package swarm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestValidateInstanceSpecs_ImageValidation(t *testing.T) {
	o := &Orchestrator{
		versions: newTestVersions(t, config.Config{
			DockerSwarm: config.DockerSwarm{
				ImageRepositoryHost: "ghcr.io/pgedge",
			},
		}),
	}
	ctx := context.Background()

	knownVersion := ds.MustParsePgEdgeVersion("17.9", "5")
	unknownVersion := ds.MustParsePgEdgeVersion("99.99", "5")

	manifestImage, err := o.versions.GetImages(knownVersion)
	require.NoError(t, err)

	t.Run("no result when Image is not set", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{NodeName: "n1", HostID: "host-1", PgEdgeVersion: knownVersion}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("no result when Image matches manifest exactly", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion,
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: manifestImage.PgEdgeImage},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		// Registry check skipped (no docker client). Version check passes.
		assert.Empty(t, results)
	})

	t.Run("no result when image tag format is unrecognizable (dev build)", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion,
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:my-custom-image"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		// Unrecognizable tag format skips version validation and is accepted.
		assert.Empty(t, results)
	})

	t.Run("no result when image tag format is unrecognizable for unknown spec version", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: unknownVersion,
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:my-custom-image"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("no result for recognizable tag with matching versions (multi-digit patch)", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.10-standard-1"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("error when image tag postgres version mismatches spec", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:18.3-spock5.0.6-standard-2"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Valid)
		assert.Contains(t, results[0].Errors[0], "18.3")
		assert.Contains(t, results[0].Errors[0], "17.9")
	})

	t.Run("accepted when upgrading spock patch within same major (e.g. 5.0.9 → 5.0.10)", func(t *testing.T) {
		pgEdgeVersion := ds.MustParsePgEdgeVersion("17.10", "5")
		for _, img := range []string{
			"ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-2",
			"ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.10-standard-2",
		} {
			changes := []*database.InstanceSpecChange{
				{Current: &database.InstanceSpec{
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: pgEdgeVersion,
					OrchestratorOpts: &database.OrchestratorOpts{
						Swarm: &database.SwarmOpts{Image: img},
					},
				}},
			}
			results, err := o.ValidateInstanceSpecs(ctx, changes)
			require.NoError(t, err)
			assert.Emptyf(t, results, "image %s should be accepted", img)
		}
	})

	t.Run("no result for digest-pinned image with matching versions", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2@sha256:abc123"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("error for digest-pinned image with version mismatch", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:18.3-spock5.0.6-standard-2@sha256:abc123"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Valid)
		assert.Contains(t, results[0].Errors[0], "18.3")
	})

	t.Run("no result for major-only pg mutable tag (unrecognizable, skips version check)", func(t *testing.T) {
		// The API requires postgres_version in major.minor format, so a tag like
		// "17-spock5-standard" (major-only pg) is treated as unrecognizable and
		// accepted without version validation — same as a dev build tag.
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:17-spock5-standard"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("error when image tag spock version mismatches spec", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion, // 17.9 / 5
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "ghcr.io/pgedge/pgedge-postgres:17.9-spock4.0.0-standard-1"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Valid)
		assert.Contains(t, results[0].Errors[0], "4.0.0")
		assert.Contains(t, results[0].Errors[0], "5")
	})
}
