package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strings"
)

// namedType pairs a type name with its struct body string produced by
// Inliner.structBody, and optional identifier metadata discovered from the
// type's Identifier() method.
type namedType struct {
	Name           string
	Body           string          // the struct body string produced by Inliner.structBody
	IdentifierInfo *IdentifierInfo // nil when the type has no Identifier() method
}

// buildOutput assembles and go/format-formats a complete Go source file.
func buildOutput(packageName string, imports map[string]string, types []namedType) ([]byte, error) {
	var sb strings.Builder

	sb.WriteString("package ")
	sb.WriteString(packageName)
	sb.WriteString("\n\n")

	// Merge struct-field imports with any resource package import needed for
	// identifier consts and functions.
	mergedImports := make(map[string]string, len(imports))
	for k, v := range imports {
		mergedImports[k] = v
	}
	for _, nt := range types {
		if info := nt.IdentifierInfo; info != nil && info.ResourcePkgPath != "" {
			mergedImports[info.ResourcePkgAlias] = info.ResourcePkgPath
			for alias, path := range info.FuncImports {
				mergedImports[alias] = path
			}
		}
	}

	if len(mergedImports) > 0 {
		// Sort aliases alphabetically for deterministic output.
		aliases := make([]string, 0, len(mergedImports))
		for alias := range mergedImports {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)

		sb.WriteString("import (\n")
		for _, alias := range aliases {
			importPath := mergedImports[alias]
			lastSegment := path.Base(importPath)
			if alias == lastSegment {
				fmt.Fprintf(&sb, "\t%q\n", importPath)
			} else {
				fmt.Fprintf(&sb, "\t%s %q\n", alias, importPath)
			}
		}
		sb.WriteString(")\n\n")
	}

	for _, nt := range types {
		if info := nt.IdentifierInfo; info != nil {
			pkg := info.ResourcePkgAlias
			fmt.Fprintf(&sb, "const %s %s.Type = %q\n\n", info.ConstName, pkg, info.ConstValue)
			if info.FuncSource != "" {
				fmt.Fprintf(&sb, "%s\n\n", info.FuncSource)
			} else {
				// Fallback for manually-constructed IdentifierInfo (e.g. in tests).
				paramName := info.ParamName
				if paramName == "" {
					paramName = "id"
				}
				fmt.Fprintf(&sb,
					"func %s(%s string) %s.Identifier {\n\treturn %s.Identifier{ID: %s, Type: %s}\n}\n\n",
					info.FuncName, paramName, pkg, pkg, paramName, info.ConstName)
			}
		}
		fmt.Fprintf(&sb, "type %s %s\n\n", nt.Name, nt.Body)
	}

	return format.Source([]byte(sb.String()))
}

// exprString renders an ast.Expr back to source using go/printer.
func exprString(expr ast.Expr) string {
	var buf bytes.Buffer
	fset := token.NewFileSet()
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return fmt.Sprintf("/* unprintable: %T */", expr)
	}
	return buf.String()
}
