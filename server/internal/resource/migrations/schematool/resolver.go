package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// Resolver resolves in-repo import paths to their PackageIndex, using the
// module path read from go.mod at the tree root.
type Resolver struct {
	modulePath string
	tree       *object.Tree
	cache      map[string]*PackageIndex // package dir → index
}

// newResolver reads go.mod from the tree root to determine the module path.
func newResolver(tree *object.Tree) (*Resolver, error) {
	content, err := readFile(tree, "go.mod")
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	modulePath, err := parseModulePath(content)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	return &Resolver{
		modulePath: modulePath,
		tree:       tree,
		cache:      make(map[string]*PackageIndex),
	}, nil
}

// parseModulePath extracts the module path from go.mod content.
// Scans line by line for a line starting with "module ".
func parseModulePath(content []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("module directive not found in go.mod")
}

// typeRemapping describes how a qualified type reference should be rewritten in
// schematool output.
type typeRemapping struct {
	newAlias      string
	newTypeName   string
	newImportPath string
}

// remapTypeRef returns a remapping for the given (importPath, typeName) pair,
// if one is configured. Returns (remapping, true) when a remapping applies.
//
// host.PgEdgeVersion is remapped to ds.PgEdgeVersion because the host package
// uses ds.PgEdgeVersion directly and should never produce an opaque
// host.PgEdgeVersion reference in generated schemas.
func (r *Resolver) remapTypeRef(importPath, typeName string) (typeRemapping, bool) {
	if importPath == r.modulePath+"/server/internal/host" && typeName == "PgEdgeVersion" {
		return typeRemapping{
			newAlias:      "ds",
			newTypeName:   "PgEdgeVersion",
			newImportPath: r.modulePath + "/server/internal/ds",
		}, true
	}
	return typeRemapping{}, false
}

// isExcludedFromInlining reports whether importPath should never be inlined,
// even if it belongs to this module. The ds package is excluded so that its
// types are always referenced by qualified name rather than expanded inline.
func (r *Resolver) isExcludedFromInlining(importPath string) bool {
	return importPath == r.modulePath+"/server/internal/ds" ||
		strings.HasPrefix(importPath, r.modulePath+"/server/internal/ds/")
}

// isInRepo reports whether importPath belongs to this module.
// True if importPath == modulePath OR starts with modulePath+"/".
func (r *Resolver) isInRepo(importPath string) bool {
	return importPath == r.modulePath || strings.HasPrefix(importPath, r.modulePath+"/")
}

// pkgDirFromImport converts an in-repo import path to a repo-relative directory.
// e.g. "github.com/pgEdge/control-plane/server/internal/resource" → "server/internal/resource"
func (r *Resolver) pkgDirFromImport(importPath string) string {
	return strings.TrimPrefix(importPath, r.modulePath+"/")
}

// packageIndexFor returns the PackageIndex for an in-repo import path, using the cache.
// Returns an error if importPath is not in-repo.
func (r *Resolver) packageIndexFor(importPath string) (*PackageIndex, error) {
	if !r.isInRepo(importPath) {
		return nil, fmt.Errorf("import path %q is not in module %q", importPath, r.modulePath)
	}

	dir := r.pkgDirFromImport(importPath)

	if idx, ok := r.cache[dir]; ok {
		return idx, nil
	}

	idx, err := indexPackageFromTree(r.tree, dir)
	if err != nil {
		return nil, fmt.Errorf("indexing package %q: %w", dir, err)
	}

	r.cache[dir] = idx
	return idx, nil
}
