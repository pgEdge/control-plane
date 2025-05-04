package ipam

import (
	"context"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestService(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		etcd := storagetest.NewEtcdTestServer(t)
		client := etcd.Client(t)
		store := NewStore(client, uuid.NewString())
		prefix := netip.MustParsePrefix("172.17.128.0/20")
		bits := 28
		logger := zerolog.New(zerolog.NewTestWriter(t))

		service := NewService(logger, store)
		assert.NotNil(t, service)

		// Allocate a subnet
		ctx := context.Background()
		subnetA, err := service.AllocateSubnet(ctx, prefix, bits)
		assert.NoError(t, err)
		assert.True(t, subnetA.IsValid())

		// Ensure the allocator is persisted
		stored, err := store.GetByKey(prefix.String()).Exec(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, stored)

		// Allocate another subnet. This should use the persisted allocator and
		// produce a different subnet.
		subnetB, err := service.AllocateSubnet(ctx, prefix, bits)
		assert.NoError(t, err)
		assert.True(t, subnetB.IsValid())
		assert.NotEqual(t, subnetA.String(), subnetB.String())
	})
}
