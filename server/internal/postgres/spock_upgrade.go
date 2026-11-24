package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PauseDDLReplication turns Spock's automatic DDL replication off on this
// instance until ResumeDDLReplication is run. It uses ALTER SYSTEM, which
// overrides the value Patroni writes to postgresql.conf, works on standbys,
// and persists across restarts and switchovers. The setting takes effect on
// reload, including for existing sessions.
func PauseDDLReplication() Statements {
	return Statements{
		Statement{SQL: "ALTER SYSTEM SET spock.enable_ddl_replication = off;"},
		Statement{SQL: "SELECT pg_reload_conf();"},
	}
}

// ResumeDDLReplication removes the override set by PauseDDLReplication, so
// the value from postgresql.conf applies again.
func ResumeDDLReplication() Statements {
	return Statements{
		Statement{SQL: "ALTER SYSTEM RESET spock.enable_ddl_replication;"},
		Statement{SQL: "SELECT pg_reload_conf();"},
	}
}

// DDLReplicationPaused reports whether PauseDDLReplication's override is in
// place on this instance. The override outlives the workflow that set it, so
// it marks a Spock major upgrade that hasn't finished.
func DDLReplicationPaused() Query[bool] {
	return Query[bool]{
		SQL: `SELECT EXISTS (
				SELECT 1 FROM pg_file_settings
				WHERE name = 'spock.enable_ddl_replication'
				  AND sourcefile LIKE '%postgresql.auto.conf'
				  AND applied
			);`,
	}
}

// ServerVersionNum returns the server's version number, e.g. 170012.
func ServerVersionNum() Query[int] {
	return Query[int]{
		SQL: "SELECT current_setting('server_version_num')::int;",
	}
}

type ExtensionVersions struct {
	// Installed is nil when the extension isn't installed in this database.
	Installed *string `db:"installed_version"`
	// Default is the version the installed binaries provide.
	Default string `db:"default_version"`
}

// SpockExtensionVersions returns the installed Spock extension version in the
// current database and the version the installed Spock binaries provide.
// They differ after the binaries are upgraded until the extension update
// runs.
func SpockExtensionVersions() Query[ExtensionVersions] {
	return Query[ExtensionVersions]{
		SQL: "SELECT installed_version, default_version FROM pg_available_extensions WHERE name = 'spock';",
	}
}

// UpdateSpockExtension updates the Spock extension in the current database to
// the version the installed binaries provide. Spock's manager worker normally
// does this itself when the server starts on new binaries.
func UpdateSpockExtension() Statement {
	return Statement{SQL: "ALTER EXTENSION spock UPDATE;"}
}

type SlotActivity struct {
	Name      string `db:"slot_name"`
	ActivePID *int32 `db:"active_pid"`
}

// SpockSlotsWithoutFailover lists this primary's Spock slots that don't have
// the failover flag. PostgreSQL 17+ native slot sync only copies slots that
// have it. Slots created by Spock before 6.0.0, and by Control Plane for
// Spock 5 databases, don't. Requires PostgreSQL 17 or later.
func SpockSlotsWithoutFailover() Query[SlotActivity] {
	return Query[SlotActivity]{
		SQL: `SELECT slot_name, active_pid
			FROM pg_replication_slots
			WHERE plugin = 'spock_output'
			  AND slot_type = 'logical'
			  AND NOT temporary
			  AND NOT synced
			  AND NOT failover
			ORDER BY slot_name;`,
	}
}

// TerminateSlotWalsender ends the walsender holding the given slot, if any,
// so the slot can be altered. The subscriber's apply worker reconnects after
// its restart delay and resumes from the slot's position.
func TerminateSlotWalsender(slotName string) Statement {
	return Statement{
		SQL: `SELECT pg_terminate_backend(active_pid)
			FROM pg_replication_slots
			WHERE slot_name = @slot_name AND active_pid IS NOT NULL;`,
		Args: pgx.NamedArgs{"slot_name": slotName},
	}
}

// EnableSlotFailoverCommand returns the replication protocol command that
// sets the failover flag on a logical slot. It must be sent with the simple
// query protocol over a replication connection, and the slot must not be in
// use. Requires PostgreSQL 17 or later. Unlike spock.slot_enable_failover(),
// which only exists from Spock 6.0.0, this also works on a Spock 5 primary.
func EnableSlotFailoverCommand(slotName string) string {
	return fmt.Sprintf("ALTER_REPLICATION_SLOT %s ( FAILOVER true )", QuoteIdentifier(slotName))
}

// StaleSpockSlotCopies lists Spock slots on a standby that native slot sync
// didn't create: copies made by Spock's own failover-slots worker, or a
// former primary's own slots after a switchover. Native slot sync won't take
// over a slot with the same name, and Patroni leaves Spock slots alone
// because of ignore_slots, so these must be dropped before native slot sync
// can keep the standby's copies current. Returns nothing on a primary.
// Requires PostgreSQL 17 or later.
func StaleSpockSlotCopies() Query[SlotActivity] {
	return Query[SlotActivity]{
		SQL: `SELECT slot_name, active_pid
			FROM pg_replication_slots
			WHERE plugin = 'spock_output'
			  AND slot_type = 'logical'
			  AND NOT temporary
			  AND NOT synced
			  AND pg_is_in_recovery()
			ORDER BY slot_name;`,
	}
}

// DropStaleSpockSlotCopy drops the given slot copy, but only while the
// instance is still a standby. Checking in the same statement means a slot is
// never dropped from an instance that was promoted after the copies were
// listed.
func DropStaleSpockSlotCopy(slotName string) Statement {
	return Statement{
		SQL:  "SELECT pg_drop_replication_slot(@slot_name) WHERE pg_is_in_recovery();",
		Args: pgx.NamedArgs{"slot_name": slotName},
	}
}

// IsReplicationSlotSynced checks whether native slot sync has made a
// permanent, usable copy of the given subscription's slot on this standby. An
// invalidated copy can't be used after a promotion, so it doesn't count.
// Requires PostgreSQL 17 or later.
func IsReplicationSlotSynced(databaseName, providerNode, subscriberNode string) Query[bool] {
	return Query[bool]{
		SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s AND synced AND NOT temporary AND invalidation_reason IS NULL);", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

type SubscriptionStatusRow struct {
	Name   string `db:"subscription_name"`
	Status string `db:"status"`
}

// SubscriptionStatuses returns the status of every Spock subscription on this
// node.
func SubscriptionStatuses() Query[SubscriptionStatusRow] {
	return Query[SubscriptionStatusRow]{
		SQL: "SELECT subscription_name, status FROM spock.sub_show_status() ORDER BY subscription_name;",
	}
}
