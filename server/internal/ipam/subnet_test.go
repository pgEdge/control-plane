package ipam_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ipam"
)

func TestSubnetRange(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		// initialize a new allocator
		allocator, err := ipam.NewSubnetRange(ipam.SubnetRangeSpec{
			CIDR:       netip.MustParsePrefix("172.17.96.0/20"),
			SubnetBits: 28,
		})
		assert.NoError(t, err)
		assert.NotNil(t, allocator)
		assert.Equal(t, "172.17.96.0/20", allocator.CIDR().String())
		assert.Equal(t, 0, allocator.Used())
		assert.Equal(t, 256, allocator.Free())

		// allocate a new prefix
		prefix, err := allocator.AllocateNext()
		assert.NoError(t, err)
		assert.True(t, prefix.IsValid())
		assert.Equal(t, 28, prefix.Bits())
		assert.True(t, allocator.Has(prefix))
		assert.Equal(t, 1, allocator.Used())
		assert.Equal(t, 255, allocator.Free())

		// Check iterator
		var containsPrefix bool
		allocator.ForEach(func(p netip.Prefix) {
			if p.String() == prefix.String() {
				containsPrefix = true
			}
		})
		assert.True(t, containsPrefix)

		// release the allocated prefix
		assert.NoError(t, allocator.Release(prefix))
		assert.False(t, allocator.Has(prefix))

		// manually allocate a prefix
		manualPrefix := netip.MustParsePrefix("172.17.96.0/28")
		assert.NoError(t, allocator.Allocate(manualPrefix))
		assert.True(t, allocator.Has(manualPrefix))

		// release the manually-allocated prefix
		assert.NoError(t, allocator.Release(manualPrefix))
		assert.False(t, allocator.Has(manualPrefix))
	})

	t.Run("snapshot restore", func(t *testing.T) {
		// we're using an unnormalized CIDR here to validate that the allocator
		// normalizes it, and that it doesn't affect the snapshot/restore
		// process.
		allocator, err := ipam.NewSubnetRange(ipam.SubnetRangeSpec{
			CIDR:       netip.MustParsePrefix("172.17.100.0/20"),
			SubnetBits: 28,
		})
		assert.NoError(t, err)
		assert.NotNil(t, allocator)

		// allocate a new prefix
		prefix, err := allocator.AllocateNext()
		assert.NoError(t, err)
		assert.True(t, prefix.IsValid())

		rangeSpec, snapshot, err := allocator.Snapshot()
		assert.NoError(t, err)
		// The rangeSpec should have the normalized CIDR
		assert.Equal(t, `{"cidr":"172.17.96.0/20","subnet_bits":28}`, rangeSpec)
		assert.NotEmpty(t, snapshot)

		restored, err := ipam.NewSubnetRange(ipam.SubnetRangeSpec{
			CIDR:       netip.MustParsePrefix("172.17.100.0/20"),
			SubnetBits: 28,
		})
		assert.NoError(t, err)
		assert.NotNil(t, restored)
		assert.NoError(t, restored.Restore(rangeSpec, snapshot))

		// Validate that the restored allocator has the same state as the original
		assert.True(t, restored.Has(prefix))
	})
}
