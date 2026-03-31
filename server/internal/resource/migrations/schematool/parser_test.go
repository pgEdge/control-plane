package main

import (
	"go/ast"
	"testing"
)

func TestIndexPackage_BasicStruct(t *testing.T) {
	src := map[string][]byte{
		"example.go": []byte(`
package example

import "encoding/json"

type Foo struct {
    Name string          ` + "`" + `json:"name"` + "`" + `
    Raw  json.RawMessage ` + "`" + `json:"raw"` + "`" + `
}

type Bar string
`),
	}

	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}
	if idx.Name != "example" {
		t.Errorf("Name = %q, want %q", idx.Name, "example")
	}
	st, ok := idx.Structs["Foo"]
	if !ok {
		t.Fatal("Foo not found in Structs")
	}
	if st == nil {
		t.Fatal("Foo struct is nil")
	}
	if len(st.Fields.List) != 2 {
		t.Errorf("Foo has %d fields, want 2", len(st.Fields.List))
	}
	_, ok = idx.Structs["Bar"]
	if ok {
		t.Error("Bar should not be in Structs (it is not a struct type)")
	}
	barExpr, ok := idx.Types["Bar"]
	if !ok {
		t.Fatal("Bar not found in Types")
	}
	if _, ok := barExpr.(*ast.Ident); !ok {
		t.Errorf("Bar underlying type is %T, want *ast.Ident", barExpr)
	}
	if idx.Imports["json"] != "encoding/json" {
		t.Errorf("Imports[json] = %q, want %q", idx.Imports["json"], "encoding/json")
	}
}

func TestIndexPackage_ImportAlias(t *testing.T) {
	src := map[string][]byte{
		"example.go": []byte(`
package example

import myjson "encoding/json"

type Foo struct {
    Raw myjson.RawMessage ` + "`" + `json:"raw"` + "`" + `
}
`),
	}
	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}
	if idx.Imports["myjson"] != "encoding/json" {
		t.Errorf("Imports[myjson] = %q, want %q", idx.Imports["myjson"], "encoding/json")
	}
}

func TestIndexPackageFromTree(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	idx, err := indexPackageFromTree(tree, "server/internal/resource")
	if err != nil {
		t.Fatalf("indexPackageFromTree: %v", err)
	}
	if _, ok := idx.Structs["Identifier"]; !ok {
		t.Error("expected Identifier struct in resource package")
	}
	if _, ok := idx.Structs["ResourceData"]; !ok {
		t.Error("expected ResourceData struct in resource package")
	}
}
