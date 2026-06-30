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
		versions: NewVersions(config.Config{
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
		// Registry check skipped (no docker client). Image accepted as-is.
		assert.Empty(t, results)
	})

	t.Run("no result when Image differs from manifest (registry check skipped in unit tests)", func(t *testing.T) {
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
		// Registry check skipped (no docker client). Any image override is accepted as-is.
		assert.Empty(t, results)
	})

	t.Run("no result when Image is set for an unknown version", func(t *testing.T) {
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
}
