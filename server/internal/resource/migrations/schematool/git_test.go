package main

import "testing"

func TestOpenRepoAtRef(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestListGoFiles(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	files, err := listGoFiles(tree, "server/internal/resource")
	if err != nil {
		t.Fatalf("listGoFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one .go file")
	}
	for _, f := range files {
		if len(f) < 3 || f[len(f)-3:] != ".go" {
			t.Errorf("unexpected file: %s", f)
		}
	}
}

func TestReadFile(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	content, err := readFile(tree, "server/internal/resource/resource.go")
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty file")
	}
}
