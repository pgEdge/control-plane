package swarm

import (
	"context"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestColdfrontExtensionStatements(t *testing.T) {
	stmts := coldfrontExtensionStatements()
	if len(stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(stmts))
	}
	stmt, ok := stmts[0].(postgres.Statement)
	if !ok {
		t.Fatalf("statement 0 is %T, want postgres.Statement", stmts[0])
	}
	// CASCADE is load-bearing: coldfront's control file requires pg_duckdb, so
	// CASCADE auto-installs it. IF NOT EXISTS keeps the step idempotent for the
	// resource's Update path and for images that pre-create the extension.
	want := "CREATE EXTENSION IF NOT EXISTS coldfront CASCADE;"
	if stmt.SQL != want {
		t.Fatalf("coldfront extension SQL = %q, want %q", stmt.SQL, want)
	}
}

func TestLakekeeperColdfrontExtensionResourceRefresh(t *testing.T) {
	r := &LakekeeperColdfrontExtensionResource{}
	if err := r.Refresh(context.Background(), nil); err == nil {
		t.Fatal("expected ErrNotFound before creation")
	}
	r.Created = true
	if err := r.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("expected nil after creation, got %v", err)
	}
}

func TestLakekeeperColdfrontExtensionResourceDependencies(t *testing.T) {
	r := &LakekeeperColdfrontExtensionResource{
		NodeName:     "n1",
		DatabaseName: "mydb",
	}
	deps := r.Dependencies()
	want := database.PostgresDatabaseResourceIdentifier("n1", "mydb")
	if len(deps) != 1 || deps[0] != want {
		t.Fatalf("Dependencies() = %v, want [%v]", deps, want)
	}
}

// TestGenerateLakekeeperInstanceResources_CreatesColdfrontExtension verifies the
// coldfront extension resource is generated for a lakekeeper service and that
// the storage-secret resource depends on it — closing the gap where the
// storage-secret step assumed the extension existed but nothing created it.
func TestGenerateLakekeeperInstanceResources_CreatesColdfrontExtension(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)

	// Cover both catalog modes: the extension is needed regardless of whether
	// the catalog DB is managed or external.
	cases := map[string]*database.ServiceInstanceSpec{
		"managed":  makeManagedLakekeeperSpec(),
		"external": makeLakekeeperSpec("postgres://lakekeeper:secret@pg-host:5432/lakekeeper?sslmode=disable", "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA=="),
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := o.generateLakekeeperInstanceResources(spec)
			if err != nil {
				t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
			}

			extRD := findResourceByType(result.Resources, ResourceTypeLakekeeperColdfrontExtension)
			if extRD == nil {
				t.Fatal("resource graph missing LakekeeperColdfrontExtensionResource")
			}
			extRes, err := resource.ToResource[*LakekeeperColdfrontExtensionResource](extRD)
			if err != nil {
				t.Fatalf("failed to decode LakekeeperColdfrontExtensionResource: %v", err)
			}
			if extRes.DatabaseName != spec.DatabaseName {
				t.Errorf("ext DatabaseName = %q, want %q", extRes.DatabaseName, spec.DatabaseName)
			}
			if extRes.NodeName != spec.NodeName {
				t.Errorf("ext NodeName = %q, want %q", extRes.NodeName, spec.NodeName)
			}

			// The extension resource must depend on the app database existing.
			dbID := database.PostgresDatabaseResourceIdentifier(spec.NodeName, spec.DatabaseName)
			if deps := extRes.Dependencies(); len(deps) != 1 || deps[0] != dbID {
				t.Errorf("ext Dependencies() = %v, want [%v]", deps, dbID)
			}

			// The storage-secret resource must now depend on the extension
			// resource, not merely the database — set_storage_secret calls a
			// coldfront function that requires the extension.
			secretRD := findResourceByType(result.Resources, ResourceTypeLakekeeperStorageSecret)
			if secretRD == nil {
				t.Fatal("resource graph missing LakekeeperStorageSecretResource")
			}
			secretRes, err := resource.ToResource[*LakekeeperStorageSecretResource](secretRD)
			if err != nil {
				t.Fatalf("failed to decode LakekeeperStorageSecretResource: %v", err)
			}
			extID := LakekeeperColdfrontExtensionResourceIdentifier(spec.ServiceInstanceID)
			foundDep := false
			for _, d := range secretRes.Dependencies() {
				if d == extID {
					foundDep = true
					break
				}
			}
			if !foundDep {
				t.Errorf("storage-secret Dependencies() missing extension identifier %v; got: %v",
					extID, secretRes.Dependencies())
			}
		})
	}
}
