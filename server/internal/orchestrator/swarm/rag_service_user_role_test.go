package swarm

import (
	"context"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestRAGServiceUserRole_ResourceVersion(t *testing.T) {
	r := &RAGServiceUserRole{}
	if got := r.ResourceVersion(); got != "1" {
		t.Errorf("ResourceVersion() = %q, want %q", got, "1")
	}
}

func TestRAGServiceUserRole_Identifier(t *testing.T) {
	r := &RAGServiceUserRole{ServiceID: "rag"}
	id := r.Identifier()
	if id.ID != "rag" {
		t.Errorf("Identifier().ID = %q, want %q", id.ID, "rag")
	}
	if id.Type != ResourceTypeRAGServiceUserRole {
		t.Errorf("Identifier().Type = %q, want %q", id.Type, ResourceTypeRAGServiceUserRole)
	}
}

func TestRAGServiceUserRole_Executor(t *testing.T) {
	r := &RAGServiceUserRole{NodeName: "n1"}
	exec := r.Executor()
	if exec != resource.PrimaryExecutor("n1") {
		t.Errorf("Executor() = %v, want PrimaryExecutor(%q)", exec, "n1")
	}
}

func TestRAGServiceUserRole_DiffIgnore(t *testing.T) {
	r := &RAGServiceUserRole{}
	ignored := r.DiffIgnore()
	want := map[string]bool{
		"/node_name": true,
		"/username":  true,
		"/password":  true,
	}
	if len(ignored) != len(want) {
		t.Errorf("DiffIgnore() length = %d, want %d", len(ignored), len(want))
	}
	for _, path := range ignored {
		if !want[path] {
			t.Errorf("unexpected path in DiffIgnore(): %q", path)
		}
	}
}

func TestRAGServiceUserRole_RefreshEmptyCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"empty username", "", "somepassword"},
		{"empty password", "svc_inst", ""},
		{"both empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RAGServiceUserRole{
				ServiceID: "rag",
				Username:  tt.username,
				Password:  tt.password,
			}
			// Refresh with nil rc — the empty-credential guard fires before any
			// injection call, so no injector is needed.
			err := r.Refresh(context.Background(), nil)
			if err != resource.ErrNotFound {
				t.Errorf("Refresh() = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRAGServiceUserRoleIdentifier(t *testing.T) {
	id := RAGServiceUserRoleIdentifier("my-instance")
	if id.ID != "my-instance" {
		t.Errorf("ID = %q, want %q", id.ID, "my-instance")
	}
	if id.Type != ResourceTypeRAGServiceUserRole {
		t.Errorf("Type = %q, want %q", id.Type, ResourceTypeRAGServiceUserRole)
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
	o := &Orchestrator{}
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

	// Single node: canonical RO ServiceUserRole + data DirResource + RAGServiceKeysResource.
	if len(result.Resources) != 3 {
		t.Fatalf("len(Resources) = %d, want 3", len(result.Resources))
	}
	if result.Resources[0].Identifier.Type != ResourceTypeServiceUserRole {
		t.Errorf("Resources[0].Identifier.Type = %q, want %q",
			result.Resources[0].Identifier.Type, ResourceTypeServiceUserRole)
	}
	wantID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	if result.Resources[0].Identifier != wantID {
		t.Errorf("Resources[0].Identifier = %v, want %v", result.Resources[0].Identifier, wantID)
	}
	if result.Resources[1].Identifier.Type != filesystem.ResourceTypeDir {
		t.Errorf("Resources[1].Identifier.Type = %q, want %q",
			result.Resources[1].Identifier.Type, filesystem.ResourceTypeDir)
	}
	if result.Resources[2].Identifier.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Resources[2].Identifier.Type = %q, want %q",
			result.Resources[2].Identifier.Type, ResourceTypeRAGServiceKeys)
	}
}

func TestGenerateRAGInstanceResources_MultiNode(t *testing.T) {
	o := &Orchestrator{}
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

	// 3 nodes → canonical(n1) + per-node(n2) + per-node(n3) + data dir + keys = 5 resources.
	if len(result.Resources) != 5 {
		t.Fatalf("len(Resources) = %d, want 5", len(result.Resources))
	}
	// First three must be ServiceUserRole resources.
	for i := 0; i < 3; i++ {
		if result.Resources[i].Identifier.Type != ResourceTypeServiceUserRole {
			t.Errorf("resource[%d] type = %q, want %q", i, result.Resources[i].Identifier.Type, ResourceTypeServiceUserRole)
		}
	}

	// Canonical is first and has no CredentialSource
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[0])
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
	for i, rd := range result.Resources[1:3] {
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

	// Data dir and keys resource are appended last.
	if result.Resources[3].Identifier.Type != filesystem.ResourceTypeDir {
		t.Errorf("Resources[3].Identifier.Type = %q, want %q",
			result.Resources[3].Identifier.Type, filesystem.ResourceTypeDir)
	}
	if result.Resources[4].Identifier.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Resources[4].Identifier.Type = %q, want %q",
			result.Resources[4].Identifier.Type, ResourceTypeRAGServiceKeys)
	}
}

func TestGenerateRAGInstanceResources_MultiNode_CanonicalNotFirst(t *testing.T) {
	o := &Orchestrator{}
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

	// 3 nodes → canonical(n2) + per-node(n1) + per-node(n3) + data dir + keys = 5 resources.
	if len(result.Resources) != 5 {
		t.Fatalf("len(Resources) = %d, want 5", len(result.Resources))
	}

	// Canonical (index 0) must be n2 with no CredentialSource
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[0])
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
	for i, rd := range result.Resources[1:3] {
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
	o := &Orchestrator{}
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
	o := &Orchestrator{}
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
