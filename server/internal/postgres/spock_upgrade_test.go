package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestEnableSlotFailoverCommand(t *testing.T) {
	assert.Equal(t,
		`ALTER_REPLICATION_SLOT "spk_app_n1_sub_n1_n2" ( FAILOVER true )`,
		postgres.EnableSlotFailoverCommand("spk_app_n1_sub_n1_n2"),
	)
	// Quotes in a name are escaped rather than ending the identifier.
	assert.Equal(t,
		`ALTER_REPLICATION_SLOT "a""b" ( FAILOVER true )`,
		postgres.EnableSlotFailoverCommand(`a"b`),
	)
}

func TestDDLReplicationStatements(t *testing.T) {
	sql := func(stmts postgres.Statements) []string {
		var out []string
		for _, stmt := range stmts {
			out = append(out, stmt.(postgres.Statement).SQL)
		}
		return out
	}
	assert.Equal(t, []string{
		"ALTER SYSTEM SET spock.enable_ddl_replication = off;",
		"SELECT pg_reload_conf();",
	}, sql(postgres.PauseDDLReplication()))
	assert.Equal(t, []string{
		"ALTER SYSTEM RESET spock.enable_ddl_replication;",
		"SELECT pg_reload_conf();",
	}, sql(postgres.ResumeDDLReplication()))
}

func TestIsReplicationSlotSynced(t *testing.T) {
	sql := postgres.IsReplicationSlotSynced("app", "n1", "n2").SQL
	// An invalidated copy can't be used after a promotion.
	assert.Contains(t, sql, "synced AND NOT temporary AND invalidation_reason IS NULL")
}
