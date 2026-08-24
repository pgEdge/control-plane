package postgres

import (
	"context"
	"errors"
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

func IsInRecovery() Query[bool] {
	return Query[bool]{
		SQL: "SELECT pg_is_in_recovery();",
	}
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
			SQL: fmt.Sprintf("CREATE DATABASE %s;", QuoteIdentifier(name)),
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
				SQL: fmt.Sprintf("ALTER DATABASE %s RENAME TO %s;", QuoteIdentifier(oldName), QuoteIdentifier(newName)),
			},
		},
	}
}

func NodeNeedsCreate(nodeName string) Query[bool] {
	return Query[bool]{
		SQL: "SELECT NOT EXISTS (SELECT 1 FROM spock.node WHERE node_name = @node);",
		Args: pgx.NamedArgs{
			"node": nodeName,
		},
	}
}

func InitializeSpockNode(nodeName string, nodeDSN *DSN) Statements {
	dsn := nodeDSN.String()
	return Statements{
		Statement{
			SQL: "CREATE EXTENSION IF NOT EXISTS spock;",
		},
		ConditionalStatement{
			If: NodeNeedsCreate(nodeName),
			Then: Statement{
				SQL: "SELECT spock.node_create(@node, @dsn);",
				Args: pgx.NamedArgs{
					"node": nodeName,
					"dsn":  dsn,
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

func subName(providerName, subscriberName string) string {
	return fmt.Sprintf("sub_%s_%s", providerName, subscriberName)
}

// slotNameExpr is the SQL expression that resolves the actual replication
// slot name via Spock's own spock.spock_gen_slot_name(), instead of a
// hand-built Go string. Embedding it as an expression — rather than
// resolving it in Go first — keeps every slot-related statement below a
// single round trip.
const slotNameExpr = "spock.spock_gen_slot_name(@slot_dbname, @slot_provider_node, @slot_sub_name)"

func slotNameArgs(databaseName, providerNode, subscriberNode string) pgx.NamedArgs {
	return pgx.NamedArgs{
		"slot_dbname":        databaseName,
		"slot_provider_node": providerNode,
		"slot_sub_name":      subName(providerNode, subscriberNode),
	}
}

// ResolveSlotName looks up the actual slot name for a provider/subscriber
// pair. Used by call sites that need the resolved string itself — because
// they reuse it across more than one statement — rather than embedding
// slotNameExpr inline.
func ResolveSlotName(databaseName, providerNode, subscriberNode string) Query[string] {
	return Query[string]{
		SQL:  fmt.Sprintf("SELECT %s;", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

// MinSpockVersionForSyncEventArgs is the first Spock version where
// spock.sync_event(boolean) and the 5-arg spock.wait_for_sync_event(...,
// wait_if_disabled) exist — both were introduced together in the
// 5.0.6->5.0.7 upgrade script. Callers must compare the live cluster's Spock
// version against this before passing transactional=true / waitIfDisabled=true
// to SyncEvent/WaitForSyncEvent below; older clusters only have the original
// call shapes.
const MinSpockVersionForSyncEventArgs = "5.0.7"

// SyncEvent sends a sync event marker from the provider. transactional ties
// the marker to the surrounding transaction so it's ordered with any
// preceding DML in that transaction, per Spock's spock.sync_event reference
// behavior. spock.sync_event(boolean) was only introduced in Spock 5.0.7 (see
// MinSpockVersionForSyncEventArgs) — pass transactional=false against older
// clusters to fall back to the original zero-arg call.
func SyncEvent(transactional bool) Query[string] {
	if transactional {
		return Query[string]{
			SQL: "SELECT spock.sync_event(true);",
		}
	}
	return Query[string]{
		SQL: "SELECT spock.sync_event();",
	}
}

// WaitForSyncEvent waits for a peer to apply the sync event at lsn.
// waitIfDisabled=true tells Spock to tolerate the subscription not existing
// yet or being temporarily disabled — both expected during add-node — rather
// than raising immediately, matching Spock's reference wait_for_sync_event
// usage. The 5-arg form (with wait_if_disabled) was only introduced in Spock
// 5.0.7 (see MinSpockVersionForSyncEventArgs) — older clusters only accept
// the 4-arg form.
func WaitForSyncEvent(originNode, lsn string, timeoutSeconds int, waitIfDisabled bool) Query[bool] {
	sql := "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout);"
	if waitIfDisabled {
		sql = "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout, true);"
	}
	return Query[bool]{
		SQL: sql,
		Args: pgx.NamedArgs{
			"origin_node": originNode,
			"lsn":         lsn,
			"timeout":     timeoutSeconds,
		},
	}
}

func ReplicationSlotNeedsCreate(databaseName, providerNode, subscriberNode string) Query[bool] {
	return Query[bool]{
		SQL:  fmt.Sprintf("SELECT NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s);", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

// CreateReplicationSlot creates the logical replication slot backing a peer
// subscription. Pass failover from NeedsNativeFailoverSlots; false is
// byte-for-byte identical to the pre-failover-slot-support statement.
func CreateReplicationSlot(databaseName, providerNode, subscriberNode string, failover bool) ConditionalStatement {
	sql := fmt.Sprintf("SELECT pg_create_logical_replication_slot(%s, 'spock_output');", slotNameExpr)
	if failover {
		sql = fmt.Sprintf("SELECT pg_create_logical_replication_slot(%s, 'spock_output', false, false, true);", slotNameExpr)
	}
	return ConditionalStatement{
		If: ReplicationSlotNeedsCreate(databaseName, providerNode, subscriberNode),
		Then: Statement{
			SQL:  sql,
			Args: slotNameArgs(databaseName, providerNode, subscriberNode),
		},
	}
}

// TerminateReplicationSlot terminates the walsender process using a
// replication slot, if one is active. This must be called before dropping a
// slot whose subscriber has gone down, since pg_drop_replication_slot fails
// on active slots.
func TerminateReplicationSlot(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	args := slotNameArgs(databaseName, providerNode, subscriberNode)

	return ConditionalStatement{
		If: Query[bool]{
			SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s AND active);", slotNameExpr),
			Args: args,
		},
		Then: Statement{
			SQL:  fmt.Sprintf("SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE slot_name = %s AND active;", slotNameExpr),
			Args: args,
		},
	}
}

func DropReplicationSlot(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	args := slotNameArgs(databaseName, providerNode, subscriberNode)

	return ConditionalStatement{
		If: Query[bool]{
			SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s);", slotNameExpr),
			Args: args,
		},
		Then: Statement{
			SQL:  fmt.Sprintf("SELECT pg_drop_replication_slot(%s);", slotNameExpr),
			Args: args,
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
	return Query[string]{
		SQL:  fmt.Sprintf("SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = %s;", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

// IsReplicationSlotActive checks if a replication slot is currently being used
// by an active walsender process. Uses EXISTS to always return exactly one row.
func IsReplicationSlotActive(databaseName, providerNode, subscriberNode string) Query[bool] {
	return Query[bool]{
		SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s AND active_pid IS NOT NULL);", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

// ReplicationSlotExists checks whether the replication slot for the given
// subscription exists. Used to poll for Spock 5.x failover slot creation after
// a switchover, which can take up to 60 seconds.
func ReplicationSlotExists(databaseName, providerNode, subscriberNode string) Query[bool] {
	return Query[bool]{
		SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = %s);", slotNameExpr),
		Args: slotNameArgs(databaseName, providerNode, subscriberNode),
	}
}

func GetReplicationSlotLSNFromCommitTS(databaseName, providerNode, subscriberNode string, commitTS time.Time) Query[string] {
	args := slotNameArgs(databaseName, providerNode, subscriberNode)
	args["commit_ts"] = commitTS

	return Query[string]{
		SQL:  fmt.Sprintf("SELECT spock.get_lsn_from_commit_ts(%s, @commit_ts::timestamp);", slotNameExpr),
		Args: args,
	}
}

func AdvanceReplicationSlotToLSN(databaseName, providerNode, subscriberNode string, targetLSN string) Statement {
	args := slotNameArgs(databaseName, providerNode, subscriberNode)
	args["lsn"] = targetLSN

	return Statement{
		SQL: fmt.Sprintf(`
			WITH current AS (
				SELECT confirmed_flush_lsn
				FROM pg_replication_slots
				WHERE slot_name = %[1]s
			)
			SELECT CASE
				WHEN @lsn > confirmed_flush_lsn
				THEN (pg_replication_slot_advance(%[1]s, @lsn)).end_lsn
				ELSE confirmed_flush_lsn
			END AS new_lsn
			FROM current;
		`, slotNameExpr),
		Args: args,
	}
}

// DropStaleReplicationOrigin drops any pre-existing replication origin for a
// provider/subscriber pair before a fresh subscription is created for it.
// Mirrors zodan.sql's create_disable_subscriptions_and_slots step, which
// explicitly does this "so create_sub starts fresh at 0/0 (avoids stale-LSN
// data loss)": if a node is removed and later re-added under the same name,
// CP's slot/origin naming (spock.spock_gen_slot_name(), same as Spock's own)
// is deterministic on (database, provider, subscriber), so the new
// subscription would otherwise inherit whatever LSN the old incarnation's
// origin was left at. Safe to call unconditionally before create — an origin
// can only exist here if it's stale, since Create() only runs when
// spock.subscription has no row for this pair yet, and an origin never
// outlives its subscription's removal on its own.
func DropStaleReplicationOrigin(databaseName, providerNode, subscriberNode string) ConditionalStatement {
	args := slotNameArgs(databaseName, providerNode, subscriberNode)
	return ConditionalStatement{
		If: Query[bool]{
			SQL:  fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_replication_origin WHERE roname = %s);", slotNameExpr),
			Args: args,
		},
		Then: Statement{
			SQL:  fmt.Sprintf("SELECT pg_replication_origin_drop(%s);", slotNameExpr),
			Args: args,
		},
	}
}

func EnsureReplicationOriginExists(slotName string) ConditionalStatement {
	return ConditionalStatement{
		If: Query[bool]{
			SQL:  "SELECT NOT EXISTS (SELECT 1 FROM pg_replication_origin WHERE roname = @slot_name);",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
		Then: Statement{
			SQL:  "SELECT pg_replication_origin_create(@slot_name);",
			Args: pgx.NamedArgs{"slot_name": slotName},
		},
	}
}

func AdvanceReplicationOrigin(slotName, lsn string) Statement {
	return Statement{
		SQL: "SELECT pg_replication_origin_advance(@slot_name, @lsn::pg_lsn);",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
			"lsn":       lsn,
		},
	}
}

// SpockProgressReachedLSN reports whether the local node's apply progress
// from the named peer has reached targetLSN. Uses remote_lsn (the LSN of the
// last applied commit) on Spock < 6, or remote_commit_lsn on Spock >= 6 —
// spock.progress became a view over apply_group_progress() in Spock 6 and
// the column was renamed. Neither uses received_lsn, which can advance on
// keepalive messages before any commits have been applied.
func SpockProgressReachedLSN(spockMajor uint64, peerNodeName, targetLSN string) Query[bool] {
	column := "remote_lsn"
	if spockMajor >= 6 {
		column = "remote_commit_lsn"
	}
	return Query[bool]{
		SQL: fmt.Sprintf(`
			SELECT COALESCE(
				(SELECT p.%s >= @target_lsn::pg_lsn
				 FROM spock.progress p
				 JOIN spock.node n ON n.node_id = p.remote_node_id
				 WHERE p.node_id = (SELECT node_id FROM spock.node_info())
				   AND n.node_name = @peer_node_name),
				false
			)
		`, column),
		Args: pgx.NamedArgs{
			"peer_node_name": peerNodeName,
			"target_lsn":     targetLSN,
		},
	}
}

// LsnAtOrBefore reports whether lsn1 <= lsn2 using PostgreSQL's pg_lsn type.
// Use this instead of Go string comparison — LSNs are hex-formatted and string
// ordering produces wrong results across segment boundaries (e.g. "F/..." > "10/...").
func LsnAtOrBefore(lsn1, lsn2 string) Query[bool] {
	return Query[bool]{
		SQL:  "SELECT @lsn1::pg_lsn <= @lsn2::pg_lsn",
		Args: pgx.NamedArgs{"lsn1": lsn1, "lsn2": lsn2},
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

// StartRepairModeTxn will start a new transaction and, if Spock is enabled,
// enable repair mode for the transaction. Callers are responsible for calling
// Rollback and Commit on the returned transaction.
func StartRepairModeTxn(ctx context.Context, conn *pgx.Conn) (pgx.Tx, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	enabled, err := IsSpockEnabled().Scalar(ctx, tx)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("failed to check if spock is enabled: %w", err),
			tx.Rollback(ctx),
		)
	}
	if enabled {
		err = EnableRepairMode().Exec(ctx, tx)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("failed to enable repair mode: %w", err),
				tx.Rollback(ctx),
			)
		}
	}
	return tx, nil
}
