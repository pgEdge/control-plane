package swarm

import (
	"encoding/json"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
)

// newTestVersions builds a *Versions from the embedded manifest.
// Use this in tests instead of the deleted NewVersions constructor.
func newTestVersions(t *testing.T, cfg config.Config) *Versions {
	t.Helper()
	var mf versionManifest
	if err := json.Unmarshal(embeddedManifest, &mf); err != nil {
		t.Fatalf("unmarshal embedded manifest: %v", err)
	}
	v, err := buildVersions(cfg, &mf)
	if err != nil {
		t.Fatalf("buildVersions: %v", err)
	}
	return v
}

// newTestServiceVersions builds a *ServiceVersions from the embedded manifest.
// Use this in tests instead of the deleted NewServiceVersions constructor.
func newTestServiceVersions(t *testing.T, cfg config.Config) *ServiceVersions {
	t.Helper()
	var mf versionManifest
	if err := json.Unmarshal(embeddedManifest, &mf); err != nil {
		t.Fatalf("unmarshal embedded manifest: %v", err)
	}
	sv, err := buildServiceVersions(cfg, &mf)
	if err != nil {
		t.Fatalf("buildServiceVersions: %v", err)
	}
	return sv
}
