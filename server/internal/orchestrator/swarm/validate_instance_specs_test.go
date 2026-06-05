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


func TestValidateInstanceSpecs_ImageWarnings(t *testing.T) {
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

	t.Run("no warning when Image is not set", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{NodeName: "n1", HostID: "host-1", PgEdgeVersion: knownVersion}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		for _, r := range results {
			assert.Empty(t, r.Warnings)
		}
	})

	t.Run("no warning when Image matches manifest exactly", func(t *testing.T) {
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
		for _, r := range results {
			assert.Empty(t, r.Warnings)
		}
	})

	t.Run("warning when Image differs from manifest image", func(t *testing.T) {
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

		var warningResult *database.ValidationResult
		for _, r := range results {
			if len(r.Warnings) > 0 {
				warningResult = r
				break
			}
		}
		require.NotNil(t, warningResult, "expected a result with warnings")
		assert.True(t, warningResult.Valid, "result with warning must still be valid")
		assert.Len(t, warningResult.Warnings, 1)
		assert.Contains(t, warningResult.Warnings[0], "my-custom-image")
		assert.Contains(t, warningResult.Warnings[0], manifestImage.PgEdgeImage)
	})

	t.Run("warning when Image is set for an unknown version", func(t *testing.T) {
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

		var warningResult *database.ValidationResult
		for _, r := range results {
			if len(r.Warnings) > 0 {
				warningResult = r
				break
			}
		}
		require.NotNil(t, warningResult, "expected a result with warnings")
		assert.True(t, warningResult.Valid)
		assert.Contains(t, warningResult.Warnings[0], "my-custom-image")
	})

	t.Run("warning result is valid (spec not rejected)", func(t *testing.T) {
		changes := []*database.InstanceSpecChange{
			{Current: &database.InstanceSpec{
				NodeName:      "n1",
				HostID:        "host-1",
				PgEdgeVersion: knownVersion,
				OrchestratorOpts: &database.OrchestratorOpts{
					Swarm: &database.SwarmOpts{Image: "custom:latest"},
				},
			}},
		}
		results, err := o.ValidateInstanceSpecs(ctx, changes)
		require.NoError(t, err)
		for _, r := range results {
			assert.True(t, r.Valid, "all results must be valid even with warnings")
		}
	})
}
