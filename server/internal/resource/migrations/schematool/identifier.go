package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
)

// IdentifierInfo holds the resource.Type constant and resource.Identifier
// function discovered by tracing a type's Identifier() method.
type IdentifierInfo struct {
	ConstName        string // e.g., "ResourceTypeDir"
	ConstValue       string // e.g., "filesystem.dir"
	FuncName         string // e.g., "DirResourceIdentifier"
	ResourcePkgAlias string // import alias for the resource package, e.g., "resource"
	ResourcePkgPath  string // import path, e.g., "github.com/pgEdge/control-plane/server/internal/resource"
	// ParamName is the single string parameter name when the identifier
	// function has exactly one string parameter; empty otherwise.
	ParamName string
	// FuncSource is the printed source of the original identifier function
	// declaration. When non-empty, buildOutput emits it verbatim instead of
	// generating a simplified single-param version.
	FuncSource string
	// FuncImports contains any additional imports (beyond the resource package)
	// required by the identifier function body, keyed by alias.
	FuncImports map[string]string
}

// findIdentifierInfo traces the Identifier() method of typeName in idx to
// locate the resource.Type constant and resource.Identifier function it uses.
// Returns nil, nil when the type has no Identifier() method or the pattern is
// not recognised.
func findIdentifierInfo(typeName string, idx *PackageIndex) (*IdentifierInfo, error) {
	methods, ok := idx.Methods[typeName]
	if !ok {
		return nil, nil
	}
	identMethod, ok := methods["Identifier"]
	if !ok {
		return nil, nil
	}

	// The Identifier() method body must be: return XxxIdentifier(...)
	funcName := extractCalledFuncName(identMethod)
	if funcName == "" {
		return nil, nil
	}

	identFunc, ok := idx.Funcs[funcName]
	if !ok {
		return nil, fmt.Errorf("identifier function %q not found in package", funcName)
	}

	// The identifier function body must be:
	//   return resource.Identifier{..., Type: SomeConst}
	constName := extractTypeConstName(identFunc)
	if constName == "" {
		return nil, fmt.Errorf("could not extract Type constant from %q body", funcName)
	}

	constValue, ok := idx.Consts[constName]
	if !ok {
		return nil, fmt.Errorf("constant %q not found in package", constName)
	}

	// Discover the import alias and path for the resource package by inspecting
	// the return type of the identifier function (e.g. resource.Identifier).
	pkgAlias, pkgPath := extractReturnPkgImport(identFunc, idx)

	return &IdentifierInfo{
		ConstName:        constName,
		ConstValue:       constValue,
		FuncName:         funcName,
		ResourcePkgAlias: pkgAlias,
		ResourcePkgPath:  pkgPath,
		ParamName:        extractSingleStringParam(identFunc),
		FuncSource:       printFuncDecl(identFunc, idx),
		FuncImports:      collectFuncImports(identFunc, idx),
	}, nil
}

// printFuncDecl renders a function declaration back to source text using the
// file set stored in idx.
func printFuncDecl(fn *ast.FuncDecl, idx *PackageIndex) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, idx.Fset, fn); err != nil {
		return ""
	}
	return buf.String()
}

// collectFuncImports returns the subset of idx.Imports that are referenced by
// selector expressions in fn's body (e.g. fmt.Sprintf → "fmt").
func collectFuncImports(fn *ast.FuncDecl, idx *PackageIndex) map[string]string {
	imports := make(map[string]string)
	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := idx.Imports[ident.Name]; ok {
			imports[ident.Name] = path
		}
		return true
	})
	return imports
}

// extractCalledFuncName returns the name of the bare function called in a
// single-statement return: return FuncName(...).
func extractCalledFuncName(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return ""
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// extractTypeConstName finds the identifier assigned to the "Type" key in
// the composite literal returned by an identifier function:
//
//	return pkg.Identifier{..., Type: SomeConst}
func extractTypeConstName(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return ""
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok {
		return ""
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Type" {
			continue
		}
		val, ok := kv.Value.(*ast.Ident)
		if !ok {
			return ""
		}
		return val.Name
	}
	return ""
}

// extractReturnPkgImport returns the import alias and path for the package
// named in a function's return type (e.g. "resource", "github.com/…/resource"
// when the return type is resource.Identifier).
func extractReturnPkgImport(fn *ast.FuncDecl, idx *PackageIndex) (alias, importPath string) {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return "", ""
	}
	sel, ok := fn.Type.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	a := pkgIdent.Name
	p, ok := idx.Imports[a]
	if !ok {
		return "", ""
	}
	return a, p
}

// extractSingleStringParam returns the parameter name when fn has exactly one
// parameter of type string, and "" otherwise.
func extractSingleStringParam(fn *ast.FuncDecl) string {
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return ""
	}
	field := fn.Type.Params.List[0]
	if len(field.Names) != 1 {
		return ""
	}
	ident, ok := field.Type.(*ast.Ident)
	if !ok || ident.Name != "string" {
		return ""
	}
	return field.Names[0].Name
}
