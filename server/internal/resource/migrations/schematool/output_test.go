package main

import (
	"strings"
	"testing"
)

func TestBuildOutput(t *testing.T) {
	imports := map[string]string{
		"json": "encoding/json",
	}
	types := []namedType{
		{
			Name: "Foo",
			Body: "struct {\n\tRaw json.RawMessage `json:\"raw\"`\n}",
		},
	}
	out, err := buildOutput("migrations", imports, types)
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	src := string(out)
	if !strings.Contains(src, "package migrations") {
		t.Error("missing package declaration")
	}
	if !strings.Contains(src, `"encoding/json"`) {
		t.Error("missing encoding/json import")
	}
	if !strings.Contains(src, "type Foo struct") {
		t.Error("missing type Foo struct")
	}
}

func TestBuildOutput_AliasedImport(t *testing.T) {
	imports := map[string]string{
		"myjson": "encoding/json",
	}
	types := []namedType{
		{Name: "Bar", Body: "struct {\n\tX myjson.RawMessage `json:\"x\"`\n}"},
	}
	out, err := buildOutput("mypkg", imports, types)
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	src := string(out)
	// Alias differs from last path segment ("json"), so must be explicit.
	if !strings.Contains(src, `myjson "encoding/json"`) {
		t.Errorf("expected explicit alias, got:\n%s", src)
	}
}
