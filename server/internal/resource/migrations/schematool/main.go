package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	packageName := flag.String("package", "", "output package name (required)")
	repoPath := flag.String("repo", ".", "path to the git repository root")
	flag.Parse()

	args := flag.Args()
	if *packageName == "" || len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: schematool --package <name> [--repo <path>] <git-ref> <package-path> <TypeName> [TypeName ...]")
		os.Exit(1)
	}

	gitRef := args[0]
	pkgPath := args[1]
	typeNames := args[2:]

	if err := run(*packageName, *repoPath, gitRef, pkgPath, typeNames); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(packageName, repoPath, gitRef, pkgPath string, typeNames []string) error {
	tree, err := openRepoAtRef(repoPath, gitRef)
	if err != nil {
		return fmt.Errorf("open repo at %q ref %q: %w", repoPath, gitRef, err)
	}

	resolver, err := newResolver(tree)
	if err != nil {
		return fmt.Errorf("create resolver: %w", err)
	}

	idx, err := indexPackageFromTree(tree, pkgPath)
	if err != nil {
		return fmt.Errorf("index package %q: %w", pkgPath, err)
	}
	resolver.cache[pkgPath] = idx

	inliner := &Inliner{
		resolver:      resolver,
		neededImports: make(map[string]string),
	}

	var types []namedType
	for _, name := range typeNames {
		st, ok := idx.Structs[name]
		if !ok {
			return fmt.Errorf("type %q not found or not a struct in %q", name, pkgPath)
		}
		body, err := inliner.structBody(st, idx, make(map[string]bool))
		if err != nil {
			return fmt.Errorf("inline %q: %w", name, err)
		}
		info, err := findIdentifierInfo(name, idx)
		if err != nil {
			return fmt.Errorf("find identifier info for %q: %w", name, err)
		}
		types = append(types, namedType{Name: name, Body: body, IdentifierInfo: info})
	}

	out, err := buildOutput(packageName, inliner.neededImports, types)
	if err != nil {
		return fmt.Errorf("build output: %w", err)
	}

	fmt.Printf("// produced by schematool %s %s %s\n",
		tree.Hash,
		pkgPath,
		strings.Join(typeNames, " "),
	)
	fmt.Print(string(out))
	return nil
}
