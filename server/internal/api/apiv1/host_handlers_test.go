package apiv1

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

// newHostTestHandlers returns handlers backed by a real host service and an
// embedded etcd server, with stored records for the given host IDs. The
// handlers run as "host-1".
func newHostTestHandlers(t *testing.T, hostIDs ...string) *PostInitHandlers {
	t.Helper()

	server := storagetest.NewEtcdTestServer(t)
	store := host.NewStore(server.Client(t), uuid.NewString())
	for _, id := range hostIDs {
		err := store.Host.Put(&host.StoredHost{
			ID: id,
			// Every registered host reports a default version, and hostToAPI
			// depends on it.
			DefaultPgEdgeVersion: ds.MustParsePgEdgeVersion("18", "5"),
		}).Exec(t.Context())
		require.NoError(t, err)
	}

	cfg := config.Config{HostID: "host-1"}
	return &PostInitHandlers{
		cfg:     cfg,
		hostSvc: host.NewService(cfg, nil, store, nil),
	}
}

func requireAPIError(t *testing.T, err error) *api.APIError {
	t.Helper()

	var target *api.APIError
	require.ErrorAs(t, err, &target)
	return target
}

// PLAT-732: GET /v1/hosts/{host_id} answered 500 server_error for an unknown
// host and echoed the internal storage key in its message.
func TestGetHost(t *testing.T) {
	h := newHostTestHandlers(t, "host-1", "host-2")

	t.Run("unknown host is not_found", func(t *testing.T) {
		_, err := h.GetHost(t.Context(), &api.GetHostPayload{HostID: "nosuchhost"})

		got := requireAPIError(t, err)
		assert.Equal(t, errNotFound, got.Name)
		assert.Equal(t, "no host found with the given ID", got.Message)
		assert.NotContains(t, got.Message, "/hosts/", "the storage key must not leak")
		assert.NotContains(t, got.Message, "key not found", "the storage error must not leak")
	})

	t.Run("known host", func(t *testing.T) {
		got, err := h.GetHost(t.Context(), &api.GetHostPayload{HostID: "host-2"})
		require.NoError(t, err)
		assert.Equal(t, api.Identifier("host-2"), got.ID)
	})
}

// Joining a cluster relies on a missing host meaning "this host ID is free",
// so it must keep working with ErrHostNotFound.
func TestValidateHostIDUniqueness(t *testing.T) {
	h := newHostTestHandlers(t, "host-1")

	t.Run("unknown host ID is unique", func(t *testing.T) {
		err := validateHostIDUniqueness(t.Context(), h.hostSvc, "host-2")
		assert.NoError(t, err)
	})

	t.Run("existing host ID is rejected", func(t *testing.T) {
		err := validateHostIDUniqueness(t.Context(), h.hostSvc, "host-1")

		got := requireAPIError(t, err)
		assert.Equal(t, errInvalidInput, got.Name)
		assert.Contains(t, got.Message, "a host with ID host-1 already exists")
	})
}

func TestValidateHostRemoval(t *testing.T) {
	h := newHostTestHandlers(t, "host-1")

	t.Run("unknown host is not_found", func(t *testing.T) {
		err := h.validateHostRemoval(t.Context(), "nosuchhost", false)
		assert.Equal(t, ErrHostNotFound, err)
	})

	t.Run("removing itself is invalid", func(t *testing.T) {
		err := h.validateHostRemoval(t.Context(), "host-1", false)

		got := requireAPIError(t, err)
		assert.Equal(t, errInvalidInput, got.Name)
	})
}

func TestAPIErrHostNotFound(t *testing.T) {
	t.Run("wrapped ErrHostNotFound is not_found", func(t *testing.T) {
		err := apiErr(fmt.Errorf("failed to get host: %w", host.ErrHostNotFound))
		assert.Equal(t, ErrHostNotFound, err)
	})

	t.Run("other errors are still server_error", func(t *testing.T) {
		got := requireAPIError(t, apiErr(errors.New("etcd unavailable")))
		assert.Equal(t, errServerError, got.Name)
	})
}
