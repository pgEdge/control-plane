package swarm

import (
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// newTestOrchestrator returns an Orchestrator with serviceVersions initialised
// from a minimal config, suitable for unit tests that call generateRAGInstanceResources.
func newTestOrchestrator() *Orchestrator {
	return &Orchestrator{
		serviceVersions: NewServiceVersions(config.Config{}),
	}
}

// minimalRAGConfig returns a minimal valid RAG service config suitable for unit tests.
func minimalRAGConfig() map[string]any {
	return map[string]any{
		"pipelines": []any{
			map[string]any{
				"name": "default",
				"tables": []any{
					map[string]any{
						"table":         "docs",
						"text_column":   "content",
						"vector_column": "embedding",
					},
				},
				"embedding_llm": map[string]any{
					"provider": "openai",
					"model":    "text-embedding-3-small",
					"api_key":  "sk-embed",
				},
				"rag_llm": map[string]any{
					"provider": "anthropic",
					"model":    "claude-sonnet-4-5",
					"api_key":  "sk-ant",
				},
			},
		},
	}
}

func TestGenerateRAGInstanceResources_ResourceList(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	result, err := o.generateRAGInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateRAGInstanceResources() error = %v", err)
	}

	if result.ServiceInstance == nil {
		t.Fatal("ServiceInstance is nil")
	}
	if result.ServiceInstance.ServiceInstanceID != spec.ServiceInstanceID {
		t.Errorf("ServiceInstance.ServiceInstanceID = %q, want %q",
			result.ServiceInstance.ServiceInstanceID, spec.ServiceInstanceID)
	}
	if result.ServiceInstance.HostID != spec.HostID {
		t.Errorf("ServiceInstance.HostID = %q, want %q",
			result.ServiceInstance.HostID, spec.HostID)
	}
	if result.ServiceInstance.State != database.ServiceInstanceStateCreating {
		t.Errorf("ServiceInstance.State = %q, want %q",
			result.ServiceInstance.State, database.ServiceInstanceStateCreating)
	}

	// Single node: Network + canonical RO + DirResource + Keys + Config + InstanceSpec + ServiceInstance = 7.
	if len(result.Resources) != 7 {
		t.Fatalf("len(Resources) = %d, want 7", len(result.Resources))
	}
	if result.Resources[0].Identifier.Type != ResourceTypeNetwork {
		t.Errorf("Resources[0].Identifier.Type = %q, want %q",
			result.Resources[0].Identifier.Type, ResourceTypeNetwork)
	}
	if result.Resources[1].Identifier.Type != ResourceTypeServiceUserRole {
		t.Errorf("Resources[1].Identifier.Type = %q, want %q",
			result.Resources[1].Identifier.Type, ResourceTypeServiceUserRole)
	}
	wantID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	if result.Resources[1].Identifier != wantID {
		t.Errorf("Resources[1].Identifier = %v, want %v", result.Resources[1].Identifier, wantID)
	}
	if result.Resources[2].Identifier.Type != filesystem.ResourceTypeDir {
		t.Errorf("Resources[2].Identifier.Type = %q, want %q",
			result.Resources[2].Identifier.Type, filesystem.ResourceTypeDir)
	}
	if result.Resources[3].Identifier.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Resources[3].Identifier.Type = %q, want %q",
			result.Resources[3].Identifier.Type, ResourceTypeRAGServiceKeys)
	}
	if result.Resources[4].Identifier.Type != ResourceTypeRAGConfig {
		t.Errorf("Resources[4].Identifier.Type = %q, want %q",
			result.Resources[4].Identifier.Type, ResourceTypeRAGConfig)
	}
	if result.Resources[5].Identifier.Type != ResourceTypeServiceInstanceSpec {
		t.Errorf("Resources[5].Identifier.Type = %q, want %q",
			result.Resources[5].Identifier.Type, ResourceTypeServiceInstanceSpec)
	}
	if result.Resources[6].Identifier.Type != ResourceTypeServiceInstance {
		t.Errorf("Resources[6].Identifier.Type = %q, want %q",
			result.Resources[6].Identifier.Type, ResourceTypeServiceInstance)
	}
}

func TestGenerateRAGInstanceResources_MultiNode(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-1",
		NodeName:     "n1",
		DatabaseNodes: []*database.NodeInstances{
			{NodeName: "n1"},
			{NodeName: "n2"},
			{NodeName: "n3"},
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateRAGInstanceResources() error = %v", err)
	}

	// 3 nodes → Network + canonical(n1) + per-node(n2) + per-node(n3) + dir + keys + config + spec + instance = 9.
	if len(result.Resources) != 9 {
		t.Fatalf("len(Resources) = %d, want 9", len(result.Resources))
	}
	// Resources[0] is Network; Resources[1..3] are ServiceUserRole resources.
	for i := 1; i < 4; i++ {
		if result.Resources[i].Identifier.Type != ResourceTypeServiceUserRole {
			t.Errorf("resource[%d] type = %q, want %q", i, result.Resources[i].Identifier.Type, ResourceTypeServiceUserRole)
		}
	}

	// Canonical is index 1 and has no CredentialSource
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[1])
	if err != nil {
		t.Fatalf("ToResource canonical: %v", err)
	}
	if canonical.CredentialSource != nil {
		t.Errorf("canonical resource should have nil CredentialSource, got %v", canonical.CredentialSource)
	}
	if canonical.Mode != ServiceUserRoleRO {
		t.Errorf("canonical Mode = %q, want %q", canonical.Mode, ServiceUserRoleRO)
	}

	// Per-node resources point back to canonical
	canonicalID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	for i, rd := range result.Resources[2:4] {
		perNode, err := resource.ToResource[*ServiceUserRole](rd)
		if err != nil {
			t.Fatalf("ToResource per-node[%d]: %v", i, err)
		}
		if perNode.CredentialSource == nil || *perNode.CredentialSource != canonicalID {
			t.Errorf("per-node[%d].CredentialSource = %v, want %v", i, perNode.CredentialSource, canonicalID)
		}
		if perNode.Mode != ServiceUserRoleRO {
			t.Errorf("per-node[%d].Mode = %q, want %q", i, perNode.Mode, ServiceUserRoleRO)
		}
	}

	// Dir, keys, config, spec, and instance are appended last.
	if result.Resources[4].Identifier.Type != filesystem.ResourceTypeDir {
		t.Errorf("Resources[4].Identifier.Type = %q, want %q",
			result.Resources[4].Identifier.Type, filesystem.ResourceTypeDir)
	}
	if result.Resources[5].Identifier.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Resources[5].Identifier.Type = %q, want %q",
			result.Resources[5].Identifier.Type, ResourceTypeRAGServiceKeys)
	}
	if result.Resources[6].Identifier.Type != ResourceTypeRAGConfig {
		t.Errorf("Resources[6].Identifier.Type = %q, want %q",
			result.Resources[6].Identifier.Type, ResourceTypeRAGConfig)
	}
	if result.Resources[7].Identifier.Type != ResourceTypeServiceInstanceSpec {
		t.Errorf("Resources[7].Identifier.Type = %q, want %q",
			result.Resources[7].Identifier.Type, ResourceTypeServiceInstanceSpec)
	}
	if result.Resources[8].Identifier.Type != ResourceTypeServiceInstance {
		t.Errorf("Resources[8].Identifier.Type = %q, want %q",
			result.Resources[8].Identifier.Type, ResourceTypeServiceInstance)
	}
}

func TestGenerateRAGInstanceResources_MultiNode_CanonicalNotFirst(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host2",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-2",
		NodeName:     "n2", // canonical is n2, not at index 0
		DatabaseNodes: []*database.NodeInstances{
			{NodeName: "n1"},
			{NodeName: "n2"},
			{NodeName: "n3"},
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateRAGInstanceResources() error = %v", err)
	}

	// 3 nodes → Network + canonical(n2) + per-node(n1) + per-node(n3) + dir + keys + config + spec + instance = 9.
	if len(result.Resources) != 9 {
		t.Fatalf("len(Resources) = %d, want 9", len(result.Resources))
	}

	// Canonical (index 1, after Network) must be n2 with no CredentialSource
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[1])
	if err != nil {
		t.Fatalf("ToResource canonical: %v", err)
	}
	if canonical.CredentialSource != nil {
		t.Errorf("canonical resource should have nil CredentialSource, got %v", canonical.CredentialSource)
	}
	if canonical.NodeName != "n2" {
		t.Errorf("canonical NodeName = %q, want %q", canonical.NodeName, "n2")
	}

	// Per-node resources must cover n1 and n3, not n2
	canonicalID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	perNodeNames := make(map[string]bool)
	for i, rd := range result.Resources[2:4] {
		perNode, err := resource.ToResource[*ServiceUserRole](rd)
		if err != nil {
			t.Fatalf("ToResource per-node[%d]: %v", i, err)
		}
		if perNode.CredentialSource == nil || *perNode.CredentialSource != canonicalID {
			t.Errorf("per-node[%d].CredentialSource = %v, want %v", i, perNode.CredentialSource, canonicalID)
		}
		perNodeNames[perNode.NodeName] = true
	}
	if perNodeNames["n2"] {
		t.Error("canonical node n2 must not appear in per-node resources")
	}
	if !perNodeNames["n1"] || !perNodeNames["n3"] {
		t.Errorf("per-node resources = %v, want n1 and n3", perNodeNames)
	}
}

func TestGenerateServiceInstanceResources_RAGDispatch(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:   "db1",
		DatabaseName: "db1",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	result, err := o.GenerateServiceInstanceResources(spec)
	if err != nil {
		t.Fatalf("GenerateServiceInstanceResources() error = %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestGenerateServiceInstanceResources_UnknownTypeReturnsError(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-unknown-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "unknown",
			ServiceType: "unknown",
			Version:     "latest",
		},
		DatabaseID:   "db1",
		DatabaseName: "db1",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	_, err := o.GenerateServiceInstanceResources(spec)
	if err == nil {
		t.Fatal("expected error for unknown service type, got nil")
	}
}

func TestGenerateRAGInstanceResources_IncompatibleVersion(t *testing.T) {
	o := newTestOrchestrator()
	// Override the "rag/latest" image with a constraint requiring PG >= 18.
	o.serviceVersions.addServiceImage("rag", "latest", &ServiceImage{
		Tag: "rag-server:latest",
		PostgresConstraint: &ds.VersionConstraint{
			Min: ds.MustParseVersion("18"),
		},
	})

	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:    "db1",
		DatabaseName:  "db1",
		HostID:        "host-1",
		NodeName:      "n1",
		PgEdgeVersion: ds.MustPgEdgeVersion("17", "5.0.0"),
	}

	_, err := o.generateRAGInstanceResources(spec)
	if err == nil {
		t.Fatal("expected compatibility error, got nil")
	}
	if !strings.Contains(err.Error(), "not compatible") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not compatible")
	}
}
