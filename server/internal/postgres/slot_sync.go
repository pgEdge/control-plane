package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// These queries support PostgreSQL's native slot sync, which keeps copies of
// a primary's Spock slots on its standbys for Spock 6 on Postgres 17 or later.
// See NeedsNativeFailoverSlots.

// ServerVersionNum returns the server's version number, e.g. 170012.
func ServerVersionNum() Query[int] {
	return Query[int]{
		SQL: "SELECT current_setting('server_version_num')::int;",
	}
}

// NativeSlotSyncEnabled reports whether this server has native slot sync
// turned on. Requires PostgreSQL 17 or later.
func NativeSlotSyncEnabled() Query[bool] {
	return Query[bool]{
		SQL: "SELECT current_setting('sync_replication_slots') = 'on';",
	}
}

type SlotActivity struct {
	Name      string `db:"slot_name"`
	ActivePID *int32 `db:"active_pid"`
}

// streamingStandbyCondition is true only on a standby that's streaming from a
// primary. Patroni starts a primary in standby mode after a restart and
// promotes it a moment later, without a WAL receiver in between, and the
// primary's own slots must never be dropped in that window.
const streamingStandbyCondition = `pg_is_in_recovery()
			  AND EXISTS (SELECT 1 FROM pg_stat_wal_receiver WHERE status = 'streaming')`

// StaleSpockSlotCopies lists Spock slots on a standby that native slot sync
// didn't create: copies made by Spock's own failover-slots worker, or a
// former primary's own slots after a switchover or failover. Native slot sync
// won't take over a slot with the same name, and Patroni leaves Spock slots
// alone because of ignore_slots, so these must be dropped before native slot
// sync can keep the standby's copies current. Returns nothing unless the
// instance is a standby streaming from a primary. Requires PostgreSQL 17 or
// later.
func StaleSpockSlotCopies() Query[SlotActivity] {
	return Query[SlotActivity]{
		SQL: `SELECT slot_name, active_pid
			FROM pg_replication_slots
			WHERE plugin = 'spock_output'
			  AND slot_type = 'logical'
			  AND NOT temporary
			  AND NOT synced
			  AND ` + streamingStandbyCondition + `
			ORDER BY slot_name;`,
	}
}

// DropStaleSpockSlotCopy drops the given slot if it's still a copy that
// native slot sync didn't make, and the instance is still a standby streaming
// from a primary. Checking in the same statement as the drop keeps the
// window small, but can't close it: slot drops aren't transactional, and
// nothing can block a promotion between the check and the drop. A synced
// copy is never dropped, because the statement skips it, and PostgreSQL
// refuses to drop one on a standby.
func DropStaleSpockSlotCopy(slotName string) Statement {
	return Statement{
		SQL: `SELECT pg_drop_replication_slot(slot_name)
			FROM pg_replication_slots
			WHERE slot_name = @slot_name
			  AND NOT synced
			  AND NOT temporary
			  AND ` + streamingStandbyCondition + `;`,
		Args: pgx.NamedArgs{"slot_name": slotName},
	}
}

// IsSlotSynced reports whether the named slot exists and is a copy made by
// native slot sync. Requires PostgreSQL 17 or later.
func IsSlotSynced(slotName string) Query[bool] {
	return Query[bool]{
		SQL:  "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = @slot_name AND synced);",
		Args: pgx.NamedArgs{"slot_name": slotName},
	}
}

// IsReplicationSlotSynced checks whether native slot sync has made a
// permanent, usable copy of the given subscription's slot on this standby. A
// copy stays temporary until the subscriber confirms a position past the one
// the standby reserved for it, and a temporary or invalidated copy can't be
// used after a promotion, so neither counts. Requires PostgreSQL 17 or later.
func IsReplicationSlotSynced(databaseName, providerNode, subscriberNode string) Query[bool] {
	return Query[bool]{
		SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s AND synced AND NOT temporary AND invalidation_reason IS NULL);", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}
