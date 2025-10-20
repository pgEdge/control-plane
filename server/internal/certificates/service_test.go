package certificates_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
)

func TestService(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		etcd := storagetest.NewEtcdTestServer(t)
		client := etcd.Client(t)
		store := certificates.NewStore(client, uuid.NewString())
		service := certificates.NewService(store)

		ctx := context.Background()
		assert.NoError(t, service.Start(ctx))

		// Create a postgres user principal
		instanceID := uuid.NewString()
		principal, err := service.PostgresUser(ctx, instanceID, "postgres")
		assert.NoError(t, err)
		assert.NotNil(t, principal)

		// Verify the principal's certificate
		assert.NoError(t, service.Verify(principal.CertPEM))

		// Simulate restoring the service from the stored CA on the next startup
		restored := certificates.NewService(store)
		assert.NoError(t, restored.Start(ctx))

		// Verify that the restored service has the same CA
		assert.NoError(t, restored.Verify(principal.CertPEM))

		// Re-run the get principal operation to ensure the principal was stored
		restoredPrincipal, err := service.PostgresUser(ctx, instanceID, "postgres")
		assert.NoError(t, err)
		assert.NotNil(t, restoredPrincipal)

		assert.Equal(t, principal.ID, restoredPrincipal.ID)
		assert.Equal(t, principal.CertPEM, restoredPrincipal.CertPEM)
		assert.Equal(t, principal.KeyPEM, restoredPrincipal.KeyPEM)
	})
}
