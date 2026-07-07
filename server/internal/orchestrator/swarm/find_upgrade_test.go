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

func testVersions(t *testing.T) *Versions {
	t.Helper()
	return newTestVersions(t, config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	})
}

func testVersionsWithStability(t *testing.T) *Versions {
	t.Helper()
	cfg := config.Config{DockerSwarm: config.DockerSwarm{ImageRepositoryHost: "ghcr.io/pgedge"}}
	v := &Versions{cfg: cfg, images: make(map[string]map[string]*Images)}
	v.addImage(ds.MustParsePgEdgeVersion("17.9", "5"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2",
		Stability:   "stable",
	})
	v.addImage(ds.MustParsePgEdgeVersion("17.10", "5"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.10-rc1-spock5.0.7-dev-1",
		Stability:   "dev",
	})
	v.addImage(ds.MustParsePgEdgeVersion("18.1", "5"), &Images{
		PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:18.1-spock5.0.4-standard-4",
		Stability:   "stable",
	})
	v.defaultVersion = ds.MustParsePgEdgeVersion("18.1", "5")
	return v
}

// ---- FindByImage -----------------------------------------------------------

func TestVersions_FindByImage(t *testing.T) {
	v := testVersions(t)

	// Pick a known image from the manifest.
	known := ds.MustParsePgEdgeVersion("17.9", "5")
	img, err := v.GetImages(known)
	require.NoError(t, err)
	knownImage := img.PgEdgeImage

	t.Run("returns version and images for a known image", func(t *testing.T) {
		ver, got, ok := v.FindByImage(knownImage)
		require.True(t, ok)
		require.NotNil(t, ver)
		require.NotNil(t, got)
		assert.Equal(t, knownImage, got.PgEdgeImage)
		assert.True(t, ver.PostgresVersion.Compare(known.PostgresVersion) == 0)
	})

	t.Run("returns false for an unknown image", func(t *testing.T) {
		ver, got, ok := v.FindByImage("ghcr.io/pgedge/pgedge-postgres:99.99-unknown")
		assert.False(t, ok)
		assert.Nil(t, ver)
		assert.Nil(t, got)
	})

	t.Run("returns false for empty string", func(t *testing.T) {
		_, _, ok := v.FindByImage("")
		assert.False(t, ok)
	})

	t.Run("each supported version has a distinct findable image", func(t *testing.T) {
		for _, ver := range v.supportedVersions {
			img, err := v.GetImages(ver)
			require.NoError(t, err)
			_, _, ok := v.FindByImage(img.PgEdgeImage)
			assert.True(t, ok, "could not find image %s back by value", img.PgEdgeImage)
		}
	})
}

// ---- Orchestrator.FindUpgrade ----------------------------------------------

func newOrchestratorWithVersions(versions *Versions) *Orchestrator {
	return &Orchestrator{versions: versions}
}

func TestOrchestrator_FindUpgrade(t *testing.T) {
	v := testVersions(t)
	o := newOrchestratorWithVersions(v)

	current := ds.MustParsePgEdgeVersion("17.9", "5")
	newer := ds.MustParsePgEdgeVersion("17.10", "5")

	newerImg, err := v.GetImages(newer)
	require.NoError(t, err)
	newerImage := newerImg.PgEdgeImage

	currentImg, err := v.GetImages(current)
	require.NoError(t, err)
	currentImage := currentImg.PgEdgeImage

	pg18 := ds.MustParsePgEdgeVersion("18.1", "5")
	pg18Img, err := v.GetImages(pg18)
	require.NoError(t, err)
	pg18Image := pg18Img.PgEdgeImage

	t.Run("happy path: returns upgrade for valid newer stable same-bucket image", func(t *testing.T) {
		got, err := o.FindUpgrade(current, newerImage)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, newerImage, got.Image)
		assert.Equal(t, newer.PostgresVersion.String(), got.PostgresVersion)
		assert.Equal(t, newer.SpockVersion.String(), got.SpockVersion)
	})

	t.Run("rejects image not in manifest", func(t *testing.T) {
		_, err := o.FindUpgrade(current, "ghcr.io/pgedge/pgedge-postgres:99.99-unknown")
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects same version (not strictly newer)", func(t *testing.T) {
		_, err := o.FindUpgrade(current, currentImage)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects older version (downgrade 17.10 → 17.9)", func(t *testing.T) {
		current1710 := ds.MustParsePgEdgeVersion("17.10", "5")
		_, err := o.FindUpgrade(current1710, currentImage) // 17.9 < 17.10
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("rejects image in a different postgres major bucket", func(t *testing.T) {
		_, err := o.FindUpgrade(current, pg18Image)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("dev stability is rejected", func(t *testing.T) {
		vs := testVersionsWithStability(t)
		od := newOrchestratorWithVersions(vs)
		cur := ds.MustParsePgEdgeVersion("17.9", "5")

		devImage := "ghcr.io/pgedge/pgedge-postgres:17.10-rc1-spock5.0.7-dev-1"
		_, err := od.FindUpgrade(cur, devImage)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("stable same-version is rejected", func(t *testing.T) {
		vs := testVersionsWithStability(t)
		od := newOrchestratorWithVersions(vs)
		cur := ds.MustParsePgEdgeVersion("17.9", "5")

		stableImage17_9 := "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2"
		// 17.9 is the current, there is no newer 17.x stable entry — should reject
		_, err := od.FindUpgrade(cur, stableImage17_9)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable), "same version should be rejected")
	})

	t.Run("rejects upgrade across different spock major buckets", func(t *testing.T) {
		cfg := config.Config{DockerSwarm: config.DockerSwarm{ImageRepositoryHost: "ghcr.io/pgedge"}}
		vs := &Versions{cfg: cfg, images: make(map[string]map[string]*Images)}
		vs.addImage(ds.MustParsePgEdgeVersion("17.9", "4"), &Images{
			PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.9-spock4.0.0-standard-1",
			Stability:   "stable",
		})
		vs.addImage(ds.MustParsePgEdgeVersion("17.10", "5"), &Images{
			PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.0-standard-1",
			Stability:   "stable",
		})
		vs.defaultVersion = ds.MustParsePgEdgeVersion("17.10", "5")

		od := newOrchestratorWithVersions(vs)
		curSpock4 := ds.MustParsePgEdgeVersion("17.9", "4")
		targetSpock5Image := "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.0-standard-1"

		_, err := od.FindUpgrade(curSpock4, targetSpock5Image)
		require.Error(t, err)
		assert.True(t, errors.Is(err, database.ErrUpgradeNotAvailable))
	})

	t.Run("returns correct fields in upgrade descriptor", func(t *testing.T) {
		got, err := o.FindUpgrade(current, newerImage)
		require.NoError(t, err)
		assert.NotEmpty(t, got.PostgresVersion)
		assert.NotEmpty(t, got.SpockVersion)
		assert.NotEmpty(t, got.Image)
		assert.Contains(t, got.Image, "ghcr.io/pgedge")
	})
}
