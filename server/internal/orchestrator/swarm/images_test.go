package swarm

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestVersions(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "127.0.0.1:5000/pgedge",
		},
	}
	versions := newTestVersions(t, cfg)

	t.Run("Supported", func(t *testing.T) {
		supported := versions.Supported()

		assert.NotEmpty(t, supported)

		for _, v := range supported {
			images, err := versions.GetImages(v)
			assert.NoError(t, err)
			assert.NotNil(t, images)
			assert.True(t, strings.HasPrefix(images.PgEdgeImage, "127.0.0.1:5000/pgedge"))
		}
	})

	t.Run("Default", func(t *testing.T) {
		v := versions.Default()

		assert.NotNil(t, v)

		images, err := versions.GetImages(v)
		assert.NoError(t, err)
		assert.NotNil(t, images)
		assert.True(t, strings.HasPrefix(images.PgEdgeImage, "127.0.0.1:5000/pgedge"))
	})
}
