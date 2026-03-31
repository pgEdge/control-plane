package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// PackageIndex holds the parsed type and import information for a Go package.
type PackageIndex struct {
	Name    string
	Fset    *token.FileSet                        // file set used when parsing; needed for printing AST nodes
	Structs map[string]*ast.StructType            // only struct types
	Types   map[string]ast.Expr                   // all named types (including structs)
	Imports map[string]string                     // alias → full import path
	Methods map[string]map[string]*ast.FuncDecl   // receiver type name → method name → decl
	Funcs   map[string]*ast.FuncDecl              // function name → decl
	Consts  map[string]string                     // const name → unquoted string literal value
}

// indexPackage parses Go source files (map of filename → content) and builds a
// PackageIndex.
func indexPackage(sources map[string][]byte) (*PackageIndex, error) {
	fset := token.NewFileSet()

	idx := &PackageIndex{
		Fset:    fset,
		Structs: make(map[string]*ast.StructType),
		Types:   make(map[string]ast.Expr),
		Imports: make(map[string]string),
		Methods: make(map[string]map[string]*ast.FuncDecl),
		Funcs:   make(map[string]*ast.FuncDecl),
		Consts:  make(map[string]string),
	}

	for filename, src := range sources {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filename, src, 0)
		if err != nil {
			return nil, err
		}

		if idx.Name == "" {
			idx.Name = f.Name.Name
		}

		// Collect imports.
		for _, imp := range f.Imports {
			// Strip surrounding quotes from the import path.
			importPath := strings.Trim(imp.Path.Value, `"`)

			var alias string
			if imp.Name != nil {
				// Skip blank and dot imports.
				if imp.Name.Name == "_" || imp.Name.Name == "." {
					continue
				}
				alias = imp.Name.Name
			} else {
				// Default alias is the last path segment.
				alias = path.Base(importPath)
			}

			idx.Imports[alias] = importPath
		}

		// Collect declarations.
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						name := s.Name.Name
						idx.Types[name] = s.Type
						if st, ok := s.Type.(*ast.StructType); ok {
							idx.Structs[name] = st
						}
					case *ast.ValueSpec:
						if d.Tok == token.CONST {
							for i, nameIdent := range s.Names {
								if i < len(s.Values) {
									if lit, ok := s.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
										idx.Consts[nameIdent.Name] = strings.Trim(lit.Value, `"`)
									}
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil && len(d.Recv.List) > 0 {
					recvType := receiverTypeName(d)
					if recvType != "" {
						if idx.Methods[recvType] == nil {
							idx.Methods[recvType] = make(map[string]*ast.FuncDecl)
						}
						idx.Methods[recvType][d.Name.Name] = d
					}
				} else {
					idx.Funcs[d.Name.Name] = d
				}
			}
		}
	}

	return idx, nil
}

// indexPackageFromTree reads all non-test .go files in pkgDir from the tree
// and delegates to indexPackage.
func indexPackageFromTree(tree *object.Tree, pkgDir string) (*PackageIndex, error) {
	files, err := listGoFiles(tree, pkgDir)
	if err != nil {
		return nil, err
	}

	sources := make(map[string][]byte, len(files))
	for _, filePath := range files {
		content, err := readFile(tree, filePath)
		if err != nil {
			return nil, err
		}
		sources[filePath] = content
	}

	return indexPackage(sources)
}

// receiverTypeName returns the base type name of a method's receiver,
// stripping any pointer indirection (e.g. *DirResource → "DirResource").
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
