package swarm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestServiceUserRoleIdentifier(t *testing.T) {
	t.Run("canonical resource uses service+mode identifier", func(t *testing.T) {
		r := &ServiceUserRole{
			ServiceID: "svc-abc",
			Mode:      ServiceUserRoleRO,
		}
		got := r.Identifier()
		want := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRO)
		if got != want {
			t.Errorf("Identifier() = %v, want %v", got, want)
		}
	})

	t.Run("per-node resource uses service+mode+node identifier", func(t *testing.T) {
		canonicalID := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRW)
		r := &ServiceUserRole{
			ServiceID:        "svc-abc",
			Mode:             ServiceUserRoleRW,
			NodeName:         "n2",
			CredentialSource: &canonicalID,
		}
		got := r.Identifier()
		want := ServiceUserRolePerNodeIdentifier("svc-abc", ServiceUserRoleRW, "n2")
		if got != want {
			t.Errorf("Identifier() = %v, want %v", got, want)
		}
	})

	t.Run("canonical and per-node identifiers are distinct", func(t *testing.T) {
		canonical := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRO)
		perNode := ServiceUserRolePerNodeIdentifier("svc-abc", ServiceUserRoleRO, "n1")
		if canonical == perNode {
			t.Errorf("canonical and per-node identifiers should differ, both = %v", canonical)
		}
	})
}

func TestServiceUserRolePerNodeIdentifier(t *testing.T) {
	t.Run("format is service-mode-node", func(t *testing.T) {
		got := ServiceUserRolePerNodeIdentifier("svc-abc", "ro", "n2")
		want := resource.Identifier{
			ID:   "svc-abc-ro-n2",
			Type: ResourceTypeServiceUserRole,
		}
		if got != want {
			t.Errorf("ServiceUserRolePerNodeIdentifier() = %v, want %v", got, want)
		}
	})

	t.Run("different nodes produce different identifiers", func(t *testing.T) {
		id1 := ServiceUserRolePerNodeIdentifier("svc-abc", "ro", "n1")
		id2 := ServiceUserRolePerNodeIdentifier("svc-abc", "ro", "n2")
		if id1 == id2 {
			t.Errorf("different nodes should produce different identifiers, both = %v", id1)
		}
	})
}

func TestServiceUserRoleDependencies(t *testing.T) {
	t.Run("canonical resource depends only on its node", func(t *testing.T) {
		r := &ServiceUserRole{
			ServiceID: "svc-abc",
			NodeName:  "n1",
			Mode:      ServiceUserRoleRO,
		}
		deps := r.Dependencies()
		nodeID := database.NodeResourceIdentifier("n1")
		if len(deps) != 1 {
			t.Fatalf("canonical resource Dependencies() = %v, want 1 dependency", deps)
		}
		if deps[0] != nodeID {
			t.Errorf("canonical resource dependency = %v, want %v", deps[0], nodeID)
		}
	})

	t.Run("per-node resource depends on node and canonical", func(t *testing.T) {
		canonicalID := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRO)
		r := &ServiceUserRole{
			ServiceID:        "svc-abc",
			Mode:             ServiceUserRoleRO,
			NodeName:         "n2",
			CredentialSource: &canonicalID,
		}
		deps := r.Dependencies()
		if len(deps) != 2 {
			t.Fatalf("per-node resource Dependencies() = %v, want 2 dependencies", deps)
		}
		nodeID := database.NodeResourceIdentifier("n2")
		if deps[0] != nodeID {
			t.Errorf("per-node resource deps[0] = %v, want node %v", deps[0], nodeID)
		}
		if deps[1] != canonicalID {
			t.Errorf("per-node resource deps[1] = %v, want canonical %v", deps[1], canonicalID)
		}
	})
}

// buildServiceUserRoles replicates the orchestrator's per-node resource
// construction logic so it can be tested without a full Orchestrator instance.
func buildServiceUserRoles(serviceID, databaseID, databaseName, firstNodeName string, extraNodeNames []string) []*ServiceUserRole {
	canonicalROID := ServiceUserRoleIdentifier(serviceID, ServiceUserRoleRO)
	canonicalRWID := ServiceUserRoleIdentifier(serviceID, ServiceUserRoleRW)

	roles := []*ServiceUserRole{
		{ServiceID: serviceID, DatabaseID: databaseID, DatabaseName: databaseName, NodeName: firstNodeName, Mode: ServiceUserRoleRO},
		{ServiceID: serviceID, DatabaseID: databaseID, DatabaseName: databaseName, NodeName: firstNodeName, Mode: ServiceUserRoleRW},
	}
	for _, nodeName := range extraNodeNames {
		roles = append(roles,
			&ServiceUserRole{ServiceID: serviceID, DatabaseID: databaseID, DatabaseName: databaseName, NodeName: nodeName, Mode: ServiceUserRoleRO, CredentialSource: &canonicalROID},
			&ServiceUserRole{ServiceID: serviceID, DatabaseID: databaseID, DatabaseName: databaseName, NodeName: nodeName, Mode: ServiceUserRoleRW, CredentialSource: &canonicalRWID},
		)
	}
	return roles
}

func TestServiceUserRolePerNodeProvisioning(t *testing.T) {
	tests := []struct {
		name          string
		extraNodes    []string
		wantTotal     int
		wantCanonical int
		wantPerNode   int
	}{
		{"single node", nil, 2, 2, 0},
		{"two nodes", []string{"n2"}, 4, 2, 2},
		{"three nodes", []string{"n2", "n3"}, 6, 2, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles := buildServiceUserRoles("svc-abc", "db-abc", "appdb", "n1", tt.extraNodes)

			if len(roles) != tt.wantTotal {
				t.Errorf("got %d ServiceUserRole resources, want %d", len(roles), tt.wantTotal)
			}

			var canonicals, perNodes []*ServiceUserRole
			for _, r := range roles {
				if r.CredentialSource == nil {
					canonicals = append(canonicals, r)
				} else {
					perNodes = append(perNodes, r)
				}
			}

			if len(canonicals) != tt.wantCanonical {
				t.Errorf("got %d canonical resources, want %d", len(canonicals), tt.wantCanonical)
			}
			if len(perNodes) != tt.wantPerNode {
				t.Errorf("got %d per-node resources, want %d", len(perNodes), tt.wantPerNode)
			}

			// Verify per-node resources reference the correct canonical identifier.
			canonicalROID := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRO)
			canonicalRWID := ServiceUserRoleIdentifier("svc-abc", ServiceUserRoleRW)
			for _, pn := range perNodes {
				var wantSource resource.Identifier
				switch pn.Mode {
				case ServiceUserRoleRO:
					wantSource = canonicalROID
				case ServiceUserRoleRW:
					wantSource = canonicalRWID
				default:
					t.Errorf("unexpected mode %q", pn.Mode)
					continue
				}
				if *pn.CredentialSource != wantSource {
					t.Errorf("per-node %s CredentialSource = %v, want %v", pn.Mode, *pn.CredentialSource, wantSource)
				}
			}

			// Verify per-node resources are routed to the correct node.
			for i, nodeName := range tt.extraNodes {
				roRole := perNodes[i*2]
				rwRole := perNodes[i*2+1]
				if roRole.NodeName != nodeName {
					t.Errorf("per-node RO role[%d].NodeName = %q, want %q", i, roRole.NodeName, nodeName)
				}
				if rwRole.NodeName != nodeName {
					t.Errorf("per-node RW role[%d].NodeName = %q, want %q", i, rwRole.NodeName, nodeName)
				}
			}
		})
	}
}

func TestServiceUserRolePerNodeIdentifierUniqueness(t *testing.T) {
	// All identifiers across a 3-node cluster must be unique.
	nodes := []string{"n1", "n2", "n3"}
	roles := buildServiceUserRoles("svc-abc", "db-abc", "appdb", nodes[0], nodes[1:])

	seen := make(map[resource.Identifier]string)
	for i, r := range roles {
		id := r.Identifier()
		if prev, exists := seen[id]; exists {
			t.Errorf("duplicate identifier %v: role[%d] and %s", id, i, prev)
		}
		seen[id] = fmt.Sprintf("role[%d] node=%s mode=%s", i, r.NodeName, r.Mode)
	}
}

// statementsSQL extracts the raw SQL from a postgres.Statements slice.
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
