//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddNodeOriginAdvanced verifies that after adding a node the replication
// origin on the new subscriber has been advanced past 0/0. A zeroed origin
// causes the apply worker to start from the beginning of WAL, producing
// duplicate-key errors or silently overwriting rows.
//
// Covers: ReplicationOriginAdvanceResource which ensures the replication
// origin exists and is advanced to the same LSN as the replication slot.
// This resource depends on ReplicationSlotAdvanceFromCTSResource which runs
// on the provider's host, while origin advancement runs on the subscriber's
// host (since cross-host connections are not allowed).
func TestAddNodeOriginAdvanced(t *testing.T) {
	t.Parallel()

	const (
		username = "admin"
		password = "password"
		dbName   = "origin_adv_db"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	hostIDs := fixture.HostIDs()
	t.Log("Step 1: Creating 2-node database fixture")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: dbName,
			Port:         pointerTo(0),
			PatroniPort:  pointerTo(0),
			DatabaseUsers: []*controlplane.DatabaseUserSpec{{
				Username:   username,
				Password:   pointerTo(password),
				DbOwner:    pointerTo(true),
				Attributes: []string{"LOGIN", "SUPERUSER"},
			}},
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[0])}},
				{Name: "n2", HostIds: []controlplane.Identifier{controlplane.Identifier(hostIDs[1])}},
			},
		},
	})
	t.Logf("Database created: %s", db.ID)

	// Write rows on n2 so its WAL position is meaningfully ahead of the slot's
	// consistent_point. This gives the origin advancement a non-trivial LSN.
	t.Log("Step 2: Writing 100 test rows to n2")
	n2Opts := ConnectionOptions{
		Matcher:  And(WithNode("n2"), WithRole("primary")),
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, `CREATE TABLE origin_probe (id INT PRIMARY KEY, v TEXT)`)
		require.NoError(t, err)

		for i := 1; i <= 100; i++ {
			_, err = conn.Exec(ctx, `INSERT INTO origin_probe VALUES ($1, $2)`, i, fmt.Sprintf("r%d", i))
			require.NoError(t, err)
		}
	})
	t.Log("Test data written successfully")

	// Add n3 with n1 as source.
	t.Log("Step 3: Adding n3 node with n1 as source")
	db.Spec.Nodes = append(db.Spec.Nodes, &controlplane.DatabaseNodeSpec{
		Name:       "n3",
		HostIds:    []controlplane.Identifier{controlplane.Identifier(hostIDs[2])},
		SourceNode: pointerTo("n1"),
	})
	t.Log("Starting database update to add n3 (this may take several minutes)")
	require.NoError(t, db.Update(ctx, UpdateOptions{Spec: db.Spec}))
	t.Log("Database update completed, n3 node added successfully")

	t.Log("Step 3b: Waiting for replication to complete across all nodes")
	db.WaitForReplication(ctx, t, username, password)
	t.Log("Replication complete")

	// The replication slot spk_<db>_n2_sub_n2_n3 lives on n2.
	// The origin with the same name lives on n3 (subscriber side).
	slotName := e2eReplicationSlotName(dbName, "n2", "n3")
	t.Logf("Step 4: Checking replication origin on n3 (slot name: %s)", slotName)

	n3Opts := ConnectionOptions{
		Matcher:  And(WithNode("n3"), WithRole("primary")),
		Username: username,
		Password: password,
	}
	db.WithConnection(ctx, n3Opts, t, func(conn *pgx.Conn) {
		// Query progress; COALESCE returns '0/0' when the origin is absent or
		// has never been advanced, so a single assert covers both failure modes.
		var lsn string
		err := conn.QueryRow(ctx, `
			SELECT COALESCE(
				(SELECT pg_replication_origin_progress($1, false)::text
				 FROM pg_replication_origin WHERE roname = $1),
				'0/0'
			)`, slotName,
		).Scan(&lsn)
		require.NoError(t, err)
		t.Logf("Replication origin LSN: %s (expected: not 0/0)", lsn)

		assert.NotEqual(t, "0/0", lsn,
			"replication origin %q on n3 should be advanced past 0/0 (got %s); "+
				"a zeroed origin risks the apply worker replaying historical WAL",
			slotName, lsn)
	})
	t.Log("Test completed successfully")
}

// e2eReplicationSlotName mirrors postgres.ReplicationSlotName without
// importing the server package from the e2e test binary.
// Format: spk_<db>_<provider>_sub_<provider>_<subscriber>
func e2eReplicationSlotName(databaseName, providerNode, subscriberNode string) string {
	return fmt.Sprintf("spk_%s_%s_sub_%s_%s",
		databaseName, providerNode, providerNode, subscriberNode)
}
