package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestVersions_AvailableUpgrades(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	v := NewVersions(cfg)

	t.Run("returns newer entries in same bucket", func(t *testing.T) {
		current := ds.MustParsePgEdgeVersion("17.9", "5")
		upgrades := v.AvailableUpgrades(current)
		assert.NotEmpty(t, upgrades)
		for _, u := range upgrades {
			assert.Equal(t, "17", ds.MustParseVersion(u.PostgresVersion).MajorVersion().String())
			assert.Equal(t, "5", ds.MustParseVersion(u.SpockVersion).MajorVersion().String())
			cmp := ds.MustParseVersion(u.PostgresVersion).Compare(current.PostgresVersion)
			assert.Greater(t, cmp, 0, "upgrade %s must be newer than current 17.9", u.PostgresVersion)
		}
	})

	t.Run("excludes entries from different postgres major", func(t *testing.T) {
		current := ds.MustParsePgEdgeVersion("17.9", "5")
		upgrades := v.AvailableUpgrades(current)
		for _, u := range upgrades {
			assert.NotContains(t, u.PostgresVersion, "16.", "should not include pg16 entries")
			assert.NotContains(t, u.PostgresVersion, "18.", "should not include pg18 entries")
		}
	})

	t.Run("returns nil when no newer entries exist (already at latest)", func(t *testing.T) {
		// 18.4 is the newest in pg18 bucket — no upgrades available
		current := ds.MustParsePgEdgeVersion("18.4", "5")
		upgrades := v.AvailableUpgrades(current)
		assert.Nil(t, upgrades)
	})

	t.Run("returns nil for nil current version", func(t *testing.T) {
		assert.Nil(t, v.AvailableUpgrades(nil))
	})

	t.Run("dev stability entries are excluded", func(t *testing.T) {
		devVersions := &Versions{
			cfg:    cfg,
			images: make(map[string]map[string]*Images),
		}
		devVersions.addImage(ds.MustParsePgEdgeVersion("17.9", "5"), &Images{
			PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2",
			Stability:   "stable",
		})
		devVersions.addImage(ds.MustParsePgEdgeVersion("17.10", "5"), &Images{
			PgEdgeImage: "ghcr.io/pgedge/pgedge-postgres:17.10-rc1-spock5.0.7-dev-1",
			Stability:   "dev",
		})

		current := ds.MustParsePgEdgeVersion("17.9", "5")
		upgrades := devVersions.AvailableUpgrades(current)
		assert.Nil(t, upgrades, "dev entries must not appear as available upgrades")
	})

	t.Run("image field is populated", func(t *testing.T) {
		current := ds.MustParsePgEdgeVersion("17.9", "5")
		upgrades := v.AvailableUpgrades(current)
		for _, u := range upgrades {
			assert.NotEmpty(t, u.Image, "upgrade entry must have an image")
			assert.Contains(t, u.Image, "ghcr.io/pgedge")
		}
	})
}
