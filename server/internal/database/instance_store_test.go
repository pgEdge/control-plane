package database_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestInstanceStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when database
		// ID A is a prefix of host ID B.
		store := database.NewInstanceStore(client, uuid.NewString())

		err := store.Put(&database.StoredInstance{
			DatabaseID: "database",
			InstanceID: "database-xyz",
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Put(&database.StoredInstance{
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

func TestStoredInstanceUpdateKeepStoredVersion(t *testing.T) {
	spock5 := ds.MustParsePgEdgeVersion("17.12", "5")
	spock6 := ds.MustParsePgEdgeVersion("17.12", "6")

	t.Run("keeps the stored version", func(t *testing.T) {
		instance := database.NewStoredInstance(&database.InstanceUpdateOptions{PgEdgeVersion: spock5})
		instance.Update(&database.InstanceUpdateOptions{
			State:             database.InstanceStateFailed,
			PgEdgeVersion:     spock6,
			KeepStoredVersion: true,
		})
		assert.Equal(t, spock5, instance.PgEdgeVersion)
		assert.Equal(t, database.InstanceStateFailed, instance.State)
	})
	t.Run("sets a version when none is stored", func(t *testing.T) {
		instance := database.NewStoredInstance(&database.InstanceUpdateOptions{})
		instance.Update(&database.InstanceUpdateOptions{PgEdgeVersion: spock6, KeepStoredVersion: true})
		assert.Equal(t, spock6, instance.PgEdgeVersion)
	})
	t.Run("replaces the stored version by default", func(t *testing.T) {
		instance := database.NewStoredInstance(&database.InstanceUpdateOptions{PgEdgeVersion: spock5})
		instance.Update(&database.InstanceUpdateOptions{PgEdgeVersion: spock6})
		assert.Equal(t, spock6, instance.PgEdgeVersion)
	})
}
