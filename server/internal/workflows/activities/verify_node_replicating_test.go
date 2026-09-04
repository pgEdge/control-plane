package activities

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

// newTestDBService constructs a database.Service wired to an embedded test
// etcd, with no orchestrator — checkNodeReplicating never calls it, since it
// only reads back already-collected instance status.
func newTestDBService(t *testing.T) *database.Service {
	t.Helper()
	srv := storagetest.NewEtcdTestServer(t)
	client := srv.Client(t)
	store := database.NewStore(client, uuid.NewString())
	logFactory, err := logging.NewFactory(config.Config{}, zerolog.Nop())
	require.NoError(t, err)
	return database.NewService(config.Config{}, nil, store, nil, nil, logFactory)
}

// putInstance creates (or upserts) an instance record and its status.
func putInstance(t *testing.T, dbSvc *database.Service, databaseID, instanceID, nodeName string, role patroni.InstanceRole, subs []database.SubscriptionStatus, statusUpdatedAt time.Time) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
		InstanceID: instanceID,
		DatabaseID: databaseID,
		HostID:     "host-1",
		NodeName:   nodeName,
		State:      database.InstanceStateAvailable,
	}))
	require.NoError(t, dbSvc.UpdateInstanceStatus(ctx, databaseID, instanceID, &database.InstanceStatus{
		Role:            &role,
		Subscriptions:   subs,
		StatusUpdatedAt: &statusUpdatedAt,
	}))
}

func TestCheckNodeReplicating(t *testing.T) {
	replicatingSub := func(name, provider string) database.SubscriptionStatus {
		return database.SubscriptionStatus{Name: name, ProviderNode: provider, Status: postgres.SubStatusReplicating}
	}
	downSub := func(name, provider string) database.SubscriptionStatus {
		return database.SubscriptionStatus{Name: name, ProviderNode: provider, Status: "down"}
	}

	t.Run("single-node database: zero expected, zero observed passes", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		putInstance(t, dbSvc, "db1", "db1-n1", "n1", patroni.InstanceRolePrimary, nil, time.Now())

		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db1", "n1", 0)
		require.NoError(t, err)
		assert.Empty(t, reason)
	})

	t.Run("multi-node database: zero observed when subscriptions are expected does not pass", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		putInstance(t, dbSvc, "db2", "db2-n1", "n1", patroni.InstanceRolePrimary, nil, time.Now())

		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db2", "n1", 2)
		require.NoError(t, err)
		assert.NotEmpty(t, reason, "must not pass just because subscriptions haven't registered yet")
		assert.Contains(t, reason, "0 of 2 expected subscriptions")
	})

	t.Run("multi-node database: all expected subscriptions replicating passes", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		subs := []database.SubscriptionStatus{replicatingSub("sub_n1n2", "n2"), replicatingSub("sub_n1n3", "n3")}
		putInstance(t, dbSvc, "db3", "db3-n1", "n1", patroni.InstanceRolePrimary, subs, time.Now())

		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db3", "n1", 2)
		require.NoError(t, err)
		assert.Empty(t, reason)
	})

	t.Run("a subscription not yet replicating does not pass", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		subs := []database.SubscriptionStatus{replicatingSub("sub_n1n2", "n2"), downSub("sub_n1n3", "n3")}
		putInstance(t, dbSvc, "db4", "db4-n1", "n1", patroni.InstanceRolePrimary, subs, time.Now())

		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db4", "n1", 2)
		require.NoError(t, err)
		assert.Contains(t, reason, `"down"`)
	})

	t.Run("no primary found yet does not pass", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db5", "n1", 2)
		require.NoError(t, err)
		assert.Contains(t, reason, "no primary instance found")
	})

	t.Run("stale status does not pass", func(t *testing.T) {
		dbSvc := newTestDBService(t)
		subs := []database.SubscriptionStatus{replicatingSub("sub_n1n2", "n2"), replicatingSub("sub_n1n3", "n3")}
		putInstance(t, dbSvc, "db6", "db6-n1", "n1", patroni.InstanceRolePrimary, subs, time.Now().Add(-time.Hour))

		// storedToInstance already demotes a stale "available" instance to
		// State: Unknown, Status: nil before checkNodeReplicating ever sees
		// it, so this surfaces as "no primary found" rather than reaching
		// checkNodeReplicating's own staleness branch — either way, it must
		// not pass.
		reason, err := checkNodeReplicating(t.Context(), dbSvc, "db6", "n1", 2)
		require.NoError(t, err)
		assert.NotEmpty(t, reason)
	})
}
