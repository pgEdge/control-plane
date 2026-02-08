/*
This tool modifies the types in our API design files to add JSON struct tags.
We're doing this programmatically because there are many fields to modify and
naming them and keeping them in sync with the required fields is error-prone.

This main file takes a list of file paths to modify. It will then add
g.Meta("struct:tag:json", "<field name and optional omitempty>") calls to every
g.Attribute and g.ErrorName call that's part of a g.Type or g.ResultType. If the
field is not listed in the type's g.Required, the struct tag will have
omitempty.
*/
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
)

func main() {
	var errs []error
	for _, path := range os.Args[1:] {
		if err := addTags(path); err != nil {
			errs = append(errs, fmt.Errorf("error processing path '%s': %w", path, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		log.Fatal(err)
	}
}

func addTags(path string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	var errs []error
	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CallExpr:
			switch {
			case selectorMatches(t, "Type"):
				errs = append(errs, processType(fset, t))
			case selectorMatches(t, "ResultType"):
				errs = append(errs, processResultType(fset, t))
			}
		}
		return true
	})
	if err := errors.Join(errs...); err != nil {
		return err
	}

	// Write to a string first in case of errors
	var buf strings.Builder
	err = format.Node(&buf, fset, f)
	if err != nil {
		return fmt.Errorf("failed to format file: %w", err)
	}

	err = os.WriteFile(path, []byte(buf.String()), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write file '%s': %w", path, err)
	}

	return nil
}

func processResultType(fset *token.FileSet, n *ast.CallExpr) error {
	for _, arg := range n.Args {
		flit, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}
		required := extractRequired(flit)
		// Do second pass to process attributes
		for _, stmt := range flit.Body.List {
			attr := extractCall(stmt, "Attributes")
			if attr == nil {
				continue
			}
			if err := processAttributes(fset, attr, required); err != nil {
				return err
			}
		}
	}

	return nil
}

func processType(fset *token.FileSet, n *ast.CallExpr) error {
	for _, arg := range n.Args {
		flit, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}
		required := extractRequired(flit)
		for _, stmt := range flit.Body.List {
			attr := extractCall(stmt, "Attribute", "ErrorName")
			if attr != nil {
				if err := processAttribute(fset, attr, required); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func extractRequired(flit *ast.FuncLit) map[string]bool {
	required := map[string]bool{}
	// Starting from the end because our convention is to put required fields
	// last.
	for _, stmt := range slices.Backward(flit.Body.List) {
		req := extractCall(stmt, "Required")
		if req == nil {
			continue
		}
		for _, arg := range req.Args {
			if field := stringVal(arg); field != "" {
				required[field] = true
			}
		}
	}

	return required
}

func processAttributes(fset *token.FileSet, n *ast.CallExpr, required map[string]bool) error {
	for _, arg := range n.Args {
		flit, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}
		for _, stmt := range flit.Body.List {
			attr := extractCall(stmt, "Attribute", "ErrorName")
			if attr != nil {
				if err := processAttribute(fset, attr, required); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func processAttribute(fset *token.FileSet, n *ast.CallExpr, required map[string]bool) error {
	// first arg is the attribute name
	if len(n.Args) == 0 {
		return fmt.Errorf("%s: Attribute missing name", fset.Position(n.Pos()))
	}
	name := stringVal(n.Args[0])
	if name == "" {
		return fmt.Errorf("%s: Attribute first argument not string", fset.Position(n.Pos()))
	}

	var hasFuncLit bool

	tagLit := stringLit(tagValue(name, required[name]))

	for _, arg := range n.Args[1:] {
		flit, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}
		hasFuncLit = true
		var hasJsonMeta bool
		for _, stmt := range flit.Body.List {
			m := extractCall(stmt, "Meta")
			if m == nil {
				continue
			}
			if len(m.Args) == 0 {
				continue
			}
			if metaName := stringVal(m.Args[0]); metaName == "struct:tag:json" {
				hasJsonMeta = true
				if len(m.Args) < 2 {
					m.Args = append(m.Args, tagLit)
				} else {
					m.Args[1] = tagLit
				}
			}
		}
		if !hasJsonMeta {
			flit.Body.List = append(flit.Body.List, metaNode(tagLit))
		}
	}

	if !hasFuncLit {
		return fmt.Errorf("%s: Attribute missing function body", fset.Position(n.Pos()))
	}

	return nil
}

func tagValue(name string, required bool) string {
	tag := name
	if !required {
		tag += ",omitempty"
	}

	return tag
}

func metaNode(tagLit *ast.BasicLit) *ast.ExprStmt {
	return &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "g"},
				Sel: &ast.Ident{Name: "Meta"},
			},
			Args: []ast.Expr{stringLit("struct:tag:json"), tagLit},
		},
	}
}

func selectorMatches(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "g" && sel.Sel.Name == name
}

func extractCall(stmt ast.Stmt, name ...string) *ast.CallExpr {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	for _, n := range name {
		if selectorMatches(call, n) {
			return call
		}
	}

	return nil
}

func stringVal(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return ""
	}
	if lit.Kind != token.STRING {
		return ""
	}
	raw, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return raw
}

func stringLit(value string) *ast.BasicLit {
	return &ast.BasicLit{
		Kind:  token.STRING,
		Value: strconv.Quote(value),
	}
}
