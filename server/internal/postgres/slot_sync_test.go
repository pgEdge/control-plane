package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestIsReplicationSlotSynced(t *testing.T) {
	sql := postgres.IsReplicationSlotSynced("app", "n1", "n2").SQL
	// A temporary or invalidated copy can't be used after a promotion.
	assert.Contains(t, sql, "synced AND NOT temporary AND invalidation_reason IS NULL")
}

func TestStaleSpockSlotCopiesOnlyOnStreamingStandby(t *testing.T) {
	// A primary that Patroni restarted is briefly in recovery before it's
	// promoted, without a WAL receiver. Its slots must never be dropped.
	for _, sql := range []string{
		postgres.StaleSpockSlotCopies().SQL,
		postgres.DropStaleSpockSlotCopy("spk_app_n1_sub_n1_n2").SQL,
	} {
		assert.Contains(t, sql, "pg_is_in_recovery()")
		assert.Contains(t, sql, "FROM pg_stat_wal_receiver WHERE status = 'streaming'")
	}
	assert.Contains(t, postgres.StaleSpockSlotCopies().SQL, "NOT synced")
}

func TestDropStaleSpockSlotCopySkipsSyncedCopies(t *testing.T) {
	// A synced copy can replace a stale one between the listing and the
	// drop. The drop must leave it alone.
	stmt := postgres.DropStaleSpockSlotCopy("spk_app_n1_sub_n1_n2")
	assert.Contains(t, stmt.SQL, "WHERE slot_name = @slot_name")
	assert.Contains(t, stmt.SQL, "AND NOT synced")
	assert.Equal(t, "spk_app_n1_sub_n1_n2", stmt.Args["slot_name"])
}
