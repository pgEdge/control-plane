package main

import (
	"strings"
	"testing"
)

// twoTypeSrc is a package with two resource types each in a separate file,
// each having a single-param identifier function. This mirrors how real
// packages are structured and is used to reproduce the multi-type output bug.
var twoTypeSrc = map[string][]byte{
	"widget.go": []byte(`
package example

import "github.com/pgEdge/control-plane/server/internal/resource"

const ResourceTypeWidget resource.Type = "example.widget"

func WidgetIdentifier(id string) resource.Identifier {
	return resource.Identifier{ID: id, Type: ResourceTypeWidget}
}

type Widget struct {
	ID string ` + "`" + `json:"id"` + "`" + `
}

func (w *Widget) Identifier() resource.Identifier {
	return WidgetIdentifier(w.ID)
}
`),
	"gadget.go": []byte(`
package example

import "github.com/pgEdge/control-plane/server/internal/resource"

const ResourceTypeGadget resource.Type = "example.gadget"

func GadgetIdentifier(id string) resource.Identifier {
	return resource.Identifier{ID: id, Type: ResourceTypeGadget}
}

type Gadget struct {
	ID string ` + "`" + `json:"id"` + "`" + `
}

func (g *Gadget) Identifier() resource.Identifier {
	return GadgetIdentifier(g.ID)
}
`),
}

// TestBuildOutput_MultiParamTypeAlsoGetsIdentifierFunction reproduces the bug
// where types whose identifier function has multiple parameters (ParamName=="")
// were silently skipped. When types are ordered so the first has a single-param
// identifier and the second has a multi-param identifier, only the first type's
// function was output.
func TestBuildOutput_MultiParamTypeAlsoGetsIdentifierFunction(t *testing.T) {
	src := map[string][]byte{
		"node.go": []byte(`
package example

import "github.com/pgEdge/control-plane/server/internal/resource"

const ResourceTypeNode resource.Type = "example.node"

func NodeIdentifier(name string) resource.Identifier {
	return resource.Identifier{ID: name, Type: ResourceTypeNode}
}

type Node struct {
	Name string ` + "`" + `json:"name"` + "`" + `
}

func (n *Node) Identifier() resource.Identifier {
	return NodeIdentifier(n.Name)
}
`),
		"link.go": []byte(`
package example

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypeLink resource.Type = "example.link"

func LinkIdentifier(from, to string) resource.Identifier {
	return resource.Identifier{ID: fmt.Sprintf("%s-%s", from, to), Type: ResourceTypeLink}
}

type Link struct {
	From string ` + "`" + `json:"from"` + "`" + `
	To   string ` + "`" + `json:"to"` + "`" + `
}

func (l *Link) Identifier() resource.Identifier {
	return LinkIdentifier(l.From, l.To)
}
`),
	}

	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}

	inliner := &Inliner{neededImports: make(map[string]string)}

	var types []namedType
	for _, name := range []string{"Node", "Link"} {
		st := idx.Structs[name]
		body, err := inliner.structBody(st, idx, make(map[string]bool))
		if err != nil {
			t.Fatalf("structBody(%s): %v", name, err)
		}
		info, err := findIdentifierInfo(name, idx)
		if err != nil {
			t.Fatalf("findIdentifierInfo(%s): %v", name, err)
		}
		types = append(types, namedType{Name: name, Body: body, IdentifierInfo: info})
	}

	out, err := buildOutput("schemas", inliner.neededImports, types)
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	src2 := string(out)

	// Both types must have their identifier functions in the output.
	if !strings.Contains(src2, "func NodeIdentifier") {
		t.Error("expected NodeIdentifier function in output")
	}
	if !strings.Contains(src2, "func LinkIdentifier") {
		t.Error("expected LinkIdentifier function in output (multi-param identifier)")
	}

	t.Logf("Generated output:\n%s", src2)
}

func TestBuildOutput_MultipleTypesAllGetIdentifierFunctions(t *testing.T) {
	idx, err := indexPackage(twoTypeSrc)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}

	inliner := &Inliner{neededImports: make(map[string]string)}

	var types []namedType
	for _, name := range []string{"Widget", "Gadget"} {
		st := idx.Structs[name]
		body, err := inliner.structBody(st, idx, make(map[string]bool))
		if err != nil {
			t.Fatalf("structBody(%s): %v", name, err)
		}
		info, err := findIdentifierInfo(name, idx)
		if err != nil {
			t.Fatalf("findIdentifierInfo(%s): %v", name, err)
		}
		types = append(types, namedType{Name: name, Body: body, IdentifierInfo: info})
	}

	out, err := buildOutput("schemas", inliner.neededImports, types)
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	src := string(out)

	if !strings.Contains(src, "func WidgetIdentifier") {
		t.Error("expected WidgetIdentifier function in output")
	}
	if !strings.Contains(src, "func GadgetIdentifier") {
		t.Error("expected GadgetIdentifier function in output")
	}

	t.Logf("Generated output:\n%s", src)
}

func TestFindIdentifierInfo(t *testing.T) {
	src := map[string][]byte{
		"example.go": []byte(`
package example

import "github.com/pgEdge/control-plane/server/internal/resource"

const ResourceTypeWidget resource.Type = "example.widget"

func WidgetIdentifier(id string) resource.Identifier {
	return resource.Identifier{
		ID:   id,
		Type: ResourceTypeWidget,
	}
}

type Widget struct {
	ID   string ` + "`" + `json:"id"` + "`" + `
	Name string ` + "`" + `json:"name"` + "`" + `
}

func (w *Widget) Identifier() resource.Identifier {
	return WidgetIdentifier(w.ID)
}
`),
	}

	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}

	info, err := findIdentifierInfo("Widget", idx)
	if err != nil {
		t.Fatalf("findIdentifierInfo: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil IdentifierInfo")
	}
	if info.ConstName != "ResourceTypeWidget" {
		t.Errorf("ConstName = %q, want %q", info.ConstName, "ResourceTypeWidget")
	}
	if info.ConstValue != "example.widget" {
		t.Errorf("ConstValue = %q, want %q", info.ConstValue, "example.widget")
	}
	if info.FuncName != "WidgetIdentifier" {
		t.Errorf("FuncName = %q, want %q", info.FuncName, "WidgetIdentifier")
	}
	if info.ParamName != "id" {
		t.Errorf("ParamName = %q, want %q", info.ParamName, "id")
	}
	if info.ResourcePkgAlias != "resource" {
		t.Errorf("ResourcePkgAlias = %q, want %q", info.ResourcePkgAlias, "resource")
	}
	if info.ResourcePkgPath != "github.com/pgEdge/control-plane/server/internal/resource" {
		t.Errorf("ResourcePkgPath = %q, want %q", info.ResourcePkgPath, "github.com/pgEdge/control-plane/server/internal/resource")
	}
}

func TestFindIdentifierInfo_NoIdentifierMethod(t *testing.T) {
	src := map[string][]byte{
		"example.go": []byte(`
package example

type Gadget struct {
	ID string ` + "`" + `json:"id"` + "`" + `
}
`),
	}

	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}

	info, err := findIdentifierInfo("Gadget", idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil IdentifierInfo for type without Identifier(), got %+v", info)
	}
}

func TestFindIdentifierInfo_MultiParam(t *testing.T) {
	// For functions with multiple parameters, ParamName should be empty.
	src := map[string][]byte{
		"example.go": []byte(`
package example

import "github.com/pgEdge/control-plane/server/internal/resource"

const ResourceTypeEdge resource.Type = "example.edge"

func EdgeIdentifier(nodeID string, direction string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeID + "-" + direction,
		Type: ResourceTypeEdge,
	}
}

type Edge struct {
	NodeID    string ` + "`" + `json:"node_id"` + "`" + `
	Direction string ` + "`" + `json:"direction"` + "`" + `
}

func (e *Edge) Identifier() resource.Identifier {
	return EdgeIdentifier(e.NodeID, e.Direction)
}
`),
	}

	idx, err := indexPackage(src)
	if err != nil {
		t.Fatalf("indexPackage: %v", err)
	}

	info, err := findIdentifierInfo("Edge", idx)
	if err != nil {
		t.Fatalf("findIdentifierInfo: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil IdentifierInfo")
	}
	if info.ConstName != "ResourceTypeEdge" {
		t.Errorf("ConstName = %q, want %q", info.ConstName, "ResourceTypeEdge")
	}
	if info.ConstValue != "example.edge" {
		t.Errorf("ConstValue = %q, want %q", info.ConstValue, "example.edge")
	}
	if info.FuncName != "EdgeIdentifier" {
		t.Errorf("FuncName = %q, want %q", info.FuncName, "EdgeIdentifier")
	}
	if info.ParamName != "" {
		t.Errorf("ParamName = %q, want empty for multi-param function", info.ParamName)
	}
}

func TestBuildOutput_WithIdentifierInfo(t *testing.T) {
	info := &IdentifierInfo{
		ConstName:        "ResourceTypeWidget",
		ConstValue:       "example.widget",
		FuncName:         "WidgetIdentifier",
		ParamName:        "id",
		ResourcePkgAlias: "resource",
		ResourcePkgPath:  "github.com/pgEdge/control-plane/server/internal/resource",
	}
	types := []namedType{
		{
			Name:           "Widget",
			Body:           "struct {\n\tID string `json:\"id\"`\n}",
			IdentifierInfo: info,
		},
	}
	out, err := buildOutput("schemas", nil, types)
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	src := string(out)

	if strings.Contains(src, "type Identifier struct") {
		t.Error("must not define a local Identifier struct")
	}
	if !strings.Contains(src, `"github.com/pgEdge/control-plane/server/internal/resource"`) {
		t.Error("expected resource import")
	}
	if !strings.Contains(src, "ResourceTypeWidget resource.Type") {
		t.Error("expected const ResourceTypeWidget resource.Type")
	}
	if !strings.Contains(src, "func WidgetIdentifier(id string) resource.Identifier") {
		t.Error("expected WidgetIdentifier returning resource.Identifier")
	}
	if !strings.Contains(src, "type Widget struct") {
		t.Error("expected Widget type")
	}

	t.Logf("Generated output:\n%s", src)
}

