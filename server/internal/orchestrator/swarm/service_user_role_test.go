package swarm

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

// statementsSQL extracts the raw SQL from a postgres.Statements slice.
// Each element must be a postgres.Statement (the only IStatement implementation
// used by ServiceUserRole).
func statementsSQL(stmts postgres.Statements) []string {
	out := make([]string, 0, len(stmts))
	for _, s := range stmts {
		if stmt, ok := s.(postgres.Statement); ok {
			out = append(out, stmt.SQL)
		}
	}
	return out
}

func joinSQL(stmts postgres.Statements) string {
	return strings.Join(statementsSQL(stmts), "\n")
}

func TestRoleAttributesAndGrants_MCP(t *testing.T) {
	r := &ServiceUserRole{
		ServiceType:  "mcp",
		DatabaseName: "mydb",
		Username:     `"svc_mcp"`,
	}
	attrs, grants := r.roleAttributesAndGrants()

	// LOGIN only, no NOINHERIT
	if len(attrs) != 1 || attrs[0] != "LOGIN" {
		t.Errorf("attributes = %v, want [LOGIN]", attrs)
	}

	sql := joinSQL(grants)
	for _, want := range []string{
		"GRANT CONNECT",
		"GRANT USAGE",
		"GRANT SELECT",
		"ALTER DEFAULT PRIVILEGES",
		"pg_read_all_settings",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("MCP grants missing %q\nGot:\n%s", want, sql)
		}
	}
}

func TestRoleAttributesAndGrants_PostgREST_Attributes(t *testing.T) {
	r := &ServiceUserRole{
		ServiceType:  "postgrest",
		DatabaseName: "mydb",
		Username:     "svc_pgrest",
		DBAnonRole:   "web_anon",
	}
	attrs, _ := r.roleAttributesAndGrants()

	attrSet := make(map[string]bool)
	for _, a := range attrs {
		attrSet[a] = true
	}
	if !attrSet["LOGIN"] {
		t.Error("PostgREST attributes must include LOGIN")
	}
	if !attrSet["NOINHERIT"] {
		t.Error("PostgREST attributes must include NOINHERIT")
	}
}

func TestRoleAttributesAndGrants_PostgREST_GrantsAnonRole(t *testing.T) {
	r := &ServiceUserRole{
		ServiceType:  "postgrest",
		DatabaseName: "mydb",
		Username:     "svc_pgrest",
		DBAnonRole:   "web_anon",
	}
	_, grants := r.roleAttributesAndGrants()
	sql := joinSQL(grants)

	if !strings.Contains(sql, "GRANT CONNECT") {
		t.Errorf("PostgREST grants missing GRANT CONNECT\nGot:\n%s", sql)
	}
	if !strings.Contains(sql, `"web_anon"`) {
		t.Errorf("PostgREST grants must grant configured DBAnonRole\nGot:\n%s", sql)
	}
}

func TestRoleAttributesAndGrants_PostgREST_DefaultAnonRole(t *testing.T) {
	// Empty DBAnonRole → default to pgedge_application_read_only
	r := &ServiceUserRole{
		ServiceType:  "postgrest",
		DatabaseName: "mydb",
		Username:     "svc_pgrest",
		DBAnonRole:   "",
	}
	_, grants := r.roleAttributesAndGrants()
	sql := joinSQL(grants)

	if !strings.Contains(sql, `"pgedge_application_read_only"`) {
		t.Errorf("PostgREST must default DBAnonRole to pgedge_application_read_only\nGot:\n%s", sql)
	}
}

func TestRoleAttributesAndGrants_PostgREST_NoDirectTableGrants(t *testing.T) {
	// PostgREST accesses tables via the anon role — no direct table grants.
	r := &ServiceUserRole{
		ServiceType:  "postgrest",
		DatabaseName: "mydb",
		Username:     "svc_pgrest",
		DBAnonRole:   "web_anon",
	}
	_, grants := r.roleAttributesAndGrants()
	sql := joinSQL(grants)

	for _, forbidden := range []string{
		"GRANT SELECT",
		"GRANT USAGE ON SCHEMA",
		"ALTER DEFAULT PRIVILEGES",
		"pg_read_all_settings",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("PostgREST grants must not include %q (accesses tables via anon role)\nGot:\n%s", forbidden, sql)
		}
	}
}

func TestRoleAttributesAndGrants_EmptyServiceTypeIsMCP(t *testing.T) {
	// Regression: empty ServiceType falls through to the MCP default.
	rEmpty := &ServiceUserRole{ServiceType: "", DatabaseName: "mydb", Username: "u"}
	rMCP := &ServiceUserRole{ServiceType: "mcp", DatabaseName: "mydb", Username: "u"}

	attrsEmpty, grantsEmpty := rEmpty.roleAttributesAndGrants()
	attrsMCP, grantsMCP := rMCP.roleAttributesAndGrants()

	if strings.Join(attrsEmpty, ",") != strings.Join(attrsMCP, ",") {
		t.Errorf("empty ServiceType attrs %v != mcp attrs %v", attrsEmpty, attrsMCP)
	}
	if joinSQL(grantsEmpty) != joinSQL(grantsMCP) {
		t.Errorf("empty ServiceType grants differ from mcp grants")
	}
}
