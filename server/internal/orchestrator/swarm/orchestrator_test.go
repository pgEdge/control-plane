package swarm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestServiceInstanceName(t *testing.T) {
	tests := []struct {
		name       string
		databaseID string
		serviceID  string
		hostID     string
	}{
		{
			name:       "short IDs",
			databaseID: "my-db",
			serviceID:  "mcp-server",
			hostID:     "host1",
		},
		{
			name:       "UUID host ID",
			databaseID: "my-db",
			serviceID:  "mcp-server",
			hostID:     "dbf5779c-492a-11f0-b11a-1b8cb15693a8",
		},
		{
			name:       "postgrest service",
			databaseID: "storefront",
			serviceID:  "api",
			hostID:     "host-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceInstanceName(tt.databaseID, tt.serviceID, tt.hostID)

			// Verify format: {databaseID}-{serviceID}-{8charHash}
			prefix := fmt.Sprintf("%s-%s-", tt.databaseID, tt.serviceID)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("ServiceInstanceName() = %q, want prefix %q", got, prefix)
			}

			// Verify the suffix is exactly 8 characters (base36 hash)
			suffix := strings.TrimPrefix(got, prefix)
			if len(suffix) != 8 {
				t.Errorf("ServiceInstanceName() hash suffix = %q (len %d), want 8 chars", suffix, len(suffix))
			}

			// Must be within Docker Swarm's 63-char limit.
			if len(got) > 63 {
				t.Errorf("ServiceInstanceName() = %q (len %d), must be <= 63 chars", got, len(got))
			}

			// Must be deterministic.
			got2 := ServiceInstanceName(tt.databaseID, tt.serviceID, tt.hostID)
			if got != got2 {
				t.Errorf("ServiceInstanceName() not deterministic: %q != %q", got, got2)
			}

			t.Logf("ServiceInstanceName() = %q (len %d)", got, len(got))
		})
	}

	t.Run("different hosts produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db1", "svc1", "host-a")
		name2 := ServiceInstanceName("db1", "svc1", "host-b")
		if name1 == name2 {
			t.Errorf("different host IDs should produce different names, both got %q", name1)
		}
	})

	t.Run("different databases produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db-aaa", "api", "host-1")
		name2 := ServiceInstanceName("db-bbb", "api", "host-1")
		if name1 == name2 {
			t.Errorf("different database IDs should produce different names, both got %q", name1)
		}
	})

	t.Run("different service IDs produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db1", "api-v1", "host-1")
		name2 := ServiceInstanceName("db1", "api-v2", "host-1")
		if name1 == name2 {
			t.Errorf("different service IDs should produce different names, both got %q", name1)
		}
	})

}

// newLakekeeperTestOrchestrator returns an Orchestrator wired for unit tests
// of lakekeeper resource generation. It uses the zero-value Docker client
// (unavailable in unit tests) but sets up the serviceVersions and config so
// that generateLakekeeperInstanceResources can be exercised without a real
// Docker daemon.
func newLakekeeperTestOrchestrator(t *testing.T) *Orchestrator {
	cfg := config.Config{
		DataDir: "/var/lib/pgedge",
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	return &Orchestrator{
		cfg:             cfg,
		serviceVersions: newTestServiceVersions(t, cfg),
		swarmNodeID:     "test-swarm-node",
		// dbNetworkAllocator is the zero value — generateLakekeeperInstanceResources
		// only stores it in the Network resource; it does not dereference it.
		dbNetworkAllocator: Allocator{},
	}
}

// makeLakekeeperSpec returns a minimal ServiceInstanceSpec for a lakekeeper
// service, pre-populated with both required config keys.
func makeLakekeeperSpec(catalogDBURL, pgEncryptionKey string) *database.ServiceInstanceSpec {
	cfg := map[string]any{}
	if catalogDBURL != "" {
		cfg["catalog_db_url"] = catalogDBURL
	}
	if pgEncryptionKey != "" {
		cfg["pg_encryption_key"] = pgEncryptionKey
	}
	return &database.ServiceInstanceSpec{
		ServiceInstanceID: "inst-lakekeeper-1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "lakekeeper",
			ServiceType: "lakekeeper",
			Version:     "0.13.1",
			Config:      cfg,
		},
		DatabaseID:   "db-1",
		DatabaseName: "testdb",
		HostID:       "host-1",
	}
}

// TestGenerateLakekeeperInstanceResources_MissingCatalogURL verifies that the
// resource generator fails loudly when catalog_db_url is absent, rather than
// producing a resource graph with blank env vars that would crash-loop the container.
func TestGenerateLakekeeperInstanceResources_MissingCatalogURL(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)

	t.Run("missing catalog_db_url returns error", func(t *testing.T) {
		spec := makeLakekeeperSpec("", "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==")
		_, err := o.generateLakekeeperInstanceResources(spec)
		if err == nil {
			t.Fatal("expected error for missing catalog_db_url, got nil")
		}
		if !strings.Contains(err.Error(), "catalog_db_url") {
			t.Errorf("error should mention catalog_db_url, got: %v", err)
		}
	})

	t.Run("missing pg_encryption_key returns error", func(t *testing.T) {
		spec := makeLakekeeperSpec("postgres://lakekeeper:secret@pg-host:5432/lakekeeper?sslmode=disable", "")
		_, err := o.generateLakekeeperInstanceResources(spec)
		if err == nil {
			t.Fatal("expected error for missing pg_encryption_key, got nil")
		}
		if !strings.Contains(err.Error(), "pg_encryption_key") {
			t.Errorf("error should mention pg_encryption_key, got: %v", err)
		}
	})

	t.Run("both missing returns error", func(t *testing.T) {
		spec := makeLakekeeperSpec("", "")
		_, err := o.generateLakekeeperInstanceResources(spec)
		if err == nil {
			t.Fatal("expected error for missing config keys, got nil")
		}
	})
}

// TestGenerateLakekeeperInstanceResources_MultiNodeRejected verifies that the
// orchestrator refuses to generate resources for a lakekeeper (ColdFront)
// service on a database that spans more than one node. This is a
// defence-in-depth guard behind the API-layer validateColdFrontSingleNode
// check; multi-node ColdFront is unsupported until mesh snowflake.node
// reconciliation lands.
func TestGenerateLakekeeperInstanceResources_MultiNodeRejected(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)

	makeMultiNode := func(nodeCount int) *database.ServiceInstanceSpec {
		spec := makeLakekeeperSpec(
			"postgres://lakekeeper:secret@pg-host:5432/lakekeeper?sslmode=disable",
			"dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==",
		)
		spec.DatabaseNodes = make([]*database.NodeInstances, nodeCount)
		for i := range spec.DatabaseNodes {
			spec.DatabaseNodes[i] = &database.NodeInstances{
				NodeName: fmt.Sprintf("n%d", i+1),
			}
		}
		return spec
	}

	t.Run("two nodes returns error", func(t *testing.T) {
		_, err := o.generateLakekeeperInstanceResources(makeMultiNode(2))
		if err == nil {
			t.Fatal("expected error for multi-node ColdFront, got nil")
		}
		if !strings.Contains(err.Error(), "multi-node ColdFront is not yet supported") {
			t.Errorf("error should mention multi-node ColdFront, got: %v", err)
		}
	})

	t.Run("single node is accepted", func(t *testing.T) {
		if _, err := o.generateLakekeeperInstanceResources(makeMultiNode(1)); err != nil {
			t.Fatalf("single-node ColdFront should be accepted, got: %v", err)
		}
	})
}

// TestGenerateLakekeeperInstanceResources_ResourceGraph verifies that, in
// external-catalog mode, the generated graph contains no standalone migrate
// resource (serve self-migrates via MIGRATE_BEFORE_SERVE) and the serve
// ServiceInstanceSpec depends only on the database network and the lakekeeper
// config resource — no migrate, no managed catalog DB.
func TestGenerateLakekeeperInstanceResources_ResourceGraph(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)

	spec := makeLakekeeperSpec(
		"postgres://lakekeeper:secret@pg-host:5432/lakekeeper?sslmode=disable",
		"dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==",
	)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	specID := ServiceInstanceSpecResourceIdentifier(spec.ServiceInstanceID)
	var specRD *resource.ResourceData
	for _, rd := range result.Resources {
		if rd.Identifier == specID {
			specRD = rd
			break
		}
	}
	if specRD == nil {
		t.Fatal("ServiceInstanceSpecResource not found in resource graph")
	}
	specRes, err := resource.ToResource[*ServiceInstanceSpecResource](specRD)
	if err != nil {
		t.Fatalf("failed to decode ServiceInstanceSpecResource: %v", err)
	}

	want := map[resource.Identifier]bool{
		NetworkResourceIdentifier(specRes.DatabaseNetworkID):       true,
		LakekeeperConfigResourceIdentifier(spec.ServiceInstanceID): true,
	}
	deps := specRes.Dependencies()
	if len(deps) != len(want) {
		t.Errorf("serve Dependencies() = %v, want exactly %v", deps, want)
	}
	for _, d := range deps {
		if !want[d] {
			t.Errorf("serve Dependencies() contains unexpected %v; want only %v", d, want)
		}
	}
}
