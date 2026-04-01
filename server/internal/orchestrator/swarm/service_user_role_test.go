package swarm

import (
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
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



