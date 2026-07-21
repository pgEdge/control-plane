package swarm

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// makeManagedLakekeeperSpec returns a ServiceInstanceSpec configured for a
// control-plane-managed catalog database (catalog_db_create: true, no
// catalog_db_url). It reuses the same fixture shape as makeLakekeeperSpec,
// adding the fields a managed catalog needs: DatabaseHosts and connect-as
// credentials.
func makeManagedLakekeeperSpec() *database.ServiceInstanceSpec {
	spec := makeLakekeeperSpec("", "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==")
	spec.ServiceSpec.Config["catalog_db_create"] = true
	spec.DatabaseName = "mydb"
	spec.NodeName = "n1"
	spec.DatabaseHosts = []database.ServiceHostEntry{
		{Host: "postgres-inst1", Port: 5432},
	}
	spec.ConnectAsUsername = "app_user"
	spec.ConnectAsPassword = "app_password"
	return spec
}

// findResourceByType returns the first ResourceData in resources whose
// Identifier.Type matches t, or nil if none is found.
func findResourceByType(resources []*resource.ResourceData, t resource.Type) *resource.ResourceData {
	for _, rd := range resources {
		if rd.Identifier.Type == t {
			return rd
		}
	}
	return nil
}

// TestGenerateLakekeeperInstanceResources_ManagedCatalog verifies that when
// catalog_db_create is set, the orchestrator provisions a
// LakekeeperCatalogDBResource, builds the catalog URL itself, threads the
// injected config into every downstream consumer, and wires the migrate
// resource's dependency on the catalog resource.
func TestGenerateLakekeeperInstanceResources_ManagedCatalog(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeManagedLakekeeperSpec()

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	wantCatalogDBName := "mydb_lakekeeper"
	wantCatalogDBURL := buildManagedCatalogDBURL(
		spec.DatabaseHosts[0], spec.ConnectAsUsername, spec.ConnectAsPassword, wantCatalogDBName)

	// The graph must contain a LakekeeperCatalogDBResource with the derived
	// name/owner.
	catalogRD := findResourceByType(result.Resources, ResourceTypeLakekeeperCatalogDB)
	if catalogRD == nil {
		t.Fatal("resource graph missing LakekeeperCatalogDBResource")
	}
	catalogRes, err := resource.ToResource[*LakekeeperCatalogDBResource](catalogRD)
	if err != nil {
		t.Fatalf("failed to decode LakekeeperCatalogDBResource: %v", err)
	}
	if catalogRes.CatalogDBName != wantCatalogDBName {
		t.Errorf("CatalogDBName = %q, want %q", catalogRes.CatalogDBName, wantCatalogDBName)
	}
	if catalogRes.CatalogDBOwner != spec.ConnectAsUsername {
		t.Errorf("CatalogDBOwner = %q, want %q", catalogRes.CatalogDBOwner, spec.ConnectAsUsername)
	}

	// serve self-migrates on first start (MIGRATE_BEFORE_SERVE); the standalone
	// migrate resource has been removed. The serve ServiceInstanceSpecResource
	// must instead depend on the catalog DB resource so serve's in-process
	// migration only runs after control-plane has created the catalog database.
	// Its config copy must also carry the built catalog_db_url, proving the
	// serve container env gets the derived URL (not the caller-supplied one,
	// since there wasn't one).
	specRD := findResourceByType(result.Resources, ResourceTypeServiceInstanceSpec)
	if specRD == nil {
		t.Fatal("resource graph missing ServiceInstanceSpecResource")
	}
	specRes, err := resource.ToResource[*ServiceInstanceSpecResource](specRD)
	if err != nil {
		t.Fatalf("failed to decode ServiceInstanceSpecResource: %v", err)
	}

	catalogID := LakekeeperCatalogDBResourceIdentifier(spec.ServiceInstanceID)
	foundDep := false
	for _, d := range specRes.Dependencies() {
		if d == catalogID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Errorf("serve ServiceInstanceSpecResource.Dependencies() missing catalog identifier %v; got: %v",
			catalogID, specRes.Dependencies())
	}

	gotURL, _ := specRes.ServiceSpec.Config["catalog_db_url"].(string)
	if gotURL != wantCatalogDBURL {
		t.Errorf("ServiceInstanceSpecResource.ServiceSpec.Config[catalog_db_url] = %q, want %q",
			gotURL, wantCatalogDBURL)
	}

	// The bootstrap and storage-secret resources alias the same service Config
	// map; the brief names all four consumers as load-bearing, so pin that both
	// observe the injected catalog_db_url. Guards against a future regression
	// pointing either back at the un-injected spec.ServiceSpec.Config.
	bootstrapRD := findResourceByType(result.Resources, ResourceTypeLakekeeperBootstrap)
	if bootstrapRD == nil {
		t.Fatal("resource graph missing LakekeeperBootstrapResource")
	}
	bootstrapRes, err := resource.ToResource[*LakekeeperBootstrapResource](bootstrapRD)
	if err != nil {
		t.Fatalf("failed to decode LakekeeperBootstrapResource: %v", err)
	}
	if got, _ := bootstrapRes.Config["catalog_db_url"].(string); got != wantCatalogDBURL {
		t.Errorf("LakekeeperBootstrapResource.Config[catalog_db_url] = %q, want %q", got, wantCatalogDBURL)
	}

	secretRD := findResourceByType(result.Resources, ResourceTypeLakekeeperStorageSecret)
	if secretRD == nil {
		t.Fatal("resource graph missing LakekeeperStorageSecretResource")
	}
	secretRes, err := resource.ToResource[*LakekeeperStorageSecretResource](secretRD)
	if err != nil {
		t.Fatalf("failed to decode LakekeeperStorageSecretResource: %v", err)
	}
	if got, _ := secretRes.Config["catalog_db_url"].(string); got != wantCatalogDBURL {
		t.Errorf("LakekeeperStorageSecretResource.Config[catalog_db_url] = %q, want %q", got, wantCatalogDBURL)
	}
}

// TestGenerateLakekeeperInstanceResources_ExternalCatalogRegression verifies
// that supplying an external catalog_db_url (no catalog_db_create) leaves
// behavior byte-identical to the pre-managed-catalog code path: no catalog
// resource is generated and the migrate resource has no dependencies.
func TestGenerateLakekeeperInstanceResources_ExternalCatalogRegression(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	const externalURL = "postgres://lakekeeper:secret@pg-host:5432/lakekeeper?sslmode=disable"
	spec := makeLakekeeperSpec(
		externalURL,
		"dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==",
	)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	if rd := findResourceByType(result.Resources, ResourceTypeLakekeeperCatalogDB); rd != nil {
		t.Errorf("resource graph should not contain a LakekeeperCatalogDBResource in external mode; got: %+v", rd)
	}

	specRD := findResourceByType(result.Resources, ResourceTypeServiceInstanceSpec)
	if specRD == nil {
		t.Fatal("resource graph missing ServiceInstanceSpecResource")
	}
	specRes, err := resource.ToResource[*ServiceInstanceSpecResource](specRD)
	if err != nil {
		t.Fatalf("failed to decode ServiceInstanceSpecResource: %v", err)
	}

	// In external mode there is no control-plane-managed catalog DB resource, so
	// the serve spec must NOT depend on one (the catalog URL is validated at spec
	// time). A stray catalog dependency here would be unsatisfiable.
	catalogID := LakekeeperCatalogDBResourceIdentifier(spec.ServiceInstanceID)
	for _, d := range specRes.Dependencies() {
		if d == catalogID {
			t.Errorf("serve ServiceInstanceSpecResource.Dependencies() must not include catalog identifier %v in external mode; got: %v",
				catalogID, specRes.Dependencies())
		}
	}

	// Positive assertion: the caller-supplied URL must pass through unchanged
	// (control-plane builds nothing in external mode) to the serve container's
	// config.
	if got, _ := specRes.ServiceSpec.Config["catalog_db_url"].(string); got != externalURL {
		t.Errorf("ServiceInstanceSpecResource config catalog_db_url = %q, want caller URL %q", got, externalURL)
	}
}

// TestGenerateLakekeeperInstanceResources_ManagedCatalogNoHosts verifies that
// catalog_db_create without any DatabaseHosts fails loudly, naming the
// service ID, rather than producing a resource graph with an empty catalog URL.
func TestGenerateLakekeeperInstanceResources_ManagedCatalogNoHosts(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeManagedLakekeeperSpec()
	spec.DatabaseHosts = nil

	_, err := o.generateLakekeeperInstanceResources(spec)
	if err == nil {
		t.Fatal("expected error for catalog_db_create with no database hosts, got nil")
	}
	if !strings.Contains(err.Error(), spec.ServiceSpec.ServiceID) {
		t.Errorf("error should name the service ID %q, got: %v", spec.ServiceSpec.ServiceID, err)
	}
}

// TestGenerateLakekeeperInstanceResources_FailLoudRegression verifies that
// omitting both catalog_db_url and catalog_db_create still errors, and that
// the updated message documents both the external and managed paths.
func TestGenerateLakekeeperInstanceResources_FailLoudRegression(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeLakekeeperSpec("", "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==")

	_, err := o.generateLakekeeperInstanceResources(spec)
	if err == nil {
		t.Fatal("expected error for missing catalog_db_url and catalog_db_create, got nil")
	}
	if !strings.Contains(err.Error(), "catalog_db_url") {
		t.Errorf("error should mention catalog_db_url, got: %v", err)
	}
	if !strings.Contains(err.Error(), "catalog_db_create") {
		t.Errorf("error should mention catalog_db_create, got: %v", err)
	}
}
