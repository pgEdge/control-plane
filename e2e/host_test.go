//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// TestGetHost checks single-host reads through the API. PLAT-732: an unknown
// host ID answered 500 server_error and leaked the internal storage key
// instead of answering 404 not_found like the sibling databases endpoint.
func TestGetHost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	t.Run("known host", func(t *testing.T) {
		hostID := fixture.HostIDs()[0]

		got, err := fixture.Client.GetHost(ctx, &api.GetHostPayload{HostID: api.Identifier(hostID)})
		require.NoError(t, err)
		assert.Equal(t, api.Identifier(hostID), got.ID)
	})

	t.Run("unknown host is not found", func(t *testing.T) {
		_, err := fixture.Client.GetHost(ctx, &api.GetHostPayload{HostID: "nosuchhost"})
		require.ErrorIs(t, err, client.ErrNotFound)
		assert.ErrorContains(t, err, "no host found with the given ID")
		assert.NotContains(t, err.Error(), "/hosts/", "the storage key must not leak")
	})
}
