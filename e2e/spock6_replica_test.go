//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// TestSpock6ReplicaOnIdleDatabase adds a replica to a Spock 6 database on
// Postgres 17 after the database has been idle. Native slot sync keeps a new
// slot copy temporary until the subscriber confirms new changes, which an
// idle database doesn't produce, so the replica must not depend on that to
// become available. The test then checks that, once there are changes, the
// replica gets permanent copies, a switchover to it keeps replication going
// both ways, and the former primary ends up with only synced copies.
func TestSpock6ReplicaOnIdleDatabase(t *testing.T) {
	t.Parallel()
	fixture.SkipIfSpock6Unsupported(t)
	fixture.SkipIfSpockNotInstalled(t)
	if fixture.Orchestrator() == "systemd" {
		t.Skip("native slot sync needs Postgres 17 or later, and systemd hosts pick their own Postgres version")
	}

	const (
		username = "admin"
		password = "password"
		dbName   = "spock6_idle_replica_db"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()
	spec := func(n1Hosts ...string) *controlplane.DatabaseSpec {
		var n1 []controlplane.Identifier
		for _, h := range n1Hosts {
			n1 = append(n1, controlplane.Identifier(h))
		}
		return &controlplane.DatabaseSpec{
			DatabaseName:    dbName,
			PostgresVersion: pointerTo("17.12"),
			SpockVersion:    pointerTo("6"),
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{{
				Username:   username,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			}},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: n1},
				{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[1])}},
			},
		}
	}

	t.Log("creating a two-node spock 6 database")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: spec(hostIDs[0]),
	})
	n1Opts := ConnectionOptions{Matcher: And(WithNode("n1"), WithRole("primary")), Username: username, Password: password}
	n2Opts := ConnectionOptions{Matcher: And(WithNode("n2"), WithRole("primary")), Username: username, Password: password}
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE items (id INT PRIMARY KEY, data TEXT);")
		require.NoError(t, err)
	})
	db.WaitForReplication(ctx, t, username, password)

	t.Log("leaving the database idle")
	time.Sleep(90 * time.Second)

	t.Log("adding a replica to n1")
	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: spec(hostIDs[0], hostIDs[2])}))
	replica := db.GetInstance(And(WithNode("n1"), WithRole("replica")))
	require.NotNil(t, replica, "n1 should have a replica")
	assert.Equal(t, "available", replica.State)

	t.Log("writing on n1 so that its subscriber confirms new changes")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 1)

	t.Log("waiting for the replica to hold a permanent copy of n1's slot")
	require.True(t, waitFor(func() bool {
		stale, synced, ok := spockSlotCopies(ctx, db, replica, username, password)
		return ok && stale == 0 && synced == 1
	}, 3*time.Minute), "the replica should hold a permanent synced copy of n1's spock slot")

	t.Log("switching n1 over to the replica")
	original := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
	require.NotNil(t, original)
	require.NoError(t, db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
		DatabaseID:          controlplane.Identifier(db.ID),
		NodeName:            "n1",
		CandidateInstanceID: pointerTo(replica.ID),
	}))
	require.True(t, waitFor(func() bool {
		require.NoError(t, db.Refresh(ctx))
		primary := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
		return primary != nil && primary.ID == replica.ID
	}, 2*time.Minute), "the replica should become n1's primary")

	t.Log("checking replication both ways after the switchover")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 2)
	insertAndCheck(ctx, t, db, n2Opts, n1Opts, 3)

	// The former primary keeps its own slot when it rejoins as a replica.
	// The instance monitor drops it so native slot sync can copy the new
	// primary's slot.
	t.Log("checking that the former primary gets a synced copy")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 4)
	require.True(t, waitFor(func() bool {
		stale, synced, ok := spockSlotCopies(ctx, db, original, username, password)
		return ok && stale == 0 && synced == 1
	}, 3*time.Minute), "the former primary should hold only a synced copy of the new primary's slot")

	t.Log("failing n1 over to the former primary")
	require.NoError(t, db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
		DatabaseID:          controlplane.Identifier(db.ID),
		NodeName:            "n1",
		CandidateInstanceID: pointerTo(original.ID),
		SkipValidation:      true,
	}))
	require.True(t, waitFor(func() bool {
		require.NoError(t, db.Refresh(ctx))
		primary := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
		return primary != nil && primary.ID == original.ID
	}, 2*time.Minute), "the former primary should become n1's primary again")

	t.Log("checking replication both ways after the failover")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 5)
	insertAndCheck(ctx, t, db, n2Opts, n1Opts, 6)

	t.Log("checking that the replica that was failed over from gets a synced copy")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 7)
	require.True(t, waitFor(func() bool {
		stale, synced, ok := spockSlotCopies(ctx, db, replica, username, password)
		return ok && stale == 0 && synced == 1
	}, 3*time.Minute), "the replica should hold only a synced copy of the new primary's slot after the failover")
}

// TestSpock6Beta1DatabaseUpdate creates a database on a Spock 6.0.0-beta1
// image, checks that it can still be updated, then moves it to beta2. The
// update must bring each node's beta1 catalog up to date before Spock runs on
// the beta2 binaries.
func TestSpock6Beta1DatabaseUpdate(t *testing.T) {
	t.Parallel()
	fixture.SkipIfSpockNotInstalled(t)
	fixture.SkipIfUpgradesUnsupported(t)

	const (
		username = "admin"
		password = "password"
		dbName   = "spock6_beta1_db"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()
	spec := func(postgresVersion string, memory *string) *controlplane.DatabaseSpec {
		return &controlplane.DatabaseSpec{
			DatabaseName:    dbName,
			PostgresVersion: pointerTo(postgresVersion),
			SpockVersion:    pointerTo("6"),
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{{
				Username:   username,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			}},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[0])}, Memory: memory},
				{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[1])}, Memory: memory},
			},
		}
	}
	n1Opts := ConnectionOptions{Matcher: And(WithNode("n1"), WithRole("primary")), Username: username, Password: password}
	n2Opts := ConnectionOptions{Matcher: And(WithNode("n2"), WithRole("primary")), Username: username, Password: password}
	t.Log("creating a two-node database on spock 6.0.0-beta1")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{Spec: spec("18.6", nil)})
	reservedObjectExists := func(opts ConnectionOptions) bool {
		var exists bool
		db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
			require.NoError(t, conn.QueryRow(ctx, "SELECT to_regclass('spock.reserved_object') IS NOT NULL;").Scan(&exists))
		})
		return exists
	}
	assert.False(t, reservedObjectExists(n1Opts), "a beta1 catalog doesn't have spock.reserved_object")
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE items (id INT PRIMARY KEY, data TEXT);")
		require.NoError(t, err)
	})
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 1)

	t.Log("updating the beta1 database without moving it")
	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: spec("18.6", pointerTo("1GiB"))}))
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 2)

	t.Log("moving the database to beta2")
	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: spec("18.7", pointerTo("1GiB"))}))

	assert.True(t, reservedObjectExists(n1Opts), "the update should bring n1's catalog up to date")
	assert.True(t, reservedObjectExists(n2Opts), "the update should bring n2's catalog up to date")

	t.Log("checking replication both ways on beta2")
	insertAndCheck(ctx, t, db, n1Opts, n2Opts, 3)
	insertAndCheck(ctx, t, db, n2Opts, n1Opts, 4)
}

// insertAndCheck inserts a row with the given id on the source node and
// waits for it to reach the target node.
func insertAndCheck(ctx context.Context, t *testing.T, db *DatabaseFixture, source, target ConnectionOptions, id int) {
	t.Helper()
	data := fmt.Sprintf("row-%d", id)
	db.WithConnection(ctx, source, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items VALUES ($1, $2);", id, data)
		require.NoError(t, err)
	})
	db.WaitForReplication(ctx, t, "admin", "password")
	db.WithConnection(ctx, target, t, func(conn *pgx.Conn) {
		var got string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = $1;", id).Scan(&got))
		assert.Equal(t, data, got)
	})
}

// spockSlotCopies counts the Spock slots on a standby that native slot sync
// didn't make (stale) and the permanent, usable copies that it did (synced).
// ok is false while the instance can't be queried or isn't a standby.
func spockSlotCopies(
	ctx context.Context,
	db *DatabaseFixture,
	instance *controlplane.Instance,
	username, password string,
) (stale, synced int, ok bool) {
	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{Instance: instance, Username: username, Password: password})
	if err != nil {
		return 0, 0, false
	}
	defer conn.Close(ctx)
	var inRecovery bool
	err = conn.QueryRow(ctx, `
		SELECT pg_is_in_recovery(),
		       count(*) FILTER (WHERE NOT synced),
		       count(*) FILTER (WHERE synced AND NOT temporary AND invalidation_reason IS NULL)
		FROM pg_replication_slots
		WHERE plugin = 'spock_output';`,
	).Scan(&inRecovery, &stale, &synced)
	return stale, synced, err == nil && inRecovery
}
