package swarm

import (
	"context"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestRAGServiceUserRole_ResourceVersion(t *testing.T) {
	r := &RAGServiceUserRole{}
	if got := r.ResourceVersion(); got != "1" {
		t.Errorf("ResourceVersion() = %q, want %q", got, "1")
	}
}

func TestRAGServiceUserRole_Identifier(t *testing.T) {
	r := &RAGServiceUserRole{ServiceInstanceID: "db1-rag-host1"}
	id := r.Identifier()
	if id.ID != "db1-rag-host1" {
		t.Errorf("Identifier().ID = %q, want %q", id.ID, "db1-rag-host1")
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
				ServiceInstanceID: "inst1",
				Username:          tt.username,
				Password:          tt.password,
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

	if len(result.Resources) != 2 {
		t.Fatalf("len(Resources) = %d, want 2", len(result.Resources))
	}
	if result.Resources[0].Identifier.Type != ResourceTypeRAGServiceUserRole {
		t.Errorf("Resources[0].Identifier.Type = %q, want %q",
			result.Resources[0].Identifier.Type, ResourceTypeRAGServiceUserRole)
	}
	if result.Resources[1].Identifier.Type != ResourceTypeRAGServiceKeys {
		t.Errorf("Resources[1].Identifier.Type = %q, want %q",
			result.Resources[1].Identifier.Type, ResourceTypeRAGServiceKeys)
	}
}

func TestGenerateRAGInstanceResources_WithCredentials(t *testing.T) {
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
		Credentials: &database.ServiceUser{
			Username: "svc_storefront_rag_host1",
			Password: "supersecret",
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateRAGInstanceResources() error = %v", err)
	}

	// Deserialise the first resource and verify credentials are populated.
	role, err := resource.ToResource[*RAGServiceUserRole](result.Resources[0])
	if err != nil {
		t.Fatalf("ToResource RAGServiceUserRole: %v", err)
	}
	if role.Username != spec.Credentials.Username {
		t.Errorf("Username = %q, want %q", role.Username, spec.Credentials.Username)
	}
	if role.Password != spec.Credentials.Password {
		t.Errorf("Password = %q, want %q", role.Password, spec.Credentials.Password)
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
