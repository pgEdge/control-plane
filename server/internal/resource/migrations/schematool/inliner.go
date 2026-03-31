package main

import (
	"fmt"
	"go/ast"
	"reflect"
	"strings"
)

// Inliner converts Go type expressions to their inlined string representations,
// recursively expanding in-repo struct types into anonymous struct literals.
type Inliner struct {
	resolver      *Resolver         // may be nil in unit tests
	neededImports map[string]string // alias → import path (collected during inlining)
}

// typeExpr converts an ast.Expr to its inlined string representation.
// currentIdx is the PackageIndex for the package where expr appears.
// visited tracks "importPath.TypeName" keys to break cycles.
func (in *Inliner) typeExpr(expr ast.Expr, currentIdx *PackageIndex, visited map[string]bool) (string, error) {
	switch t := expr.(type) {
	case *ast.Ident:
		name := t.Name
		if isBuiltin(name) {
			return name, nil
		}
		if underlying, ok := currentIdx.Types[name]; ok {
			key := currentIdx.Name + "." + name
			if visited[key] {
				return "any /* cycle */", nil
			}
			v := copyVisited(visited)
			v[key] = true
			if st, ok := underlying.(*ast.StructType); ok {
				return in.structBody(st, currentIdx, v)
			}
			return in.typeExpr(underlying, currentIdx, v)
		}
		// Not found — generic param or unresolved name.
		return name, nil

	case *ast.SelectorExpr:
		pkgIdent, ok := t.X.(*ast.Ident)
		if !ok {
			return exprString(expr), nil
		}
		alias := pkgIdent.Name
		typeName := t.Sel.Name

		importPath, found := currentIdx.Imports[alias]
		if !found {
			return alias + "." + typeName, nil
		}

		if in.resolver != nil {
			if remap, ok := in.resolver.remapTypeRef(importPath, typeName); ok {
				in.neededImports[remap.newAlias] = remap.newImportPath
				return remap.newAlias + "." + remap.newTypeName, nil
			}
		}

		if in.resolver != nil && in.resolver.isInRepo(importPath) && !in.resolver.isExcludedFromInlining(importPath) {
			pkgIdx, err := in.resolver.packageIndexFor(importPath)
			if err != nil {
				return alias + "." + typeName, nil
			}
			key := importPath + "." + typeName
			if visited[key] {
				return "any /* cycle */", nil
			}
			v := copyVisited(visited)
			v[key] = true
			if underlying, ok := pkgIdx.Types[typeName]; ok {
				if st, ok := underlying.(*ast.StructType); ok {
					return in.structBody(st, pkgIdx, v)
				}
				return in.typeExpr(underlying, pkgIdx, v)
			}
			return alias + "." + typeName, nil
		}

		// External package — record the import and return as-is.
		in.neededImports[alias] = importPath
		return alias + "." + typeName, nil

	case *ast.StarExpr:
		inner, err := in.typeExpr(t.X, currentIdx, visited)
		if err != nil {
			return "", err
		}
		return "*" + inner, nil

	case *ast.ArrayType:
		if t.Len == nil {
			inner, err := in.typeExpr(t.Elt, currentIdx, visited)
			if err != nil {
				return "", err
			}
			return "[]" + inner, nil
		}
		// Fixed-size array — fall back to printer.
		return exprString(expr), nil

	case *ast.MapType:
		key, err := in.typeExpr(t.Key, currentIdx, visited)
		if err != nil {
			return "", err
		}
		val, err := in.typeExpr(t.Value, currentIdx, visited)
		if err != nil {
			return "", err
		}
		return "map[" + key + "]" + val, nil

	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "any", nil
		}
		return "interface{}", nil

	default:
		return exprString(expr), nil
	}
}

// structBody generates the body of an anonymous struct (including braces),
// with all in-repo struct types recursively inlined.
func (in *Inliner) structBody(st *ast.StructType, currentIdx *PackageIndex, visited map[string]bool) (string, error) {
	var sb strings.Builder
	sb.WriteString("struct {\n")

	for _, field := range st.Fields.List {
		typeStr, err := in.typeExpr(field.Type, currentIdx, visited)
		if err != nil {
			return "", err
		}

		// Extract json tag.
		jsonTag := ""
		if field.Tag != nil {
			tagStr := strings.Trim(field.Tag.Value, "`")
			val := reflect.StructTag(tagStr).Get("json")
			if val != "" {
				jsonTag = "`json:\"" + val + "\"`"
			}
		}

		if len(field.Names) == 0 {
			// Embedded field.
			if jsonTag != "" {
				fmt.Fprintf(&sb, "\t%s %s\n", typeStr, jsonTag)
			} else {
				fmt.Fprintf(&sb, "\t%s\n", typeStr)
			}
		} else {
			for _, nameIdent := range field.Names {
				if jsonTag != "" {
					fmt.Fprintf(&sb, "\t%s %s %s\n", nameIdent.Name, typeStr, jsonTag)
				} else {
					fmt.Fprintf(&sb, "\t%s %s\n", nameIdent.Name, typeStr)
				}
			}
		}
	}

	sb.WriteString("}")
	return sb.String(), nil
}

// isBuiltin reports whether name is a predeclared Go identifier.
func isBuiltin(name string) bool {
	switch name {
	case "bool", "byte", "complex64", "complex128", "error",
		"float32", "float64", "int", "int8", "int16", "int32", "int64",
		"rune", "string", "uint", "uint8", "uint16", "uint32", "uint64",
		"uintptr", "any", "comparable":
		return true
	}
	return false
}

// copyVisited returns a shallow copy of a visited map to avoid mutating the caller's map.
func copyVisited(v map[string]bool) map[string]bool {
	c := make(map[string]bool, len(v))
	for k := range v {
		c[k] = true
	}
	return c
}
