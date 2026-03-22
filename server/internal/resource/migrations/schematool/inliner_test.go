package main

import (
	"strings"
	"testing"
)

func makePkg(src string) (*PackageIndex, error) {
	return indexPackage(map[string][]byte{"test.go": []byte(src)})
}

func TestInlineSimpleStruct(t *testing.T) {
	src := `
package example
type Inner struct {
    X int ` + "`" + `json:"x"` + "`" + `
}
type Outer struct {
    A string ` + "`" + `json:"a"` + "`" + `
    B Inner  ` + "`" + `json:"b"` + "`" + `
}`
	idx, err := makePkg(src)
	if err != nil {
		t.Fatalf("makePkg: %v", err)
	}
	in := &Inliner{resolver: nil, neededImports: make(map[string]string)}
	result, err := in.structBody(idx.Structs["Outer"], idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody: %v", err)
	}
	if !strings.Contains(result, "struct {") {
		t.Errorf("expected inline anonymous struct, got:\n%s", result)
	}
	if strings.Contains(result, "Inner") {
		t.Errorf("Inner should be inlined, not referenced by name, got:\n%s", result)
	}
}

func TestInlineExternalType(t *testing.T) {
	src := `
package example
import "encoding/json"
type Foo struct {
    Raw json.RawMessage ` + "`" + `json:"raw"` + "`" + `
}`
	idx, err := makePkg(src)
	if err != nil {
		t.Fatalf("makePkg: %v", err)
	}
	in := &Inliner{resolver: nil, neededImports: make(map[string]string)}
	result, err := in.structBody(idx.Structs["Foo"], idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody: %v", err)
	}
	if !strings.Contains(result, "json.RawMessage") {
		t.Errorf("expected json.RawMessage in output, got:\n%s", result)
	}
	if in.neededImports["json"] != "encoding/json" {
		t.Errorf("expected encoding/json import, got: %v", in.neededImports)
	}
}

func TestInlinePointerAndSlice(t *testing.T) {
	src := `
package example
type Item struct {
    V string ` + "`" + `json:"v"` + "`" + `
}
type Container struct {
    Ptr   *Item  ` + "`" + `json:"ptr"` + "`" + `
    Slice []Item ` + "`" + `json:"slice"` + "`" + `
}`
	idx, err := makePkg(src)
	if err != nil {
		t.Fatalf("makePkg: %v", err)
	}
	in := &Inliner{resolver: nil, neededImports: make(map[string]string)}
	result, err := in.structBody(idx.Structs["Container"], idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody: %v", err)
	}
	if !strings.Contains(result, "*struct {") {
		t.Errorf("expected *struct{} for Ptr field, got:\n%s", result)
	}
	if !strings.Contains(result, "[]struct {") {
		t.Errorf("expected []struct{} for Slice field, got:\n%s", result)
	}
}

func TestInlineTypeAlias(t *testing.T) {
	src := `
package example
type MyString string
type Foo struct {
    Name MyString ` + "`" + `json:"name"` + "`" + `
}`
	idx, err := makePkg(src)
	if err != nil {
		t.Fatalf("makePkg: %v", err)
	}
	in := &Inliner{resolver: nil, neededImports: make(map[string]string)}
	result, err := in.structBody(idx.Structs["Foo"], idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody: %v", err)
	}
	if !strings.Contains(result, "string") {
		t.Errorf("expected MyString to resolve to string, got:\n%s", result)
	}
}

func TestInlineCycleDetection(t *testing.T) {
	// Go does not allow directly self-referential structs (it would be infinite size),
	// but a pointer to self is valid and the inliner should not loop.
	src := `
package example
type Node struct {
	Value string ` + "`" + `json:"value"` + "`" + `
	Next  *Node  ` + "`" + `json:"next"` + "`" + `
}`
	idx, err := makePkg(src)
	if err != nil {
		t.Fatalf("makePkg: %v", err)
	}
	in := &Inliner{resolver: nil, neededImports: make(map[string]string)}
	result, err := in.structBody(idx.Structs["Node"], idx, make(map[string]bool))
	if err != nil {
		t.Fatalf("structBody should not error on self-referential type: %v", err)
	}
	// The Next field's type should contain cycle-break output, not loop forever.
	if !strings.Contains(result, "Next") {
		t.Errorf("expected Next field in output, got:\n%s", result)
	}
	t.Logf("Cycle output:\n%s", result)
}
