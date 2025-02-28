package ipam

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestService(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		etcd := storagetest.NewEtcdTestServer(t)
		client := etcd.Client(t)
		store := NewStore(client, uuid.NewString())
		cfg := config.Config{
			DockerSwarm: config.DockerSwarm{
				BridgeNetworksCIDR:         "172.17.128.0/20",
				BridgeNetworksSubnetBits:   28,
				DatabaseNetworksCIDR:       "10.0.128.0/18",
				DatabaseNetworksSubnetBits: 26,
			},
		}
		logger := zerolog.New(zerolog.NewTestWriter(t))

		service := NewService(cfg, logger, store)
		assert.NotNil(t, service)

		ctx := context.Background()
		assert.NoError(t, service.Start(ctx))

		// Allocate a bridge subnet
		bridge, err := service.AllocateBridgeSubnet(ctx)
		assert.NoError(t, err)
		assert.True(t, bridge.IsValid())

		// Allocate a database subnet
		database, err := service.AllocateDatabaseSubnet(ctx)
		assert.NoError(t, err)
		assert.True(t, database.IsValid())

		// Simulate restoring the allocators from snapshots on the next startup
		restored := NewService(cfg, logger, store)
		assert.NoError(t, restored.Start(ctx))
		assert.True(t, restored.bridgeAllocator.Has(bridge))
		assert.True(t, restored.databaseAllocator.Has(database))
	})
}
