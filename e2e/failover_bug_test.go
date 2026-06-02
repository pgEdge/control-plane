//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
	"github.com/stretchr/testify/require"
)

func TestFailoverBug(t *testing.T) {
	t.Parallel()

	hostIDs := fixture.HostIDs()
	host1 := hostIDs[0]
	host2 := hostIDs[1]
	host3 := hostIDs[2]

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	tLog(t, "creating database")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "failover_bug",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1), controlplane.Identifier(host2), controlplane.Identifier(host3)},
				},
				{
					Name:    "n2",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
				{
					Name:    "n3",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
		},
	})

	waitForFailoverSlots(ctx, t, db)

	tLog(t, "seeding test data on n1")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n1"), WithRole(client.RolePrimary)),
		Username: "admin",
		Password: "password",
	}, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `CREATE TABLE test (col int PRIMARY KEY, detail text);`)
		require.NoError(t, err)

		_, err = conn.Exec(ctx, `INSERT INTO test SELECT g, 'n1-seed-' || g FROM generate_series(1, 9) g;`)
		require.NoError(t, err)
	})

	tLog(t, "seeding test data on n2")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n2"), WithRole(client.RolePrimary)),
		Username: "admin",
		Password: "password",
	}, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `INSERT INTO test SELECT g, 'n2-seed-' || g FROM generate_series(10, 19) g;`)
		require.NoError(t, err)
	})

	tLog(t, "seeding test data on n3")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n3"), WithRole(client.RolePrimary)),
		Username: "admin",
		Password: "password",
	}, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `INSERT INTO test SELECT g, 'n3-seed-' || g FROM generate_series(20, 29) g;`)
		require.NoError(t, err)
	})

	db.WaitForReplication(ctx, t, "admin", "password")

	tLog(t, "validating row count across all instances")

	for _, instance := range db.Instances {
		db.WithConnection(ctx, ConnectionOptions{
			Instance: instance,
			Username: "admin",
			Password: "password",
		}, t, func(conn *pgx.Conn) {
			var count int
			require.NoError(t, conn.QueryRow(ctx, `select count(*) from test`).Scan(&count))
			require.Equal(t, 29, count)
		})
	}

	tLog(t, "querying lsns and bytes behind for all instances")

	for instance := range db.GetInstances(And(WithRole(client.RolePrimary), Or(WithNode("n2"), WithNode("n3")))) {
		db.WithConnection(ctx, ConnectionOptions{
			Instance: instance,
			Username: "admin",
			Password: "password",
		}, t, func(conn *pgx.Conn) {
			var result string
			const query = `SELECT slot_name
						|| ' conf_flush=' || confirmed_flush_lsn
						|| ' cur_wal=' || pg_current_wal_lsn()
						|| ' bytes_behind=' || (pg_current_wal_lsn() - confirmed_flush_lsn)
				FROM pg_replication_slots
				WHERE slot_name LIKE 'spk_%_n1';`
			require.NoError(t, conn.QueryRow(ctx, query).Scan(&result))

			tLogf(t, "lsns and bytes behind for instance %s: %s", instance.ID, result)
		})
	}

	tLog(t, "stopping n1 original primary")

	n1OriginalPrimary := db.GetInstance(And(WithRole(client.RolePrimary), WithNode("n1")))
	resp, err := fixture.Client.StopInstance(ctx, &controlplane.StopInstancePayload{
		DatabaseID: db.ID,
		InstanceID: n1OriginalPrimary.ID,
	})
	require.NoError(t, err)
	err = db.waitForTask(ctx, resp.Task)
	require.NoError(t, err)

	tLog(t, "waiting for new n1 primary to be promoted")

	require.True(t, waitFor(func() bool {
		require.NoError(t, db.Refresh(ctx))
		newPrimary := db.GetInstance(And(WithRole(client.RolePrimary)))
		return newPrimary != nil && newPrimary.ID != n1OriginalPrimary.ID
	}, 60*time.Second))

	tLog(t, "inserting new test data on n2")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n2"), WithRole(client.RolePrimary)),
		Username: "admin",
		Password: "password",
	}, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `INSERT INTO test VALUES (501, 'post-failover-from-n2');`)
		require.NoError(t, err)
	})

	tLog(t, "inserting new test data on n3")

	db.WithConnection(ctx, ConnectionOptions{
		Matcher:  And(WithNode("n3"), WithRole(client.RolePrimary)),
		Username: "admin",
		Password: "password",
	}, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `INSERT INTO test VALUES (601, 'post-failover-from-n3');`)
		require.NoError(t, err)
	})

	waitForFailoverSlots(ctx, t, db)

	tLog(t, "waiting for new test data to arrive on all primary instances")

	for instance := range db.GetInstances(WithRole(client.RolePrimary)) {
		db.WithConnection(ctx, ConnectionOptions{
			Instance: instance,
			Username: "admin",
			Password: "password",
		}, t, func(conn *pgx.Conn) {
			require.True(t, waitFor(func() bool {
				var count int
				require.NoError(t, conn.QueryRow(ctx, `select count(*) from test`).Scan(&count))

				var n2Exists bool
				require.NoError(t, conn.QueryRow(ctx, `select EXISTS(SELECT 1 FROM test WHERE col=501)`).Scan(&n2Exists))

				var n3Exists bool
				require.NoError(t, conn.QueryRow(ctx, `select EXISTS(SELECT 1 FROM test WHERE col=601)`).Scan(&n3Exists))

				tLogf(t, "instance=%s, count=%d, n2 row exists=%v, n3 row exists=%v", instance.ID, count, n2Exists, n3Exists)

				return count == 31 && n2Exists && n3Exists
			}, 30*time.Second))
		})
	}
}
