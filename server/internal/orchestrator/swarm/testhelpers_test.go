package swarm

import (
	"encoding/json"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
)

func parseEmbeddedManifest(t *testing.T) *versionManifest {
	t.Helper()
	var mf versionManifest
	if err := json.Unmarshal(embeddedManifest, &mf); err != nil {
		t.Fatalf("unmarshal embedded manifest: %v", err)
	}
	return &mf
}

// newTestVersions builds a *Versions from the embedded manifest.
// Use this in tests instead of the deleted NewVersions constructor.
func newTestVersions(t *testing.T, cfg config.Config) *Versions {
	t.Helper()
	v, err := buildVersions(cfg, parseEmbeddedManifest(t))
	if err != nil {
		t.Fatalf("buildVersions: %v", err)
	}
	return v
}

// newTestServiceVersions builds a *ServiceVersions from the embedded manifest.
// Use this in tests instead of the deleted NewServiceVersions constructor.
func newTestServiceVersions(t *testing.T, cfg config.Config) *ServiceVersions {
	t.Helper()
	sv, err := buildServiceVersions(cfg, parseEmbeddedManifest(t))
	if err != nil {
		t.Fatalf("buildServiceVersions: %v", err)
	}
	return sv
}
