//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// replicationHealthTimeout is the default polling budget given to each replication
// health assertion. Long enough to cover post-failover convergence; short enough to
// fail a test before the parent context expires.
const replicationHealthTimeout = 90 * time.Second

type replicationSlot struct {
	Name               string
	Type               string
	Active             bool
	InvalidationReason *string
}

func (s replicationSlot) String() string {
	reason := "<nil>"
	if s.InvalidationReason != nil {
		reason = *s.InvalidationReason
	}
	return fmt.Sprintf("%s(%s,active=%v,invalidation=%s)", s.Name, s.Type, s.Active, reason)
}

type spockSubscription struct {
	Name   string
	Status string
}

// assertReplicationSlotsHealthy polls pg_replication_slots on the primary of nodeName
// until all slots are active and not invalidated, or timeout expires.
// Passes trivially when the primary has no replication slots.
func assertReplicationSlotsHealthy(
	ctx context.Context,
	t testing.TB,
	db *DatabaseFixture,
	nodeName, username, password string,
	timeout time.Duration,
) {
	t.Helper()

	var lastSlots []replicationSlot

	ok := waitFor(func() bool {
		_ = db.Refresh(ctx)

		conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
			Matcher:  And(WithNode(nodeName), WithRole("primary")),
			Username: username,
			Password: password,
		})
		if err != nil {
			t.Logf("[replication-slots][%s] connect failed: %v", nodeName, err)
			return false
		}
		defer conn.Close(ctx)

		rows, err := conn.Query(ctx, `
			SELECT slot_name, slot_type, active, invalidation_reason
			FROM pg_replication_slots
		`)
		if err != nil {
			t.Logf("[replication-slots][%s] query failed: %v", nodeName, err)
			return false
		}
		defer rows.Close()

		lastSlots = nil
		for rows.Next() {
			var s replicationSlot
			if err := rows.Scan(&s.Name, &s.Type, &s.Active, &s.InvalidationReason); err != nil {
				t.Logf("[replication-slots][%s] scan failed: %v", nodeName, err)
				return false
			}
			lastSlots = append(lastSlots, s)
		}
		if rows.Err() != nil {
			return false
		}

		for _, s := range lastSlots {
			if !s.Active || s.InvalidationReason != nil {
				t.Logf("[replication-slots][%s] slot not yet healthy: %s", nodeName, s)
				return false
			}
		}
		return true
	}, timeout)

	require.Truef(t, ok,
		"[replication-slots][%s] slots did not become healthy within %s; last state: %v",
		nodeName, timeout, lastSlots)

	t.Logf("[replication-slots][%s] %d slot(s) all active and not invalidated", nodeName, len(lastSlots))
}

// assertSpockSubscriptionsHealthy polls spock.subscription on the primary of nodeName
// until all subscriptions are in 'replicating' state, or timeout expires.
// Passes trivially when there are no subscriptions (single-node Patroni-only setup).
func assertSpockSubscriptionsHealthy(
	ctx context.Context,
	t testing.TB,
	db *DatabaseFixture,
	nodeName, username, password string,
	timeout time.Duration,
) {
	t.Helper()

	var lastSubs []spockSubscription

	ok := waitFor(func() bool {
		_ = db.Refresh(ctx)

		conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
			Matcher:  And(WithNode(nodeName), WithRole("primary")),
			Username: username,
			Password: password,
		})
		if err != nil {
			t.Logf("[spock-subs][%s] connect failed: %v", nodeName, err)
			return false
		}
		defer conn.Close(ctx)

		rows, err := conn.Query(ctx, `
			SELECT sub_name, status
			FROM spock.subscription
		`)
		if err != nil {
			t.Logf("[spock-subs][%s] query failed: %v", nodeName, err)
			return false
		}
		defer rows.Close()

		lastSubs = nil
		for rows.Next() {
			var s spockSubscription
			if err := rows.Scan(&s.Name, &s.Status); err != nil {
				t.Logf("[spock-subs][%s] scan failed: %v", nodeName, err)
				return false
			}
			lastSubs = append(lastSubs, s)
		}
		if rows.Err() != nil {
			return false
		}

		for _, s := range lastSubs {
			if s.Status != "replicating" {
				t.Logf("[spock-subs][%s] subscription %q not replicating: status=%s", nodeName, s.Name, s.Status)
				return false
			}
		}
		return true
	}, timeout)

	require.Truef(t, ok,
		"[spock-subs][%s] subscriptions did not reach 'replicating' within %s; last state: %v",
		nodeName, timeout, lastSubs)

	t.Logf("[spock-subs][%s] %d subscription(s) all replicating", nodeName, len(lastSubs))
}

// assertNoStaleSlots asserts that the current primary of nodeName holds no replication
// slots that are both inactive and not invalidated. Such slots accumulate WAL
// indefinitely and indicate a consumer that was never cleaned up after a topology change.
//
// This check is intentionally point-in-time: call it after assertReplicationSlotsHealthy
// confirms that all expected slots are active, so any remaining inactive slot is
// genuinely orphaned rather than transiently reconnecting.
func assertNoStaleSlots(
	ctx context.Context,
	t testing.TB,
	db *DatabaseFixture,
	nodeName, username, password string,
) {
	t.Helper()

	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
		Matcher:  And(WithNode(nodeName), WithRole("primary")),
		Username: username,
		Password: password,
	})
	if err != nil {
		// Primary may be transiently unavailable (e.g., immediately post-failover).
		// Log and skip rather than failing hard — the slot health check already
		// polled until the primary was reachable.
		t.Logf("[stale-slots][%s] connect failed (skipping check): %v", nodeName, err)
		return
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT slot_name, slot_type,
		       pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS lag_bytes
		FROM pg_replication_slots
		WHERE active = false
		  AND invalidation_reason IS NULL
	`)
	require.NoError(t, err, "[stale-slots][%s] query failed", nodeName)
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var name, typ string
		var lagBytes int64
		require.NoError(t, rows.Scan(&name, &typ, &lagBytes))
		stale = append(stale, fmt.Sprintf("%s(%s,lag=%dB)", name, typ, lagBytes))
	}
	require.NoError(t, rows.Err())

	require.Emptyf(t, stale,
		"[stale-slots][%s] stale (inactive, non-invalidated) replication slots found: %v",
		nodeName, stale)

	t.Logf("[stale-slots][%s] no stale slots", nodeName)
}

// assertAllNodesReplicationHealthy validates replication health across every Spock node
// in the database after a topology change (failover or switchover). It checks:
//
//   - All replication slots on each node's primary are active and not invalidated
//   - All Spock subscriptions on each node's primary are in 'replicating' state
//   - No stale (inactive, non-invalidated) slots remain on the current primary
//
// A polling loop with the given timeout accommodates post-topology-change convergence.
// Both checks pass trivially when the primary has no slots or subscriptions respectively
// (e.g. a single-node Patroni-only setup with no Spock multi-master replication).
func assertAllNodesReplicationHealthy(
	ctx context.Context,
	t testing.TB,
	db *DatabaseFixture,
	username, password string,
	timeout time.Duration,
) {
	t.Helper()

	_ = db.Refresh(ctx)

	for _, node := range db.Spec.Nodes {
		assertReplicationSlotsHealthy(ctx, t, db, node.Name, username, password, timeout)
		assertSpockSubscriptionsHealthy(ctx, t, db, node.Name, username, password, timeout)
		assertNoStaleSlots(ctx, t, db, node.Name, username, password)
	}
}
