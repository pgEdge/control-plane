// integration_test.go
package main

import (
	"strings"
	"testing"
)

func TestIntegration_ResourceData(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}

	resolver, err := newResolver(tree)
	if err != nil {
		t.Fatalf("newResolver: %v", err)
	}

	idx, err := indexPackageFromTree(tree, "server/internal/resource")
	if err != nil {
		t.Fatalf("indexPackageFromTree: %v", err)
	}
	resolver.cache["server/internal/resource"] = idx

	inliner := &Inliner{
		resolver:      resolver,
		neededImports: make(map[string]string),
	}

	st, ok := idx.Structs["ResourceData"]
	if !ok {
		t.Fatal("ResourceData not found in server/internal/resource")
	}

	body, err := inliner.structBody(st, idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody: %v", err)
	}

	out, err := buildOutput("migrations", inliner.neededImports, []namedType{
		{Name: "ResourceData", Body: body},
	})
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}

	src := string(out)

	if !strings.Contains(src, "package migrations") {
		t.Error("missing package declaration")
	}
	// ResourceData has an Executor field (in-repo struct) — must be inlined, not referenced.
	if strings.Contains(src, "resource.Executor") {
		t.Error("Executor should be inlined, not referenced as resource.Executor")
	}
	// ResourceData has an Attributes json.RawMessage field — external, must appear as-is.
	if !strings.Contains(src, "json.RawMessage") {
		t.Error("expected json.RawMessage (external type) to appear unchanged")
	}
	// encoding/json import must be present.
	if !strings.Contains(src, `"encoding/json"`) {
		t.Error("expected encoding/json import")
	}

	t.Logf("Generated output:\n%s", src)
}
