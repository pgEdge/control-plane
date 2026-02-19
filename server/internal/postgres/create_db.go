package postgres

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReplicationSet struct {
	SetID       uint32
	SetNodeID   uint32
	SetName     string
	RepInsert   bool
	RepUpdate   bool
	RepDelete   bool
	RepTruncate bool
}

type ReplicationSetTable struct {
	SetID        uint32
	SetRelOID    uint32
	SetAttList   []string
	SetRowFilter string
}

func IsSpockEnabled() Query[bool] {
	return Query[bool]{
		SQL: "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'spock');",
	}
}

func EnableRepairMode() Statement {
	return Statement{
		SQL: "SELECT spock.repair_mode('True');",
	}
}

func CreateDatabase(name string) ConditionalStatement {
	return ConditionalStatement{
		If: Query[bool]{
			SQL: "SELECT NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = @name);",
			Args: pgx.NamedArgs{
				"name": name,
			},
		},
		Then: Statement{
			SQL: fmt.Sprintf("CREATE DATABASE %q;", name),
		},
	}
}

func TerminateOtherConnections(dbName string) Statement {
	return Statement{
		SQL: "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND datname = @name;",
		Args: pgx.NamedArgs{
			"name": dbName,
		},
	}
}

func RenameDB(oldName, newName string) ConditionalStatement {
	return ConditionalStatement{
		If: Query[bool]{
			SQL: "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = @oldName) AND NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = @newName);",
			Args: pgx.NamedArgs{
				"oldName": oldName,
				"newName": newName,
			},
		},
		Then: Statements{
			TerminateOtherConnections(oldName),
			Statement{
				SQL: fmt.Sprintf("ALTER DATABASE %q RENAME TO %q;", oldName, newName),
			},
		},
	}
}

func InitializePgEdgeExtensions(nodeName string, dsn *DSN) Statements {
	return Statements{
		Statement{
			SQL: "CREATE EXTENSION IF NOT EXISTS spock;",
		},
		// Statement{
		// 	SQL: "CREATE EXTENSION IF NOT EXISTS snowflake;",
		// },
		// Statement{
		// 	SQL: "CREATE EXTENSION IF NOT EXISTS lolor;",
		// },
		ConditionalStatement{
			If: Query[bool]{
				SQL: "SELECT NOT EXISTS (SELECT 1 FROM spock.node WHERE node_name = @node_name);",
				Args: pgx.NamedArgs{
					"node_name": nodeName,
				},
			},
			Then: Statement{
				SQL: "SELECT spock.node_create(@node_name, @dsn);",
				Args: pgx.NamedArgs{
					"node_name": nodeName,
					"dsn":       dsn.String(),
				},
			},
		},
	}
}

func SubscriptionNeedsCreate(providerName, subscriberName string) Query[bool] {
	sub := subName(providerName, subscriberName)
	return Query[bool]{
		SQL: "SELECT NOT EXISTS (SELECT 1 FROM spock.subscription WHERE sub_name = @sub_name);",
		Args: pgx.NamedArgs{
			"sub_name": sub,
		},
	}
}

func SubscriptionDsnNeedsUpdate(providerName, subscriberName string, providerDSN *DSN) Query[bool] {
	sub := subName(providerName, subscriberName)
	dsn := providerDSN.String()
	return Query[bool]{
		SQL: "SELECT NOT EXISTS (SELECT 1 from spock.node_interface JOIN spock.subscription ON if_id = sub_origin_if WHERE sub_name = @sub_name AND if_dsn = @peer_dsn);",
		Args: pgx.NamedArgs{
			"sub_name": sub,
			"peer_dsn": dsn,
		},
	}
}

func SubscriptionNeedsEnable(providerName, subscriberName string, disabled bool) Query[bool] {
	sub := subName(providerName, subscriberName)
	return Query[bool]{
		SQL: "SELECT NOT @disabled AND EXISTS (SELECT 1 from spock.subscription WHERE sub_name = @sub_name AND sub_enabled = 'f');",
		Args: pgx.NamedArgs{
			"sub_name": sub,
			"disabled": disabled,
		},
	}
}

func CreateSubscription(providerName, subscriberName string, providerDSN *DSN, disabled bool, syncStructure bool, syncData bool) ConditionalStatement {
	sub := subName(providerName, subscriberName)
	dsn := providerDSN.String()
	interfaceName := fmt.Sprintf("%s_%d", providerName, time.Now().Unix())
	return ConditionalStatement{
		If: SubscriptionNeedsCreate(providerName, subscriberName),
		Then: Statement{
			SQL: `
				SELECT spock.sub_create(
					subscription_name      => @sub_name::name,
					provider_dsn           => @peer_dsn::text,
					synchronize_structure  => @sync_structure::boolean,
					synchronize_data       => @sync_data::boolean,
					enabled                => @enabled::boolean
				);
			`,
			Args: pgx.NamedArgs{
				"sub_name":       sub,
				"peer_dsn":       dsn,
				"sync_structure": syncStructure,
				"sync_data":      syncData,
				"enabled":        !disabled,
			},
		},
		Else: ConditionalStatement{
			If: SubscriptionDsnNeedsUpdate(providerName, subscriberName, providerDSN),
			Then: Statements{
				Statement{
					SQL: "SELECT spock.node_add_interface(@peer_name, @interface_name, @peer_dsn);",
					Args: pgx.NamedArgs{
						"peer_name":      providerName,
						"interface_name": interfaceName,
						"peer_dsn":       dsn,
					},
				},
				Statement{
					SQL: "SELECT spock.sub_alter_interface(@sub_name, @interface_name);",
					Args: pgx.NamedArgs{
						"sub_name":       sub,
						"interface_name": interfaceName,
					},
				},
				Statement{
					SQL: "SELECT spock.node_drop_interface(@peer_name, if_name) FROM spock.node_interface JOIN spock.node ON if_nodeid = node_id WHERE node_name = @peer_name AND if_name != @interface_name;",
					Args: pgx.NamedArgs{
						"peer_name":      providerName,
						"interface_name": interfaceName,
					},
				},
			},
		},
	}
}

func DropSubscription(providerName, subscriberName string) Statement {
	return Statement{
		SQL: "SELECT spock.sub_drop(@sub_name, ifexists := 'true');",
		Args: pgx.NamedArgs{
			"sub_name": subName(providerName, subscriberName),
		},
	}
}

func DropAllSubscriptions() Statement {
	return Statement{
		SQL: "SELECT spock.sub_drop(sub_name) FROM spock.subscription;",
	}
}

func DropSpockAndCleanupSlots(dbName string) Statements {
	return Statements{
		Statement{
			SQL: "DROP EXTENSION IF EXISTS spock CASCADE;",
		},
		// Dropping Spock doesn't always stop all the Spock processes cleanly.
		// We need to terminate their connections in order to drop the
		// replication slots.
		TerminateOtherConnections(dbName),
		Statement{
			// This is filtered to exclude slots created by Patroni.
			SQL: "SELECT slot_name, pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_type = 'logical';",
		},
		Statement{
			// Replication origins are only used by logical replication. This
			// function is only used during restore, so we assume that any
			// logical replication needs to be cleaned up and recreated.
			SQL: "SELECT pg_replication_origin_drop(roname) FROM pg_replication_origin;",
		},
	}
}

func ReplicationSlotName(databaseName, providerName, subscriberName string) string {
	return fmt.Sprintf(
		"spk_%s_%s_%s",
		databaseName,
		providerName,
		subName(providerName, subscriberName),
	)
}

func subName(providerName, subscriberName string) string {
	return fmt.Sprintf("sub_%s_%s", providerName, subscriberName)
}

func SyncEvent() Query[string] {
	return Query[string]{
		SQL: "SELECT spock.sync_event();",
	}
}

func WaitForSyncEvent(originNode, lsn string, timeoutSeconds int) Query[bool] {
	return Query[bool]{
		SQL: "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout);",
		Args: pgx.NamedArgs{
			"origin_node": originNode,
			"lsn":         lsn,
			"timeout":     timeoutSeconds,
		},
	}
}

func ReplicationSlotNeedsCreate(databaseName, providerNode, subscriberNode string) Query[bool] {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return Query[bool]{
		SQL: "SELECT NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = @slot_name);",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
		},
	}
}

func CreateReplicationSlot(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return ConditionalStatement{
		If: ReplicationSlotNeedsCreate(databaseName, providerNode, subscriberNode),
		Then: Statement{
			SQL: "SELECT pg_create_logical_replication_slot(@slot_name, 'spock_output');",
			Args: pgx.NamedArgs{
				"slot_name": slotName,
			},
		},
	}
}

// TerminateReplicationSlot terminates the walsender process using a
// replication slot, if one is active. This must be called before dropping a
// slot whose subscriber has gone down, since pg_drop_replication_slot fails
// on active slots.
func TerminateReplicationSlot(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return ConditionalStatement{
		If: Query[bool]{
			SQL:  "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = @slot_name AND active);",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
		Then: Statement{
			SQL:  "SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE slot_name = @slot_name AND active;",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
	}
}

func DropReplicationSlot(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return ConditionalStatement{
		If: Query[bool]{
			SQL:  "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = @slot_name);",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
		Then: Statement{
			SQL:  "SELECT pg_drop_replication_slot(@slot_name);",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
	}
}

func LagTrackerCommitTimestamp(originNode, receiverNode string) Query[time.Time] {
	return Query[time.Time]{
		SQL: `
			SELECT commit_timestamp
			FROM spock.lag_tracker
			WHERE origin_name = @origin_node
			  AND receiver_name = @receiver_node;
		`,
		Args: pgx.NamedArgs{
			"origin_node":   originNode,
			"receiver_node": receiverNode,
		},
	}
}

func CurrentReplicationSlotLSN(databaseName, providerNode, subscriberNode string) Query[string] {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return Query[string]{
		SQL: "SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = @slot_name;",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
		},
	}
}

// IsReplicationSlotActive checks if a replication slot is currently being used
// by an active walsender process. Uses EXISTS to always return exactly one row.
func IsReplicationSlotActive(databaseName, providerNode, subscriberNode string) Query[bool] {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return Query[bool]{
		SQL: "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = @slot_name AND active_pid IS NOT NULL);",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
		},
	}
}

func GetReplicationSlotLSNFromCommitTS(databaseName, providerNode, subscriberNode string, commitTS time.Time) Query[string] {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return Query[string]{
		SQL: "SELECT spock.get_lsn_from_commit_ts(@slot_name, @commit_ts::timestamp);",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
			"commit_ts": commitTS,
		},
	}
}

func AdvanceReplicationSlotToLSN(databaseName, providerNode, subscriberNode string, targetLSN string) Statement {
	slotName := ReplicationSlotName(databaseName, providerNode, subscriberNode)

	return Statement{
		SQL: `
			WITH current AS (
				SELECT confirmed_flush_lsn
				FROM pg_replication_slots
				WHERE slot_name = @slot_name
			)
			SELECT CASE
				WHEN @lsn > confirmed_flush_lsn
				THEN (pg_replication_slot_advance(@slot_name, @lsn)).end_lsn
				ELSE confirmed_flush_lsn
			END AS new_lsn
			FROM current;
		`,
		Args: pgx.NamedArgs{
			"slot_name": slotName,
			"lsn":       targetLSN,
		},
	}
}

// GetSubscriptionStatus returns the current status of a specific subscription
func GetSubscriptionStatus(providerNode, subscriberNode string) Query[string] {
	return Query[string]{
		SQL: `SELECT (spock.sub_show_status(@sub_name)).status;`,
		Args: pgx.NamedArgs{
			"sub_name": subName(providerNode, subscriberNode),
		},
	}
}

func EnableSubscription(providerNode, subscriberNode string, disabled bool) ConditionalStatement {
	return ConditionalStatement{
		If: SubscriptionNeedsEnable(providerNode, subscriberNode, disabled),
		Then: Statement{
			SQL: `
				SELECT spock.sub_enable(
					subscription_name := @sub_name,
					immediate := true
				);
			`,
			Args: pgx.NamedArgs{
				"sub_name": subName(providerNode, subscriberNode),
			},
		},
	}
}

func GetReplicationSets() Query[ReplicationSet] {
	return Query[ReplicationSet]{
		SQL: `
		SELECT
			set_id::oid          AS setid,
			set_nodeid::oid      AS setnodeid,
			set_name             AS setname,
			replicate_insert     AS repinsert,
			replicate_update     AS repupdate,
			replicate_delete     AS repdelete,
			replicate_truncate   AS reptruncate
		FROM spock.replication_set
		ORDER BY set_id;
	`,
	}
}
func GetReplicationSetTables() Query[ReplicationSetTable] {
	return Query[ReplicationSetTable]{
		SQL: `
		SELECT
			set_id::oid                           AS setid,
			set_reloid::oid                       AS setreloid,
			COALESCE(set_att_list, '{}'::text[])  AS setattlist,
			COALESCE(set_row_filter::text, '')    AS setrowfilter
		FROM spock.replication_set_table
		ORDER BY set_id, set_reloid;
	`,
	}
}

// https://docs.pgedge.com/spock_ext/spock_functions/functions/spock_repset_create
func CreateReplicationSet(r ReplicationSet) Statement {
	return Statement{
		SQL: `
			SELECT
			CASE
				WHEN NOT EXISTS (
					SELECT 1 FROM spock.replication_set WHERE set_name = @set_name::name
				)
				THEN spock.repset_create(
					@set_name::name,
					@rep_ins::boolean,
					@rep_upd::boolean,
					@rep_del::boolean,
					@rep_trunc::boolean
				)
				ELSE (SELECT set_id FROM spock.replication_set WHERE set_name = @set_name::name)
			END;
		`,
		Args: pgx.NamedArgs{
			"set_name":  r.SetName,
			"rep_ins":   r.RepInsert,
			"rep_upd":   r.RepUpdate,
			"rep_del":   r.RepDelete,
			"rep_trunc": r.RepTruncate,
		},
	}
}

// https://docs.pgedge.com/spock_ext/spock_functions/functions/spock_repset_add_table
func AddReplicationSetTable(
	setName string,
	relOID uint32,
	columns []string,
	rowFilter string,
	sync bool,
	includePartitions bool,
) Statement {
	var colsArg any
	if len(columns) == 0 {
		colsArg = nil
	} else {
		colsArg = columns
	}

	var filterArg any
	if strings.TrimSpace(rowFilter) == "" {
		filterArg = nil
	} else {
		filterArg = rowFilter
	}

	return Statement{
		SQL: `
			SELECT spock.repset_add_table(
				@set_name::name,
				@rel::oid::regclass,
				@sync::boolean,
				@cols::text[],
				@row_filter::text,
				@include_partitions::boolean
			);
		`,
		Args: pgx.NamedArgs{
			"set_name":           setName,
			"rel":                relOID,
			"cols":               colsArg,
			"row_filter":         filterArg,
			"sync":               sync,
			"include_partitions": includePartitions,
		},
	}
}

func RestoreReplicationSets(sets []ReplicationSet, tabs []ReplicationSetTable) Statements {
	idToName := make(map[uint32]string, len(sets))
	stmts := make(Statements, 0, len(sets)+len(tabs))

	for _, s := range sets {
		idToName[s.SetID] = s.SetName
		stmts = append(stmts, CreateReplicationSet(s))
	}

	for _, t := range tabs {
		if setName := idToName[t.SetID]; setName != "" {
			stmts = append(stmts, AddReplicationSetTable(
				setName,
				t.SetRelOID,
				t.SetAttList,
				t.SetRowFilter,
				false, // synchronize_data on restore default false
				true,  // include_partitions default true
			))
		}
	}

	return stmts
}
