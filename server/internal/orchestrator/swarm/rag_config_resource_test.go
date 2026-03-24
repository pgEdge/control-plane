package swarm

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestRAGConfigResource_ResourceVersion(t *testing.T) {
	r := &RAGConfigResource{}
	if got := r.ResourceVersion(); got != "1" {
		t.Errorf("ResourceVersion() = %q, want %q", got, "1")
	}
}

func TestRAGConfigResource_Identifier(t *testing.T) {
	r := &RAGConfigResource{ServiceInstanceID: "storefront-rag-host1"}
	id := r.Identifier()
	if id.ID != "storefront-rag-host1" {
		t.Errorf("Identifier().ID = %q, want %q", id.ID, "storefront-rag-host1")
	}
	if id.Type != ResourceTypeRAGConfig {
		t.Errorf("Identifier().Type = %q, want %q", id.Type, ResourceTypeRAGConfig)
	}
}

func TestRAGConfigResourceIdentifier(t *testing.T) {
	id := RAGConfigResourceIdentifier("my-instance")
	if id.ID != "my-instance" {
		t.Errorf("ID = %q, want %q", id.ID, "my-instance")
	}
	if id.Type != ResourceTypeRAGConfig {
		t.Errorf("Type = %q, want %q", id.Type, ResourceTypeRAGConfig)
	}
}

func TestRAGConfigResource_Executor(t *testing.T) {
	r := &RAGConfigResource{HostID: "host-1"}
	exec := r.Executor()
	if exec != resource.HostExecutor("host-1") {
		t.Errorf("Executor() = %v, want HostExecutor(%q)", exec, "host-1")
	}
}

func TestRAGConfigResource_DiffIgnore(t *testing.T) {
	r := &RAGConfigResource{}
	ignored := r.DiffIgnore()
	want := map[string]bool{
		"/username": true,
		"/password": true,
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

func TestRAGConfigResource_Dependencies(t *testing.T) {
	r := &RAGConfigResource{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceID:         "rag",
		DirResourceID:     "storefront-rag-host1-data",
	}
	deps := r.Dependencies()

	if len(deps) != 3 {
		t.Fatalf("Dependencies() len = %d, want 3", len(deps))
	}

	wantDeps := []resource.Identifier{
		filesystem.DirResourceIdentifier("storefront-rag-host1-data"),
		ServiceUserRoleIdentifier("rag", ServiceUserRoleRO),
		RAGServiceKeysResourceIdentifier("storefront-rag-host1"),
	}
	for i, want := range wantDeps {
		if deps[i] != want {
			t.Errorf("Dependencies()[%d] = %v, want %v", i, deps[i], want)
		}
	}
}
