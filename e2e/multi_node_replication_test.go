//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// TestMultiNodeReplicationHealth creates a two-node Spock database and verifies that
// replication slots and Spock subscriptions on all nodes are healthy across a range of
// topology-change scenarios.
//
// With a real Spock multi-master topology the assertions in replication_health_test.go
// are no longer trivial: Spock logical slots and cross-node subscriptions must
// reconnect to the new primary within the polling window.
//
// Host layout (requires ≥ 4 hosts):
//
//	n1 — first half of fixture hosts (primary + replicas, Patroni-managed HA)
//	n2 — second half of fixture hosts
func TestMultiNodeReplicationHealth(t *testing.T) {
	t.Parallel()

	hosts := fixture.HostIDs()
	require.GreaterOrEqualf(t, len(hosts), 4,
		"multi-node replication test needs at least 4 hosts; got %d", len(hosts))

	// 25 min budget: 6-instance creation (~4 min) + baseline + 6 sequential
	// sub-tests each involving topology changes and replication health polling.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	mid := len(hosts) / 2
	n1Hosts := hosts[:mid]
	n2Hosts := hosts[mid:]

	toIDs := func(ss []string) []controlplane.Identifier {
		out := make([]controlplane.Identifier, len(ss))
		for i, s := range ss {
			out[i] = controlplane.Identifier(s)
		}
		return out
	}

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_multi_node_repl",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   username,
					Password:   pointerTo(password),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{Name: "n1", HostIds: toIDs(n1Hosts)},
				{Name: "n2", HostIds: toIDs(n2Hosts)},
			},
		},
	})

	dbID := controlplane.Identifier(db.ID)

	// Wait for all instances to leave creating/modifying state before any checks.
	waitFor(func() bool {
		_ = db.Refresh(ctx)
		for _, inst := range db.Instances {
			if inst.State == "modifying" || inst.State == "creating" {
				return false
			}
		}
		return true
	}, 90*time.Second)

	// --- shared helpers ---

	getPrimary := func(nodeName string) string {
		inst := db.GetInstance(And(WithNode(nodeName), WithRole("primary")))
		if inst == nil {
			return ""
		}
		return inst.ID
	}

	waitForPrimaryChange := func(nodeName, orig string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			_ = db.Refresh(ctx)
			if p := getPrimary(nodeName); p != "" && p != orig {
				return true
			}
			time.Sleep(1 * time.Second)
		}
		return false
	}

	waitForPrimaryIs := func(nodeName, expected string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			_ = db.Refresh(ctx)
			if p := getPrimary(nodeName); p == expected {
				return true
			}
			time.Sleep(1 * time.Second)
		}
		return false
	}

	waitForReadyReplica := func(nodeName, curPrimary string, timeout time.Duration) string {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			_ = db.Refresh(ctx)
			for _, inst := range db.Instances {
				if inst.NodeName != nodeName {
					continue
				}
				role := ""
				if inst.Postgres != nil && inst.Postgres.Role != nil {
					role = *inst.Postgres.Role
				}
				st := inst.State
				if inst.ID != curPrimary && role != "primary" &&
					(st == "available" || st == "ready" || st == "running") {
					return inst.ID
				}
			}
			time.Sleep(1 * time.Second)
		}
		return ""
	}

	// waitForInstanceRole polls until the given instance reports the expected
	// Patroni role (e.g. "replica") and is in a stable operational state.
	waitForInstanceRole := func(instanceID, role string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			_ = db.Refresh(ctx)
			for _, inst := range db.Instances {
				if inst.ID != instanceID {
					continue
				}
				instRole := ""
				if inst.Postgres != nil && inst.Postgres.Role != nil {
					instRole = *inst.Postgres.Role
				}
				st := inst.State
				t.Logf("[instance-role] %s: role=%s state=%s (want role=%s)", instanceID, instRole, st, role)
				if instRole == role && (st == "available" || st == "ready" || st == "running") {
					return true
				}
			}
			time.Sleep(2 * time.Second)
		}
		return false
	}

	// --- baseline ---

	t.Log("checking baseline replication health before any topology change")
	db.WaitForReplication(ctx, t, username, password)
	assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)

	// --- 1. switchover n1 ---

	t.Run("switchover n1 preserves replication", func(t *testing.T) {
		origPrimary := getPrimary("n1")
		require.NotEmpty(t, origPrimary, "n1 has no primary instance")
		t.Logf("[switchover-n1] primary before: %s", origPrimary)

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n1",
		})
		require.NoError(t, err, "switchover API call failed")

		require.Truef(t, waitForPrimaryChange("n1", origPrimary, 60*time.Second),
			"n1 primary did not change within timeout (still %s)", origPrimary)
		t.Logf("[switchover-n1] primary after: %s", getPrimary("n1"))

		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)
		db.WaitForReplication(ctx, t, username, password)
	})

	// --- 2. failover n1 ---

	t.Run("failover n1 preserves replication", func(t *testing.T) {
		origPrimary := getPrimary("n1")
		require.NotEmpty(t, origPrimary, "n1 has no primary instance")

		candidate := waitForReadyReplica("n1", origPrimary, 60*time.Second)
		require.NotEmpty(t, candidate, "no ready replica on n1 available for failover")
		t.Logf("[failover-n1] primary before: %s, candidate: %s", origPrimary, candidate)

		skipTrue := true
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &candidate,
			SkipValidation:      skipTrue,
		})
		require.NoError(t, err, "failover API call failed")

		require.Truef(t, waitForPrimaryIs("n1", candidate, 75*time.Second),
			"n1 primary did not become %s within timeout (current %s)",
			candidate, getPrimary("n1"))
		t.Logf("[failover-n1] primary after: %s", getPrimary("n1"))

		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)
		db.WaitForReplication(ctx, t, username, password)
	})

	// --- 3. old primary rejoins as replica ---

	// Trigger a fresh failover and explicitly verify that the demoted primary
	// rejoins the cluster as a streaming replica. Replication slots on the new
	// primary must become active — which only happens when the demoted instance
	// reconnects — so assertAllNodesReplicationHealthy implicitly covers this,
	// but we also assert the API-visible role transition.
	t.Run("old primary rejoins as replica after failover", func(t *testing.T) {
		// After multiple failovers in previous sub-tests, give the cluster time
		// to settle before requiring a ready replica.
		demotedPrimary := getPrimary("n1")
		require.NotEmpty(t, demotedPrimary, "n1 has no primary instance")

		candidate := waitForReadyReplica("n1", demotedPrimary, 90*time.Second)
		require.NotEmpty(t, candidate, "no ready replica on n1 for rejoin test")
		t.Logf("[rejoin] demoting %s, promoting %s", demotedPrimary, candidate)

		skipTrue := true
		err := db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
			DatabaseID:          dbID,
			NodeName:            "n1",
			CandidateInstanceID: &candidate,
			SkipValidation:      skipTrue,
		})
		require.NoError(t, err, "failover API call failed")

		require.Truef(t, waitForPrimaryIs("n1", candidate, 75*time.Second),
			"n1 primary did not become %s within timeout", candidate)

		// The demoted instance must re-join the cluster as a streaming replica.
		// Patroni will run pg_rewind (or pg_basebackup) and bring it back up.
		require.Truef(t, waitForInstanceRole(demotedPrimary, "replica", 2*time.Minute),
			"demoted instance %s did not rejoin as replica within timeout", demotedPrimary)
		t.Logf("[rejoin] instance %s is now a replica", demotedPrimary)

		// With the rejoin complete, all streaming slots (including the one for
		// the rejoined replica) must be active.
		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)
		db.WaitForReplication(ctx, t, username, password)
	})

	// --- 4. switchover n2 ---

	// Symmetric counterpart of sub-test 1: flip n2's primary and verify that
	// n1's Spock subscription to n2 (and n2's physical streaming slots) recover.
	t.Run("switchover n2 preserves replication", func(t *testing.T) {
		origPrimary := getPrimary("n2")
		require.NotEmpty(t, origPrimary, "n2 has no primary instance")
		t.Logf("[switchover-n2] primary before: %s", origPrimary)

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n2",
		})
		require.NoError(t, err, "switchover n2 API call failed")

		require.Truef(t, waitForPrimaryChange("n2", origPrimary, 60*time.Second),
			"n2 primary did not change within timeout (still %s)", origPrimary)
		t.Logf("[switchover-n2] primary after: %s", getPrimary("n2"))

		// n1's subscription to the new n2 primary must reach 'replicating'.
		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)
		db.WaitForReplication(ctx, t, username, password)
	})

	// --- 5. data integrity across switchover ---

	// Write a known row to n1 before a switchover, sync to n2, perform the
	// switchover, write another row on the new primary, then verify both rows
	// are present on all nodes. This confirms no committed writes are lost
	// during a topology change.
	t.Run("data integrity across switchover", func(t *testing.T) {
		connOpts := func(nodeName string) ConnectionOptions {
			return ConnectionOptions{
				Matcher:  And(WithNode(nodeName), WithRole("primary")),
				Username: username,
				Password: password,
			}
		}

		// Seed table on n1 (ON CONFLICT so the test is re-runnable).
		db.WithConnection(ctx, connOpts("n1"), t, func(conn *pgx.Conn) {
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS repl_integrity (
					id   INT PRIMARY KEY,
					phase TEXT NOT NULL,
					node  TEXT NOT NULL
				)`)
			require.NoError(t, err)
			_, err = conn.Exec(ctx,
				`INSERT INTO repl_integrity VALUES ($1,$2,$3)
				 ON CONFLICT (id) DO UPDATE SET phase=$2, node=$3`,
				1, "pre_switchover", "n1")
			require.NoError(t, err)
		})

		// Confirm n2 received the row before the topology change.
		db.WaitForReplication(ctx, t, username, password)

		origPrimary := getPrimary("n1")
		require.NotEmpty(t, origPrimary, "n1 has no primary instance")

		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n1",
		})
		require.NoError(t, err, "switchover API call failed")

		require.Truef(t, waitForPrimaryChange("n1", origPrimary, 60*time.Second),
			"n1 primary did not change within timeout")

		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)

		// Write a second row on the new n1 primary.
		db.WithConnection(ctx, connOpts("n1"), t, func(conn *pgx.Conn) {
			_, err := conn.Exec(ctx,
				`INSERT INTO repl_integrity VALUES ($1,$2,$3)
				 ON CONFLICT (id) DO UPDATE SET phase=$2, node=$3`,
				2, "post_switchover", "n1")
			require.NoError(t, err)
		})

		db.WaitForReplication(ctx, t, username, password)

		// Both rows must be present on every node's primary.
		for _, node := range db.Spec.Nodes {
			node := node
			db.WithConnection(ctx, connOpts(node.Name), t, func(conn *pgx.Conn) {
				var count int
				row := conn.QueryRow(ctx, `SELECT COUNT(*) FROM repl_integrity`)
				require.NoError(t, row.Scan(&count))
				require.Equalf(t, 2, count,
					"expected 2 rows on node %s after switchover", node.Name)
			})
		}
	})

	// --- 6. back-to-back topology changes ---

	// Switchover n1 then immediately switchover n2 without waiting for full
	// replication recovery between them. This verifies the cluster tolerates
	// overlapping topology changes and that all subscriptions eventually
	// re-establish when both primary elections settle.
	t.Run("back-to-back topology changes", func(t *testing.T) {
		origN1 := getPrimary("n1")
		origN2 := getPrimary("n2")
		require.NotEmpty(t, origN1, "n1 has no primary instance")
		require.NotEmpty(t, origN2, "n2 has no primary instance")

		// Switchover n1 — wait only for primary role to transfer, not for
		// replication to fully recover.
		err := db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n1",
		})
		require.NoError(t, err, "switchover n1 API call failed")
		require.Truef(t, waitForPrimaryChange("n1", origN1, 60*time.Second),
			"n1 primary did not change within timeout")
		t.Logf("[back-to-back] n1 new primary: %s", getPrimary("n1"))

		// Immediately switchover n2 before Spock subscriptions from n1's
		// switchover have necessarily finished reconnecting.
		err = db.SwitchoverDatabaseNode(ctx, &controlplane.SwitchoverDatabaseNodePayload{
			DatabaseID: dbID,
			NodeName:   "n2",
		})
		require.NoError(t, err, "switchover n2 API call failed")
		require.Truef(t, waitForPrimaryChange("n2", origN2, 60*time.Second),
			"n2 primary did not change within timeout")
		t.Logf("[back-to-back] n2 new primary: %s", getPrimary("n2"))

		// After both elections settle, all slots and subscriptions must recover.
		assertAllNodesReplicationHealthy(ctx, t, db, username, password, replicationHealthTimeout)
		db.WaitForReplication(ctx, t, username, password)
	})
}
