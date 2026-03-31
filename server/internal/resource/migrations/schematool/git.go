package main

import (
	"io"
	"path"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// openRepoAtRef opens the git repo at repoPath and returns an *object.Tree at
// refName. refName may be a branch name, tag, or full commit SHA.
func openRepoAtRef(repoPath, refName string) (*object.Tree, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	hash, err := repo.ResolveRevision(plumbing.Revision(refName))
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, err
	}

	return commit.Tree()
}

// readFile reads a file at filePath from a git tree, returning its contents as
// []byte.
func readFile(tree *object.Tree, filePath string) ([]byte, error) {
	f, err := tree.File(filePath)
	if err != nil {
		return nil, err
	}

	r, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

// listGoFiles returns the paths of all non-test .go files directly inside dir
// within the given tree (non-recursive — only immediate children of dir).
func listGoFiles(tree *object.Tree, dir string) ([]string, error) {
	var files []string

	err := tree.Files().ForEach(func(f *object.File) error {
		if path.Dir(f.Name) == dir &&
			strings.HasSuffix(f.Name, ".go") &&
			!strings.HasSuffix(f.Name, "_test.go") {
			files = append(files, f.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}
