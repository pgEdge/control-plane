package swarm

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLakekeeperBaseURL(t *testing.T) {
	// Default port when unset (0) is the in-container listen port 8181.
	assert.Equal(t, "http://172.18.0.5:8181", lakekeeperBaseURL("172.18.0.5", 0))
	// Explicit port is honored.
	assert.Equal(t, "http://172.18.0.5:9000", lakekeeperBaseURL("172.18.0.5", 9000))
}

func TestBridgeIPAddress(t *testing.T) {
	inspect := func(nets map[string]*network.EndpointSettings) types.ContainerJSON {
		return types.ContainerJSON{
			NetworkSettings: &types.NetworkSettings{Networks: nets},
		}
	}

	t.Run("returns the bridge IP", func(t *testing.T) {
		ip, err := bridgeIPAddress(inspect(map[string]*network.EndpointSettings{
			"bridge":        {IPAddress: "172.18.0.5"},
			"mydb-database": {IPAddress: "10.0.1.7"}, // overlay IP must not be picked
		}))
		require.NoError(t, err)
		assert.Equal(t, "172.18.0.5", ip)
	})

	t.Run("errors when the bridge network is absent", func(t *testing.T) {
		_, err := bridgeIPAddress(inspect(map[string]*network.EndpointSettings{
			"mydb-database": {IPAddress: "10.0.1.7"},
		}))
		assert.Error(t, err)
	})

	t.Run("errors when the bridge IP is empty", func(t *testing.T) {
		_, err := bridgeIPAddress(inspect(map[string]*network.EndpointSettings{
			"bridge": {IPAddress: ""},
		}))
		assert.Error(t, err)
	})
}
