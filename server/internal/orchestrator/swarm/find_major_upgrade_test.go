package swarm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

// versionsWithSpock6 builds a manifest with a spock5 stable entry and a
// spock6 dev entry at the same postgres version, mirroring the real
// version-manifest.json shape used for the Spock 6 dev-stability entries.
func versionsWithSpock6(t *testing.T) *Versions {
	t.Helper()
	cfg := config.Config{DockerSwarm: config.DockerSwarm{ImageRepositoryHost: "ghcr.io/pgedge"}}
	v := &Versions{cfg: cfg, images: make(map[string]map[string]*Images)}
	v.addImage(ds.MustParsePgEdgeVersion("18.4", "5"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:18.4-spock5.0.10-standard-1",
		Stability:   "stable",
	})
	v.addImage(ds.MustParsePgEdgeVersion("18.4", "6"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1",
		Stability:   "dev",
	})
	v.addImage(ds.MustParsePgEdgeVersion("17.9", "5"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2",
		Stability:   "stable",
	})
	v.defaultVersion = ds.MustParsePgEdgeVersion("18.4", "5")
	return v
}

func TestOrchestrator_FindMajorUpgrade(t *testing.T) {
	v := versionsWithSpock6(t)
	o := newOrchestratorWithVersions(v)

	current := ds.MustParsePgEdgeVersion("18.4", "5")
	spock6Image := "ghcr.io/pgedge/pgedge-postgres:18.4-spock6.0.0-standard-1"
	spock5Image := "ghcr.io/pgedge/pgedge-postgres:18.4-spock5.0.10-standard-1"

	t.Run("happy path: finds a dev-stability cross-major target", func(t *testing.T) {
		got, err := o.FindMajorUpgrade(current, "6", spock6Image)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, spock6Image, got.Image)
		assert.Equal(t, "6", got.SpockVersion)
	})

	t.Run("rejects a target whose spock major equals the current major", func(t *testing.T) {
		_, err := o.FindMajorUpgrade(current, "5", spock5Image)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects a target image whose spock major does not match target_spock_version", func(t *testing.T) {
		_, err := o.FindMajorUpgrade(current, "6", spock5Image)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects a target in a different postgres major bucket", func(t *testing.T) {
		pg17Spock5 := "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2"
		_, err := o.FindMajorUpgrade(current, "6", pg17Spock5)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects image not in manifest", func(t *testing.T) {
		_, err := o.FindMajorUpgrade(current, "6", "ghcr.io/pgedge/pgedge-postgres:99.99-unknown")
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("invalid target_spock_version is rejected", func(t *testing.T) {
		_, err := o.FindMajorUpgrade(current, "not-a-version", spock6Image)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})
}
