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
)

// TestRollingUpdatePreservesReplication verifies that Spock subscriptions
// remain functional after a rolling update on a multi-node database with
// replica instances (PLAT-665). It writes data before and after the update
// and asserts that both directions of replication still work.
func TestRollingUpdatePreservesReplication(t *testing.T) {
	t.Parallel()

	fixture.SkipIfUpgradesUnsupported(t)

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]
	host4 := fixture.HostIDs()[3]

	username := "admin"
	password := "password"

	fromVersion := "18.3"
	toVersion := "18.4"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Log("creating two-node database with replicas")

	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "repl_test",
			PostgresVersion: &fromVersion,
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{
					controlplane.Identifier(host1),
					controlplane.Identifier(host2),
				}},
				{Name: "n2", HostIds: []controlplane.Identifier{
					controlplane.Identifier(host3),
					controlplane.Identifier(host4),
				}},
			},
		},
	})

	n1Opts := ConnectionOptions{Matcher: And(WithNode("n1"), WithRole("primary")), Username: username, Password: password}
	n2Opts := ConnectionOptions{Matcher: And(WithNode("n2"), WithRole("primary")), Username: username, Password: password}

	// Write a row on n1 and wait for it to replicate to n2 before the update.
	t.Log("writing pre-update data to n1")

	var preLSN string
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE items (id INT PRIMARY KEY, data TEXT);")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO items VALUES (1, 'before-update');")
		require.NoError(t, err)

		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.sync_event();").Scan(&preLSN))
	})

	t.Log("waiting for pre-update row to replicate to n2")

	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		var synced bool
		require.NoError(t, conn.QueryRow(ctx,
			"CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 30);", "n1", preLSN,
		).Scan(&synced))
		require.True(t, synced, "pre-update row did not replicate to n2 before update")

		var data string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = 1;").Scan(&data))
		assert.Equal(t, "before-update", data)
	})

	// Perform the rolling update — version bump changes the image, which
	// triggers a container restart on each instance.
	t.Logf("performing rolling update from %s to %s", fromVersion, toVersion)

	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "repl_test",
			PostgresVersion: &toVersion,
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{
					controlplane.Identifier(host1),
					controlplane.Identifier(host2),
				}},
				{Name: "n2", HostIds: []controlplane.Identifier{
					controlplane.Identifier(host3),
					controlplane.Identifier(host4),
				}},
			},
		},
	})
	require.NoError(t, err)

	t.Log("asserting both nodes have an active primary after update")
	require.NotNil(t, db.GetInstance(And(WithNode("n1"), WithRole("primary"))))
	require.NotNil(t, db.GetInstance(And(WithNode("n2"), WithRole("primary"))))

	// Write a new row on n1 after the update and verify it reaches n2.
	// This is the core regression check: if the second switchover broke the
	// n1→n2 subscription, this insert will never replicate.
	t.Log("writing post-update data to n1, verifying replication to n2")

	var postLSN string
	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items VALUES (2, 'after-update');")
		require.NoError(t, err)

		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.sync_event();").Scan(&postLSN))
	})

	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		var synced bool
		require.NoError(t, conn.QueryRow(ctx,
			"CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 30);", "n1", postLSN,
		).Scan(&synced))
		require.True(t, synced, "post-update row did not replicate n1→n2")

		var data string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = 2;").Scan(&data))
		assert.Equal(t, "after-update", data)
	})

	// Also verify the reverse direction: n2→n1.
	t.Log("writing post-update data to n2, verifying replication to n1")

	var reverseLSN string
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "INSERT INTO items VALUES (3, 'after-update-n2');")
		require.NoError(t, err)

		require.NoError(t, conn.QueryRow(ctx, "SELECT spock.sync_event();").Scan(&reverseLSN))
	})

	db.WithConnection(ctx, n1Opts, t, func(conn *pgx.Conn) {
		var synced bool
		require.NoError(t, conn.QueryRow(ctx,
			"CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 30);", "n2", reverseLSN,
		).Scan(&synced))
		require.True(t, synced, "post-update row did not replicate n2→n1")

		var data string
		require.NoError(t, conn.QueryRow(ctx, "SELECT data FROM items WHERE id = 3;").Scan(&data))
		assert.Equal(t, "after-update-n2", data)
	})
}

func TestMinorVersionUpgrade(t *testing.T) {
	t.Parallel()

	fixture.SkipIfUpgradesUnsupported(t)

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	host3 := fixture.HostIDs()[2]
	host4 := fixture.HostIDs()[3]

	username := "admin"
	password := "password"

	fromVersion := "18.3"
	toVersion := "18.4"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Logf("creating database with initial version %s", fromVersion)

	db := fixture.NewDatabaseFixture(t.Context(), t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "test",
			PostgresVersion: &fromVersion,
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
					},
				},
				{
					Name: "n2", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host3),
						controlplane.Identifier(host4),
					},
				},
			},
		},
	})

	t.Log("asserting that the primary is running on the first host")

	// Assert that the primary is running on the first host in the host_ids list.
	primary := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
	require.NotNil(t, primary)
	assert.Equal(t, host1, primary.HostID)

	opts := ConnectionOptions{
		InstanceID: primary.ID,
		Username:   username,
		Password:   password,
	}

	t.Logf("validating that the initial version is %s", fromVersion)

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var current string

		rows := conn.QueryRow(ctx, "SHOW server_version;")
		require.NoError(t, rows.Scan(&current))

		assert.Equal(t, fromVersion, current)
	})

	t.Logf("updating version to %s", toVersion)

	// Bump the minor version
	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName:    "test",
			PostgresVersion: &toVersion,
			Port:            pointerTo(0),
			PatroniPort:     pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host1),
						controlplane.Identifier(host2),
					},
				},
				{
					Name: "n2", HostIds: []controlplane.Identifier{
						controlplane.Identifier(host3),
						controlplane.Identifier(host4),
					},
				},
			},
		},
	})
	require.NoError(t, err)

	t.Log("asserting that both nodes have an active primary after update")

	// The primary may have moved to a different host during the rolling update.
	// Rolling updates no longer force a switchover back to the original primary
	// (PLAT-665), so we only assert that each node has some primary.
	updatedN1 := db.GetInstance(And(WithNode("n1"), WithRole("primary")))
	require.NotNil(t, updatedN1)
	updatedN2 := db.GetInstance(And(WithNode("n2"), WithRole("primary")))
	require.NotNil(t, updatedN2)

	// Use the current n1 primary for the version check.
	opts = ConnectionOptions{
		InstanceID: updatedN1.ID,
		Username:   username,
		Password:   password,
	}

	t.Logf("validating that the version is updated to %s", toVersion)

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var current string

		rows := conn.QueryRow(ctx, "SHOW server_version;")
		require.NoError(t, rows.Scan(&current))

		assert.Equal(t, toVersion, current)
	})
}
