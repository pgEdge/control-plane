package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

func CreateDatabase(name string) Statement {
	return Statement{
		SQL: fmt.Sprintf("CREATE DATABASE %q;", name),
	}
}

func InitializePgEdgeExtensions(nodeName string, dsn *DSN) Statements {
	return Statements{
		{
			SQL: "CREATE EXTENSION IF NOT EXISTS spock;",
		},
		{
			SQL: "CREATE EXTENSION IF NOT EXISTS snowflake;",
		},
		{
			SQL: "CREATE EXTENSION IF NOT EXISTS lolor;",
		},
		{
			SQL: "SELECT spock.node_create(@node_name, @dsn);",
			Args: pgx.NamedArgs{
				"node_name": nodeName,
				"dsn":       dsn.String(),
			},
		},
	}
}

func CreateSubscription(nodeName, peerName string, peerDSN *DSN) Statement {
	return Statement{
		SQL: "SELECT spock.sub_create(@sub_name, @peer_dsn);",
		Args: pgx.NamedArgs{
			"sub_name": fmt.Sprintf("sub_%s%s", nodeName, peerName),
			"peer_dsn": peerDSN.String(),
		},
	}
}
