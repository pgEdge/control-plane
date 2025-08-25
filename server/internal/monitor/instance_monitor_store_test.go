package monitor_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func TestInstanceMonitorStore(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	t.Run("PLAT-245", func(t *testing.T) {
		// PLAT-245: ensure that we don't get overlapping results when host ID
		// A is a prefix of host ID B.
		store := monitor.NewInstanceMonitorStore(client, uuid.NewString())

		err := store.Put(&monitor.StoredInstanceMonitor{
			HostID:           "host",
			DatabaseID:       "foo-database",
			InstanceID:       "foo-database-xyz",
			DatabaseName:     "foo",
			InstanceHostname: "foo-database-xyz",
		}).Exec(t.Context())
		require.NoError(t, err)

		err = store.Put(&monitor.StoredInstanceMonitor{
			HostID:           "host2",
			DatabaseID:       "bar-database",
			InstanceID:       "bar-database-xyz",
			DatabaseName:     "bar",
			InstanceHostname: "bar-database-xyz",
		}).Exec(t.Context())
		require.NoError(t, err)

		res, err := store.GetAllByHostID("host").Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "foo-database", res[0].DatabaseID)

		// Delete by prefix and ensure only the 'host' monitors are deleted.
		_, err = store.DeleteByHostID("host").Exec(t.Context())
		require.NoError(t, err)

		res, err = store.GetAllByHostID("host2").Exec(t.Context())
		require.NoError(t, err)

		assert.Len(t, res, 1)
		assert.Equal(t, "bar-database", res[0].DatabaseID)
	})
}
