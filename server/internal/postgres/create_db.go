package postgres

import (
	"fmt"

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
				"peer_dsn": peerDSN.String(),
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

func subName(nodeName, peerName string) string {
	return fmt.Sprintf("sub_%s%s", nodeName, peerName)
}
