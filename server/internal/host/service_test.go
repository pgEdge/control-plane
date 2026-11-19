package host_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestServiceGetHost(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	store := host.NewStore(server.Client(t), uuid.NewString())
	svc := host.NewService(config.Config{}, nil, store, nil)

	err := store.Host.Put(&host.StoredHost{ID: "host-1"}).Exec(t.Context())
	require.NoError(t, err)

	t.Run("unknown host", func(t *testing.T) {
		// PLAT-732: a missing host must be reported as ErrHostNotFound rather
		// than a wrapped storage error, so that callers can tell "no such
		// host" apart from a storage failure.
		_, err := svc.GetHost(t.Context(), "nosuchhost")
		require.ErrorIs(t, err, host.ErrHostNotFound)
		assert.NotErrorIs(t, err, storage.ErrNotFound, "the storage error must not leak to callers")
		assert.NotContains(t, err.Error(), "/hosts/", "the storage key must not leak to callers")
	})

	t.Run("known host without a status", func(t *testing.T) {
		h, err := svc.GetHost(t.Context(), "host-1")
		require.NoError(t, err)
		assert.Equal(t, "host-1", h.ID)
	})
}
