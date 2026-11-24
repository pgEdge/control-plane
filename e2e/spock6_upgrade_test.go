//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// TestSpock5To6Upgrade upgrades a three-node database, where n1 has a
// replica, from Spock 5.0.14 to Spock 6 by changing spock_version while every
// node takes writes, then checks that:
//   - every write that succeeded during the upgrade reached every node,
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
		{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[2])}},
		{Name: "n3", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[3])}},
	}

	t.Log("creating a three-node spock 5 database, with a replica on n1")
	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: spockUpgradeSpec(dbName, "5", nodes),
	})

	n1Opts := ConnectionOptions{Matcher: And(WithNode("n1"), WithRole("primary")), Username: username, Password: password}
	n2Opts := ConnectionOptions{Matcher: And(WithNode("n2"), WithRole("primary")), Username: username, Password: password}
	n3Opts := ConnectionOptions{Matcher: And(WithNode("n3"), WithRole("primary")), Username: username, Password: password}

	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		var version string
		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.spock_version();").Scan(&version))
		t.Logf("spock version before the upgrade: %s", version)

		_, err := conn.Exec(ctx, "CREATE TABLE upgrade_writes (node TEXT, seq INT, PRIMARY KEY (node, seq));")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "CREATE TABLE items (id INT PRIMARY KEY, data TEXT);")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "INSERT INTO items VALUES (1, 'before-upgrade');")
		require.NoError(t, err)
	})
	waitForRow(ctx, t, db, n1Opts, n2Opts, "n1", 1, "before-upgrade")
	waitForRow(ctx, t, db, n1Opts, n3Opts, "n1", 1, "before-upgrade")

	waitForSpockVersionReported(ctx, t, db)

	t.Log("starting writes on every node")
	var writers []*upgradeWriter
	for _, node := range nodes {
		w := newUpgradeWriter(t, db, node.Name, username, password)
		writers = append(writers, w)
		go w.run(ctx)
	}
	for _, w := range writers {
		w.waitForWrites(t, 1, time.Minute)
	}

	t.Log("upgrading spock from 5 to 6")
	require.NoError(t, db.Update(ctx, UpdateOptions{
		Spec: spockUpgradeSpec(dbName, "6", nodes),
	}))

	t.Log("checking that writes continue after the upgrade")
	for _, w := range writers {
		w.waitForWrites(t, w.committedCount()+10, 2*time.Minute)
	}
	for _, w := range writers {
		w.stopAndWait()
	}
	require.NoError(t, db.Refresh(ctx))
	db.WaitForReplication(ctx, t, username, password)

	t.Log("checking that every write that succeeded reached every node")
	for instance := range db.GetInstances(WithRole(client.RolePrimary)) {
		opts := ConnectionOptions{Instance: instance, Username: username, Password: password}
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			for _, w := range writers {
				committed := w.committedSeqs()
				var found int
				require.NoError(t, conn.QueryRow(ctx,
					"SELECT count(*) FROM upgrade_writes WHERE node = $1 AND seq = ANY($2);",
					w.node, committed,
				).Scan(&found))
				assert.Equal(t, len(committed), found,
					"instance %s should have every write that succeeded on %s", instance.ID, w.node)
			}
		})
	}
	for _, w := range writers {
		t.Logf("node %s: %d writes succeeded, %d failed while instances restarted", w.node, w.committedCount(), w.failedCount())
	}

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
			// One slot per subscriber node.
			assert.Equal(t, len(nodes)-1, synced, "replica %s should hold a synced copy of each of its primary's slots", instance.ID)
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
	waitForRow(ctx, t, db, n2Opts, n3Opts, "n2", 2, "after-upgrade")

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
	db.WithConnection(ctx, n3Opts, t, func(conn *pgx.Conn) {
		require.True(t, waitFor(func() bool {
			var exists bool
			require.NoError(t, conn.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM information_schema.columns
				               WHERE table_name = 'items' AND column_name = 'note');`,
			).Scan(&exists))
			return exists
		}, 60*time.Second), "the new column should replicate to n3")
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

	// The former primary keeps its own Spock slots when it rejoins as a
	// replica. They must be dropped for native slot sync to copy the new
	// primary's slots to it.
	t.Log("checking that the former n1 primary gets synced slot copies")
	require.True(t, waitFor(func() bool {
		conn, err := db.ConnectToInstance(ctx, ConnectionOptions{Instance: original, Username: username, Password: password})
		if err != nil {
			return false
		}
		defer conn.Close(ctx)
		var inRecovery bool
		var stale, synced int
		err = conn.QueryRow(ctx, `
			SELECT pg_is_in_recovery(),
			       count(*) FILTER (WHERE NOT synced),
			       count(*) FILTER (WHERE synced)
			FROM pg_replication_slots
			WHERE plugin = 'spock_output' AND NOT temporary;`,
		).Scan(&inRecovery, &stale, &synced)
		return err == nil && inRecovery && stale == 0 && synced == len(nodes)-1
	}, 3*time.Minute), "the former n1 primary should hold only synced copies of the new primary's slots")
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

// upgradeWriter inserts rows on one node's primary until it's stopped. It
// connects through all of the node's instances with
// target_session_attrs=read-write, so it reconnects to whichever instance is
// the primary after a restart or switchover. It records only the rows whose
// insert succeeded: a row whose insert failed may or may not have committed.
type upgradeWriter struct {
	node string
	dsn  string
	stop chan struct{}
	done chan struct{}

	mu        sync.Mutex
	committed []int32
	failed    int
}

func newUpgradeWriter(t *testing.T, db *DatabaseFixture, node, username, password string) *upgradeWriter {
	t.Helper()

	var hosts, ports []string
	for instance := range db.GetInstances(WithNode(node)) {
		require.NotNil(t, instance.ConnectionInfo, "instance %s has no connection info", instance.ID)
		require.NotEmpty(t, instance.ConnectionInfo.Addresses, "instance %s has no addresses", instance.ID)
		require.NotNil(t, instance.ConnectionInfo.Port, "instance %s has no port", instance.ID)
		hosts = append(hosts, instance.ConnectionInfo.Addresses[0])
		ports = append(ports, strconv.Itoa(*instance.ConnectionInfo.Port))
	}
	require.NotEmpty(t, hosts, "node %s has no instances", node)

	return &upgradeWriter{
		node: node,
		dsn: fmt.Sprintf(
			"host=%s port=%s dbname=%s user=%s password=%s target_session_attrs=read-write connect_timeout=5",
			strings.Join(hosts, ","), strings.Join(ports, ","), db.Spec.DatabaseName, username, password,
		),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (w *upgradeWriter) run(ctx context.Context) {
	defer close(w.done)

	var conn *pgx.Conn
	defer func() {
		if conn != nil {
			_ = conn.Close(context.Background())
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var seq int32
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
		}
		if conn == nil {
			c, err := pgx.Connect(ctx, w.dsn)
			if err != nil {
				continue
			}
			conn = c
		}
		seq++
		_, err := conn.Exec(ctx, "INSERT INTO upgrade_writes (node, seq) VALUES ($1, $2);", w.node, seq)
		w.mu.Lock()
		if err != nil {
			w.failed++
		} else {
			w.committed = append(w.committed, seq)
		}
		w.mu.Unlock()
		if err != nil {
			_ = conn.Close(context.Background())
			conn = nil
		}
	}
}

func (w *upgradeWriter) stopAndWait() {
	close(w.stop)
	<-w.done
}

func (w *upgradeWriter) committedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.committed)
}

func (w *upgradeWriter) failedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

func (w *upgradeWriter) committedSeqs() []int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.committed)
}

// waitForWrites waits until at least count writes have succeeded.
func (w *upgradeWriter) waitForWrites(t *testing.T, count int, timeout time.Duration) {
	t.Helper()
	require.True(t, waitFor(func() bool {
		return w.committedCount() >= count
	}, timeout), "node %s should reach %d successful writes", w.node, count)
}
