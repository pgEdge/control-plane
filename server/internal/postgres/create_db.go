package postgres

import (
	"fmt"
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
		Statement{
			SQL: "CREATE EXTENSION IF NOT EXISTS snowflake;",
		},
		Statement{
			SQL: "CREATE EXTENSION IF NOT EXISTS lolor;",
		},
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

func GetSubscriptionID(nodeName, peerName string) Query[uint32] {
	return Query[uint32]{
		SQL: "SELECT sub_id FROM spock.subscription WHERE sub_name = @sub_name;",
		Args: pgx.NamedArgs{
			"sub_name": subName(nodeName, peerName),
		},
	}
}

func CreateSubscription(nodeName, peerName string, peerDSN *DSN) ConditionalStatement {
	sub := subName(nodeName, peerName)
	dsn := peerDSN.String()
	interfaceName := fmt.Sprintf("%s_%d", peerName, time.Now().Unix())
	return ConditionalStatement{
		If: Query[bool]{
			SQL: "SELECT NOT EXISTS (SELECT 1 FROM spock.subscription WHERE sub_name = @sub_name);",
			Args: pgx.NamedArgs{
				"sub_name": sub,
			},
		},
		Then: Statement{
			SQL: "SELECT spock.sub_create(@sub_name, @peer_dsn);",
			Args: pgx.NamedArgs{
				"sub_name": sub,
				"peer_dsn": dsn,
			},
		},
		Else: ConditionalStatement{
			If: Query[bool]{
				SQL: "SELECT NOT EXISTS (SELECT 1 from spock.node_interface JOIN spock.subscription ON if_id = sub_origin_if WHERE sub_name = @sub_name AND if_dsn = @peer_dsn);",
				Args: pgx.NamedArgs{
					"sub_name": sub,
					"peer_dsn": dsn,
				},
			},
			Then: Statements{
				Statement{
					SQL: "SELECT spock.node_add_interface(@peer_name, @interface_name, @peer_dsn);",
					Args: pgx.NamedArgs{
						"peer_name":      peerName,
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
						"peer_name":      peerName,
						"interface_name": interfaceName,
					},
				},
			},
		},
	}
}

func DropSubscription(nodeName, peerName string) Statement {
	return Statement{
		SQL: "SELECT spock.sub_drop(@sub_name, ifexists := 'true');",
		Args: pgx.NamedArgs{
			"sub_name": fmt.Sprintf("sub_%s%s", nodeName, peerName),
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

func subName(nodeName, peerName string) string {
	return fmt.Sprintf("sub_%s%s", nodeName, peerName)
}

func SyncEvent() Query[string] {
	return Query[string]{
		SQL: "SELECT spock.sync_event();",
	}
}

func WaitForSyncEvent(originNode, lsn string, timeoutMs int) Statement {
	return Statement{
		SQL: "CALL spock.wait_for_sync_event(true, @origin_node, @lsn, @timeout);",
		Args: pgx.NamedArgs{
			"origin_node": originNode,
			"lsn":         lsn,
			"timeout":     timeoutMs,
		},
	}
}

func CreateDisabledSubscription(subscriberNode, providerNode string, providerDSN *DSN) Statement {
	return Statement{
		SQL: `
			SELECT spock.sub_create(
				sub_name := @sub_name,
				provider_dsn := @provider_dsn,
				synchronize_structure := false,
				synchronize_data := false,
				forward_origins := ARRAY[]::text[],
				enabled := false
			);
		`,
		Args: pgx.NamedArgs{
			"sub_name":     subName(subscriberNode, providerNode),
			"provider_dsn": providerDSN.String(),
		},
	}
}

func CreateReplicationSlot(databaseName, providerNode, subscriberNode string) Statement {
	subName := subName(providerNode, subscriberNode)
	slotName := fmt.Sprintf("spk_%s_%s_%s", databaseName, providerNode, subName)

	return Statement{
		SQL: "SELECT pg_create_logical_replication_slot(@slot_name, 'spock_output');",
		Args: pgx.NamedArgs{
			"slot_name": slotName,
		},
	}
}

func CreateActiveSubscription(subscriberNode, providerNode string, providerDSN *DSN) Statement {
	return Statement{
		SQL: `
			SELECT spock.sub_create(
				subscription_name := @sub_name,
				provider_dsn := @provider_dsn,
				synchronize_structure := true,
				synchronize_data := true,
				forward_origins := '{}'::text[],
				enabled := true
			);
		`,
		Args: pgx.NamedArgs{
			"sub_name":     subName(subscriberNode, providerNode),
			"provider_dsn": providerDSN.String(),
		},
	}
}
