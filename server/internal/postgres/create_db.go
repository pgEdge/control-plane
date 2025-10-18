package postgres

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

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

func WaitForSyncEvent(originNode, lsn string, timeoutSeconds int) Statement {
	return Statement{
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

func RepsetCreateIfNotExists(name string, replicateInsert, replicateUpdate, replicateDelete, replicateTruncate bool) Statement {
	return Statement{
		SQL: `
INSERT INTO spock.replication_set (set_nodeid, set_name, replicate_insert, replicate_update, replicate_delete, replicate_truncate)
SELECT
    n.node_id,
    @name::name,
    @replicate_insert::boolean,
    @replicate_update::boolean,
    @replicate_delete::boolean,
    @replicate_truncate::boolean
FROM spock.node n
WHERE NOT EXISTS (
    SELECT 1 FROM spock.replication_set rs WHERE rs.set_name = @name
)
LIMIT 1;
`,
		Args: pgx.NamedArgs{
			"name":               name,
			"replicate_insert":   replicateInsert,
			"replicate_update":   replicateUpdate,
			"replicate_delete":   replicateDelete,
			"replicate_truncate": replicateTruncate,
		},
	}
}

func RepsetAddTableIfNotExists(repsetName, relation string) Statement {
	return Statement{
		SQL: `
SELECT spock.repset_add_table(
    rs.set_id,
    @relation::regclass,
    false,
    NULL,
    NULL
)
FROM spock.replication_set rs
WHERE rs.set_name = @repset_name
  AND NOT EXISTS (
    SELECT 1
    FROM spock.replication_set_table rst
    WHERE rst.set_id = rs.set_id
      AND rst.set_reloid = (@relation::regclass)::oid
)
LIMIT 1;
`,
		Args: pgx.NamedArgs{
			"repset_name": repsetName,
			"relation":    relation,
		},
	}
}

func RepsetAddAllTables(repsetName string, schemas []string) Statement {
	quoted := make([]string, 0, len(schemas))
	for _, s := range schemas {
		qs := strings.ReplaceAll(s, `'`, `''`)
		quoted = append(quoted, fmt.Sprintf("'%s'", qs))
	}
	schemaArrayLiteral := fmt.Sprintf("ARRAY[%s]::text[]", strings.Join(quoted, ","))
	sql := fmt.Sprintf("SELECT spock.repset_add_all_tables('%s', %s);", strings.ReplaceAll(repsetName, `'`, `''`), schemaArrayLiteral)

	return Statement{
		SQL: sql,
	}
}

func RepsetAddTableWithOptions(repsetName, relation string, attList []string, rowFilter *string) *Statement {
	if len(attList) == 0 && rowFilter == nil {
		return nil
	}

	sql := `
SELECT spock.repset_add_table(
    rs.set_id,
    @relation::regclass,
    false,
    @att_array::text[],
    @row_filter::text
)
FROM spock.replication_set rs
WHERE rs.set_name = @repset_name
  AND NOT EXISTS (
    SELECT 1
    FROM spock.replication_set_table rst
    WHERE rst.set_id = rs.set_id
      AND rst.set_reloid = (@relation::regclass)::oid
)
LIMIT 1;
`
	var attArg any = nil
	if len(attList) > 0 {
		attArg = attList
	}
	var rfArg any = nil
	if rowFilter != nil {
		rfArg = *rowFilter
	}

	return &Statement{
		SQL: sql,
		Args: pgx.NamedArgs{
			"repset_name": repsetName,
			"relation":    relation,
			"att_array":   attArg,
			"row_filter":  rfArg,
		},
	}
}

func SetGUCsForRestore() Statement {
	return Statement{
		SQL: `
SET log_statement = 'none';
SET spock.enable_ddl_replication = off;
`,
	}
}

func RestoreGUCsAfterRestore() Statement {
	return Statement{
		SQL: `
SET spock.enable_ddl_replication = on;
SET log_statement = 'all';
`,
	}
}

func RepsetsSQL(hasSetID bool) string {
	if hasSetID {
		return `
SELECT COALESCE(json_agg(row_to_json(t)), '[]'::json)
FROM (
  SELECT set_id, set_name, replicate_insert, replicate_update, replicate_delete, replicate_truncate
  FROM spock.replication_set
  ORDER BY set_name
) t;
`
	}
	// older schema
	return `
SELECT COALESCE(json_agg(row_to_json(t)), '[]'::json)
FROM (
  SELECT repsetid, repsetname, replicate_insert, replicate_update, replicate_delete, replicate_truncate
  FROM spock.replication_set
  ORDER BY repsetname
) t;
`
}

func RstTablesSQL(hasSetReloid bool) string {
	if hasSetReloid {
		return `
SELECT COALESCE(json_agg(row_to_json(t)), '[]'::json)
FROM (
  SELECT
    rs.set_name AS set_name,
    rst.set_reloid::regclass::text AS table_name,
    rst.set_att_list,
    rst.set_row_filter
  FROM spock.replication_set_table rst
  LEFT JOIN spock.replication_set rs ON rst.set_id = rs.set_id
  WHERE EXISTS (
    SELECT 1
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.oid = rst.set_reloid
      AND c.relkind IN ('r','v')
  )
  ORDER BY rs.set_name, rst.set_reloid::regclass::text
) t;
`
	}
	// older schema
	return `
SELECT COALESCE(json_agg(row_to_json(t)), '[]'::json)
FROM (
  SELECT
    rset.repsetname AS set_name,
    rst.tableoid::regclass::text AS table_name,
    rst.set_att_list,
    rst.set_row_filter
  FROM spock.replication_set_table rst
  LEFT JOIN spock.replication_set rset ON rst.repsetid = rset.repsetid
  WHERE EXISTS (
    SELECT 1 FROM pg_class c WHERE c.oid = rst.tableoid AND c.relkind IN ('r','v')
  )
  ORDER BY rset.repsetname, rst.tableoid::regclass::text
) t;
`
}

func DetectSpockHasSetID() Query[string] {
	return Query[string]{
		SQL: `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = 'spock'
    AND table_name = 'replication_set'
    AND column_name = 'set_id'
);
`,
	}
}

// DetectSpockHasSetReloid returns a query that checks whether spock.replication_set_table has column 'set_reloid'.
// Returns "t" if true, "f" otherwise.
func DetectSpockHasSetReloid() Query[string] {
	return Query[string]{
		SQL: `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = 'spock'
    AND table_name = 'replication_set_table'
    AND column_name = 'set_reloid'
);
`,
	}
}

type PsqlCommand struct {
	DBName string
	SQL    string
}

func (p PsqlCommand) StringSlice() []string {
	return []string{
		"psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-t", "-A", "-d", p.DBName, "-c", p.SQL,
	}
}

func PsqlCmdSQL(dbName, sql string) PsqlCommand {
	return PsqlCommand{DBName: dbName, SQL: sql}
}
