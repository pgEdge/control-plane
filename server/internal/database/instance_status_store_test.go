package database_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestInstanceStatusStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when database
		// ID A is a prefix of host ID B.
		store := database.NewInstanceStatusStore(client, uuid.NewString())

		err := store.Put(&database.StoredInstanceStatus{
			DatabaseID: "database",
			InstanceID: "database-xyz",
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Put(&database.StoredInstanceStatus{
			DatabaseID: "database2",
			InstanceID: "database2-xyz",
		}).Exec(t.Context())
		require.NoError(t, err)

		res, err := store.GetByDatabaseID("database").Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "database", res[0].DatabaseID)
	})
}
