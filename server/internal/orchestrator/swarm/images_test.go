package swarm_test

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	"github.com/stretchr/testify/assert"
)

func TestVersions(t *testing.T) {
	// Versions is a collection of constant values that are determined at
	// startup. These tests validate that these constants are internally
	// consistent and match expectations, but they don't enforce specific values
	// so that the values can change without updating these tests.
	versions := swarm.NewVersions(config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "127.0.0.1:5000/pgedge",
		},
	})

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
