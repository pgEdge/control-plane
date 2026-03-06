package ports_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/ports"
)

func TestPortRange(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Equal(t, 100, r.Free())
		assert.Equal(t, 0, r.Used())

		// AllocateNext returns a port within the range
		port, err := r.AllocateNext()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, port, 5432)
		assert.LessOrEqual(t, port, 5531)
		assert.True(t, r.Has(port))
		assert.Equal(t, 99, r.Free())
		assert.Equal(t, 1, r.Used())

		// ForEach visits allocated ports
		var seen []int
		r.ForEach(func(p int) { seen = append(seen, p) })
		assert.Equal(t, []int{port}, seen)

		// Release returns the port to the pool
		require.NoError(t, r.Release(port))
		assert.False(t, r.Has(port))
		assert.Equal(t, 100, r.Free())

		// Allocate reserves a specific port
		require.NoError(t, r.Allocate(5500))
		assert.True(t, r.Has(5500))
		assert.Equal(t, 99, r.Free())

		// Release the manually-allocated port
		require.NoError(t, r.Release(5500))
		assert.False(t, r.Has(5500))
		assert.Equal(t, 100, r.Free())
	})

	t.Run("single port range", func(t *testing.T) {
		r, err := ports.NewPortRange(8080, 8080)
		require.NoError(t, err)
		assert.Equal(t, 1, r.Free())

		port, err := r.AllocateNext()
		require.NoError(t, err)
		assert.Equal(t, 8080, port)

		_, err = r.AllocateNext()
		assert.ErrorIs(t, err, ports.ErrFull)
	})

	t.Run("allocate specific port already in use returns ErrAllocated", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		require.NoError(t, r.Allocate(5440))
		err = r.Allocate(5440)
		assert.ErrorIs(t, err, ports.ErrAllocated)
	})

	t.Run("allocate port outside valid range [1, 65535] returns ErrNotInRange", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		err = r.Allocate(0)
		var notInRange *ports.ErrNotInRange
		assert.ErrorAs(t, err, &notInRange)
		assert.Equal(t, ports.MinValidPort, notInRange.Min)
		assert.Equal(t, ports.MaxValidPort, notInRange.Max)

		err = r.Allocate(65536)
		assert.ErrorAs(t, err, &notInRange)
	})

	t.Run("allocate port outside [min, max] but within [1, 65535] succeeds", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		// Ports outside [min, max] can be recorded; they don't count toward Free/Used.
		require.NoError(t, r.Allocate(5431))
		require.NoError(t, r.Allocate(5532))
		assert.True(t, r.Has(5431))
		assert.True(t, r.Has(5532))
		assert.Equal(t, 100, r.Free())
	})

	t.Run("release out-of-range port is a no-op", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)
		assert.NoError(t, r.Release(9999))
		assert.Equal(t, 100, r.Free())
	})

	t.Run("Has returns false for out-of-range port", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)
		assert.False(t, r.Has(0))
		assert.False(t, r.Has(65536))
	})

	t.Run("allocates all ports in range before exhausting", func(t *testing.T) {
		r, err := ports.NewPortRange(3000, 3004)
		require.NoError(t, err)

		seen := make(map[int]bool)
		for range 5 {
			port, err := r.AllocateNext()
			require.NoError(t, err)
			assert.GreaterOrEqual(t, port, 3000)
			assert.LessOrEqual(t, port, 3004)
			assert.False(t, seen[port], "port %d allocated twice", port)
			seen[port] = true
		}
		assert.Len(t, seen, 5)

		_, err = r.AllocateNext()
		assert.ErrorIs(t, err, ports.ErrFull)
	})

	t.Run("snapshot restore", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		port, err := r.AllocateNext()
		require.NoError(t, err)

		snapshot := r.Snapshot()
		assert.NotEmpty(t, snapshot)

		restored, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)
		require.NoError(t, restored.Restore(snapshot))

		assert.True(t, restored.Has(port))
		assert.Equal(t, 99, restored.Free())
	})

	t.Run("restore with different min/max succeeds and preserves allocations", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		require.NoError(t, r.Allocate(5440))

		snapshot := r.Snapshot()

		// Restore into a range with different min/max — should succeed.
		other, err := ports.NewPortRange(6000, 6099)
		require.NoError(t, err)
		require.NoError(t, other.Restore(snapshot))

		// The previously allocated port is still marked as allocated.
		assert.True(t, other.Has(5440))
	})

	t.Run("restore with wrong-size data returns error", func(t *testing.T) {
		r, err := ports.NewPortRange(5432, 5531)
		require.NoError(t, err)

		err = r.Restore([]byte("too short"))
		assert.Error(t, err)
	})
}

func TestNewPortRange_validation(t *testing.T) {
	t.Run("min greater than max", func(t *testing.T) {
		_, err := ports.NewPortRange(5531, 5432)
		assert.Error(t, err)
	})

	t.Run("port zero is invalid", func(t *testing.T) {
		_, err := ports.NewPortRange(0, 100)
		assert.Error(t, err)
	})

	t.Run("port above 65535 is invalid", func(t *testing.T) {
		_, err := ports.NewPortRange(1, 65536)
		assert.Error(t, err)
	})
}
