//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

// spockUpgradePostgresVersion has both a Spock 5.0.14 and a Spock 6 entry in
// the version manifest. Postgres 17 is the version where the upgrade switches
// replicas from Spock's failover-slots worker to native slot sync.
const spockUpgradePostgresVersion = "17.12"

func spockUpgradeSpec(dbName, spockVersion string, nodes []*controlplane.DatabaseNodeSpec) *controlplane.DatabaseSpec {
	return &controlplane.DatabaseSpec{
		DatabaseName:    dbName,
		PostgresVersion: pointerTo(spockUpgradePostgresVersion),
		SpockVersion:    pointerTo(spockVersion),
		Port:            pointerTo(0),
		PatroniPort:     pointerTo(0),
		DatabaseUsers: []*controlplane.DatabaseUserSpec{{
			Username:   "admin",
			Password:   pointerTo("password"),
			DbOwner:    pointerTo(true),
			Attributes: []string{"LOGIN", "SUPERUSER"},
		}},
		Nodes: nodes,
	}
}

// TestSpock5To6Upgrade upgrades a two-node database with a replica per node
// from Spock 5.0.14 to Spock 6 by changing spock_version, then checks that:
//   - every node runs Spock 6 and its extension matches the binaries,
//   - DDL replication is back on everywhere,
//   - every Spock slot has the failover flag, and each replica holds a copy
//     made by native slot sync,
//   - data written before the upgrade is intact and replication works both
//     ways afterwards, including DDL,
//   - replication continues after a failover.
func TestSpock5To6Upgrade(t *testing.T) {
	t.Parallel()

	fixture.SkipIfUpgradesUnsupported(t)
	fixture.SkipIfSpockNotInstalled(t)

	const (
		username = "admin"
		password = "password"
		dbName   = "spock_upgrade_db"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()
	nodes := []*controlplane.DatabaseNodeSpec{
		{Name: "n1", HostIds: []controlplane.Identifier{
			controlplane.Identifier(hostIDs[0]),
			controlplane.Identifier(hostIDs[1]),
		}},
		{Name: "n2", HostIds: []controlplane.Identifier{
			controlplane.Identifier(hostIDs[2]),
			controlplane.Identifier(hostIDs[3]),
		}},
	}

	t.Log("creating a two-node spock 5 database with replicas")
	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: spockUpgradeSpec(dbName, "5", nodes),
	})

	n1Opts := ConnectionOptions{Matcher: And(WithNode("n1"), WithRole("primary")), Username: username, Password: password}
	n2Opts := ConnectionOptions{Matcher: And(WithNode("n2"), WithRole("primary")), Username: username, Password: password}

	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		var version string
		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.spock_version();").Scan(&version))
		t.Logf("spock version before the upgrade: %s", version)

		_, err := conn.Exec(ctx, "CREATE TABLE items (id INT PRIMARY KEY, data TEXT);")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "INSERT INTO items VALUES (1, 'before-upgrade');")
		require.NoError(t, err)
	})
	waitForRow(ctx, t, db, n1Opts, n2Opts, "n1", 1, "before-upgrade")

	waitForSpockVersionReported(ctx, t, db)

	t.Log("upgrading spock from 5 to 6")
	require.NoError(t, db.Update(ctx, UpdateOptions{
		Spec: spockUpgradeSpec(dbName, "6", nodes),
	}))

	for instance := range db.GetInstances(WithRole(client.RolePrimary)) {
		opts := ConnectionOptions{Instance: instance, Username: username, Password: password}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			var version string
			require.NoError(t, conn.QueryRow(ctx, "SELECT spock.spock_version();").Scan(&version))
			assert.Regexp(t, `^6\.`, version, "instance %s should run spock 6", instance.ID)

			var installed, available string
			require.NoError(t, conn.QueryRow(ctx,
				"SELECT installed_version, default_version FROM pg_available_extensions WHERE name = 'spock';",
			).Scan(&installed, &available))
			assert.Equal(t, available, installed, "instance %s should have an up-to-date spock extension", instance.ID)

			var unflagged int
			require.NoError(t, conn.QueryRow(ctx,
				"SELECT count(*) FROM pg_replication_slots WHERE plugin = 'spock_output' AND NOT failover;",
			).Scan(&unflagged))
			assert.Zero(t, unflagged, "every spock slot on instance %s should have the failover flag", instance.ID)
		})
	}

	for instance := range db.GetInstances(func(*controlplane.Instance) bool { return true }) {
		opts := ConnectionOptions{Instance: instance, Username: username, Password: password}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			var ddl string
			require.NoError(t, conn.QueryRow(ctx, "SHOW spock.enable_ddl_replication;").Scan(&ddl))
			assert.Equal(t, "on", ddl, "ddl replication should be resumed on instance %s", instance.ID)
		})
	}

	for instance := range db.GetInstances(WithRole(client.RoleReplica)) {
		opts := ConnectionOptions{Instance: instance, Username: username, Password: password}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			var stale, synced int
			require.NoError(t, conn.QueryRow(ctx, `
				SELECT count(*) FILTER (WHERE NOT synced),
				       count(*) FILTER (WHERE synced)
				FROM pg_replication_slots
				WHERE plugin = 'spock_output' AND NOT temporary;`,
			).Scan(&stale, &synced))
			assert.Zero(t, stale, "replica %s should have no slot copies that native slot sync didn't make", instance.ID)
			assert.Equal(t, 1, synced, "replica %s should hold a synced copy of its primary's slot", instance.ID)
		})
	}

	t.Log("checking replication in both directions after the upgrade")
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		var data string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = 1;").Scan(&data))
		assert.Equal(t, "before-upgrade", data)
	})
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items VALUES (2, 'after-upgrade');")
		require.NoError(t, err)
	})
	waitForRow(ctx, t, db, n2Opts, n1Opts, "n2", 2, "after-upgrade")

	t.Log("checking ddl replication after the upgrade")
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "ALTER TABLE items ADD COLUMN note TEXT;")
		require.NoError(t, err)
	})
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		require.True(t, waitFor(func() bool {
			var exists bool
			require.NoError(t, conn.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM information_schema.columns
				               WHERE table_name = 'items' AND column_name = 'note');`,
			).Scan(&exists))
			return exists
		}, 60*time.Second), "the new column should replicate to n2")
	})

	t.Log("failing over n1 and checking replication")
	original := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
	require.NotNil(t, original)
	require.NoError(t, db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
		DatabaseID:     db.ID,
		NodeName:       "n1",
		SkipValidation: true,
	}))
	require.True(t, waitFor(func() bool {
		require.NoError(t, db.Refresh(ctx))
		primary := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
		return primary != nil && primary.ID != original.ID
	}, 2*time.Minute), "n1's primary should change")

	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items (id, data) VALUES (3, 'after-failover');")
		require.NoError(t, err)
	})
	waitForRow(ctx, t, db, n2Opts, n1Opts, "n2", 3, "after-failover")
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items (id, data) VALUES (4, 'from-new-primary');")
		require.NoError(t, err)
	})
	waitForRow(ctx, t, db, n1Opts, n2Opts, "n1", 4, "from-new-primary")
}

// TestSpockMajorVersionChangeRejected checks the update validation for Spock
// major version changes.
func TestSpockMajorVersionChangeRejected(t *testing.T) {
	t.Parallel()

	fixture.SkipIfUpgradesUnsupported(t)
	fixture.SkipIfSpockNotInstalled(t)

	nodes := []*controlplane.DatabaseNodeSpec{
		{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(fixture.HostIDs()[0])}},
	}

	t.Run("downgrade from spock 6 to 5", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
		defer cancel()
		const dbName = "spock_downgrade_db"
		db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
			Spec: spockUpgradeSpec(dbName, "6", nodes),
		})
		err := db.Update(ctx, UpdateOptions{Spec: spockUpgradeSpec(dbName, "5", nodes)})
		require.ErrorContains(t, err, "downgrading the spock major version is not supported")
	})

	t.Run("upgrade from a spock version older than 5.0.14", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
		defer cancel()
		const dbName = "spock_old_upgrade_db"
		// 17.11 ships with Spock 5.0.13.
		spec := spockUpgradeSpec(dbName, "5", nodes)
		spec.PostgresVersion = pointerTo("17.11")
		db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{Spec: spec})
		waitForSpockVersionReported(ctx, t, db)

		err := db.Update(ctx, UpdateOptions{Spec: spockUpgradeSpec(dbName, "6", nodes)})
		require.ErrorContains(t, err, "requires 5.0.14 or later")
	})
}

// waitForSpockVersionReported waits until every primary reports its Spock
// version. A Spock major upgrade is validated against the versions the
// instance monitor reports, which it fills in shortly after the database is
// created. The API only shows the Spock status of primaries, so replicas,
// whose status is collected in the same pass, can't be checked here.
func waitForSpockVersionReported(ctx context.Context, t *testing.T, db *DatabaseFixture) {
	t.Helper()

	require.True(t, waitFor(func() bool {
		require.NoError(t, db.Refresh(ctx))
		primaries := 0
		for instance := range db.GetInstances(WithRole(client.RolePrimary)) {
			primaries++
			if instance.Spock == nil || instance.Spock.Version == nil || *instance.Spock.Version == "" {
				return false
			}
		}
		return primaries > 0
	}, 2*time.Minute), "every primary should report its spock version")
	// Give the replicas' status, collected on the same interval, time to
	// land as well.
	time.Sleep(10 * time.Second)
}

// waitForRow waits for the row with the given id, written on the source node,
// to reach the target node with the given data.
func waitForRow(
	ctx context.Context,
	t *testing.T,
	db *DatabaseFixture,
	source, target ConnectionOptions,
	sourceNode string,
	id int,
	data string,
) {
	t.Helper()

	var lsn string
	db.WithConnection(ctx, source, t, func(conn *pgx.Conn) {
		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.sync_event();").Scan(&lsn))
	})
	db.WithConnection(ctx, target, t, func(conn *pgx.Conn) {
		var synced bool
		require.NoError(t, conn.QueryRow(ctx,
			"CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 60);", sourceNode, lsn,
		).Scan(&synced))
		require.True(t, synced, "row %d did not replicate from %s", id, sourceNode)

		var got string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = $1;", id).Scan(&got))
		assert.Equal(t, data, got)
	})
}
