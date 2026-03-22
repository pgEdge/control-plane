package main

import "testing"

func TestResolverModulePath(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	r, err := newResolver(tree)
	if err != nil {
		t.Fatalf("newResolver: %v", err)
	}
	if r.modulePath != "github.com/pgEdge/control-plane" {
		t.Errorf("modulePath = %q, want %q", r.modulePath, "github.com/pgEdge/control-plane")
	}
}

func TestResolverIsInRepo(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	r, err := newResolver(tree)
	if err != nil {
		t.Fatalf("newResolver: %v", err)
	}
	if !r.isInRepo("github.com/pgEdge/control-plane/server/internal/resource") {
		t.Error("expected in-repo import to return true")
	}
	if r.isInRepo("encoding/json") {
		t.Error("expected stdlib import to return false")
	}
	if r.isInRepo("github.com/some/other") {
		t.Error("expected external import to return false")
	}
}

func TestResolverPackageIndex(t *testing.T) {
	tree, err := openRepoAtRef("../../../../", "HEAD")
	if err != nil {
		t.Fatalf("openRepoAtRef: %v", err)
	}
	r, err := newResolver(tree)
	if err != nil {
		t.Fatalf("newResolver: %v", err)
	}
	idx, err := r.packageIndexFor("github.com/pgEdge/control-plane/server/internal/resource")
	if err != nil {
		t.Fatalf("packageIndexFor: %v", err)
	}
	if _, ok := idx.Structs["Identifier"]; !ok {
		t.Error("expected Identifier struct in resource package")
	}
}

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "simple module",
			content: "module github.com/foo/bar\n\ngo 1.22\n",
			want:    "github.com/foo/bar",
		},
		{
			name:    "leading whitespace",
			content: "  module   github.com/foo/bar  \n",
			want:    "github.com/foo/bar",
		},
		{
			name:    "module with version suffix",
			content: "module github.com/foo/bar/v2\n",
			want:    "github.com/foo/bar/v2",
		},
		{
			name:    "missing module directive",
			content: "go 1.22\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseModulePath([]byte(tt.content))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseModulePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseModulePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
