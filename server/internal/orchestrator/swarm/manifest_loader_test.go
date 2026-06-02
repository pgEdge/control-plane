package swarm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

// testCfg returns a config with an isolated cache directory in t.TempDir().
func testCfg(t *testing.T, extra ...func(*config.DockerSwarm)) (config.Config, string) {
	t.Helper()
	cacheDir := t.TempDir()
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	for _, fn := range extra {
		fn(&cfg.DockerSwarm)
	}
	return cfg, filepath.Join(cacheDir, "manifest-cache.json")
}

// validManifest returns a well-formed manifest JSON matching the embedded one.
func validManifest(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"images": map[string]any{
			"postgres": []map[string]any{
				{
					"postgres_version": "17.10",
					"spock_version":    "5",
					"image":            "pgedge-postgres:17.10-spock5.0.8-standard-1",
					"stability":        "stable",
					"default":          true,
				},
			},
			"mcp": []map[string]any{
				{"version": "latest", "image": "postgres-mcp:latest", "stability": "stable", "default": true},
			},
			"postgrest": []map[string]any{
				{"version": "14.5", "image": "postgrest:14.5", "stability": "stable", "default": true},
			},
			"rag": []map[string]any{
				{"version": "latest", "image": "rag-server:latest", "stability": "stable", "default": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal test manifest: %v", err)
	}
	return data
}

// TestManifestLoader_LoadFromEmbedded verifies the loader falls back to the
// embedded manifest when no URL, cache, or manifest_path is available.
func TestManifestLoader_LoadFromEmbedded(t *testing.T) {
	cfg, cachePath := testCfg(t)
	// Point at a server that always 500s to force URL failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
		withEmbeddedFallback(),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	v := loader.Versions()
	if v.Default() == nil {
		t.Fatal("expected non-nil default version from embedded manifest")
	}
	if len(v.Supported()) == 0 {
		t.Fatal("expected at least one supported version from embedded manifest")
	}

	sv := loader.ServiceVersions()
	if _, err := sv.SupportedServiceVersions("mcp"); err != nil {
		t.Fatalf("expected ServiceVersions to be populated from embedded manifest: %v", err)
	}
}

// TestManifestLoader_LoadFromURL verifies the happy-path URL fetch.
func TestManifestLoader_LoadFromURL(t *testing.T) {
	cfg, cachePath := testCfg(t)
	manifest := validManifest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(manifest)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	v := loader.Versions()
	def := v.Default()
	if def == nil {
		t.Fatal("expected non-nil default version")
	}
	if def.PostgresVersion.String() != "17.10" {
		t.Errorf("default postgres version = %s, want 17.10", def.PostgresVersion)
	}

	// Cache should have been written.
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file at %s: %v", cachePath, err)
	}
}

// TestManifestLoader_LoadFromCache verifies that a stale URL causes the loader
// to fall back to the disk cache.
func TestManifestLoader_LoadFromCache(t *testing.T) {
	cfg, cachePath := testCfg(t)
	manifest := validManifest(t)

	// Pre-populate the cache.
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	// URL always fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	v := loader.Versions()
	if v.Default() == nil {
		t.Fatal("expected non-nil default version from cache")
	}
}

// TestManifestLoader_CustomURLNoFallbackToEmbedded verifies that a custom
// manifest_url that fails does NOT fall back to the embedded manifest and
// instead returns an error.
func TestManifestLoader_CustomURLNoFallbackToEmbedded(t *testing.T) {
	cfg, cachePath := testCfg(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	// Use a non-default URL to trigger the custom-URL chain.
	cfg.DockerSwarm.ManifestURL = srv.URL + "/custom"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
	)
	if err == nil {
		t.Fatal("expected error for custom URL with no cache and failing server")
	}
}

// TestManifestLoader_LoadFromManifestPath verifies the local file override.
func TestManifestLoader_LoadFromManifestPath(t *testing.T) {
	manifest := validManifest(t)
	mfFile := filepath.Join(t.TempDir(), "local-manifest.json")
	if err := os.WriteFile(mfFile, manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	_, cachePath := testCfg(t)
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
			ManifestPath:        mfFile,
		},
	}

	loader, err := NewManifestLoader(context.Background(), cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	v := loader.Versions()
	if v.Default().PostgresVersion.String() != "17.10" {
		t.Errorf("default version = %s, want 17.10", v.Default().PostgresVersion)
	}
}

// TestManifestLoader_ManifestPathMissing verifies that a missing manifest_path
// returns an error (no fallback to embedded).
func TestManifestLoader_ManifestPathMissing(t *testing.T) {
	_, cachePath := testCfg(t)
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
			ManifestPath:        "/does/not/exist/manifest.json",
		},
	}

	_, err := NewManifestLoader(context.Background(), cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
	)
	if err == nil {
		t.Fatal("expected error when manifest_path points to a non-existent file")
	}
}

// TestManifestLoader_InvalidSchemaVersion verifies that a manifest with an
// unsupported schema_version causes URL/cache to be skipped and falls back to
// the embedded manifest (default URL chain).
func TestManifestLoader_InvalidSchemaVersion(t *testing.T) {
	cfg, cachePath := testCfg(t)
	badManifest := []byte(`{"schema_version":99,"images":{}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(badManifest)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
		withEmbeddedFallback(),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	// Falls back to embedded — should still work.
	if loader.Versions().Default() == nil {
		t.Fatal("expected fallback to embedded on invalid schema_version")
	}
}

// TestManifestLoader_MalformedJSON verifies that malformed JSON causes fallback.
func TestManifestLoader_MalformedJSON(t *testing.T) {
	cfg, cachePath := testCfg(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
		withEmbeddedFallback(),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	if loader.Versions().Default() == nil {
		t.Fatal("expected fallback to embedded on malformed JSON")
	}
}

// TestManifestLoader_NoRefreshWhenManifestPathSet verifies that when
// manifest_path is set the background refresh goroutine is NOT started.
//
// Strategy: inject a pre-fired ticker via withTickerC. If the goroutine were
// started, it would immediately consume the tick, fetch the URL (which returns
// PG 16.14), and update the default version. We then assert the version is
// still 17.10 (from the local file), proving no goroutine ran.
func TestManifestLoader_NoRefreshWhenManifestPathSet(t *testing.T) {
	manifest := validManifest(t)
	mfFile := filepath.Join(t.TempDir(), "local-manifest.json")
	if err := os.WriteFile(mfFile, manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	// Serve a different default version at the URL so any refresh would be detectable.
	updated, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"images": map[string]any{
			"postgres": []map[string]any{
				{
					"postgres_version": "16.14",
					"spock_version":    "5",
					"image":            "pgedge-postgres:16.14-spock5.0.8-standard-1",
					"stability":        "stable",
					"default":          true,
				},
			},
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(updated)
	}))
	defer srv.Close()

	_, cachePath := testCfg(t)
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
			ManifestPath:        mfFile,
			ManifestURL:         srv.URL,
		},
	}

	// Pre-fired ticker: if the refresh goroutine were started it would consume
	// this tick immediately and refresh from the URL.
	immediateTick := make(chan time.Time, 1)
	immediateTick <- time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
		withTickerC(immediateTick),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	// Allow enough time for the goroutine (if it existed) to process the tick
	// and complete an HTTP round-trip to localhost.
	time.Sleep(50 * time.Millisecond)

	if got := loader.Versions().Default().PostgresVersion.String(); got != "17.10" {
		t.Errorf("default version = %q; refresh goroutine must not run when manifest_path is set", got)
	}
}

// TestManifestLoader_RefreshSuccess verifies that refresh() updates in-memory
// versions when a new valid manifest is served.
func TestManifestLoader_RefreshSuccess(t *testing.T) {
	cfg, cachePath := testCfg(t)

	// First serve a manifest with PG 17.10.
	current := validManifest(t)

	// Prepare an updated manifest with PG 16.14 as default.
	updated, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"images": map[string]any{
			"postgres": []map[string]any{
				{
					"postgres_version": "16.14",
					"spock_version":    "5",
					"image":            "pgedge-postgres:16.14-spock5.0.8-standard-1",
					"stability":        "stable",
					"default":          true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	serve := current
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(serve)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	if loader.Versions().Default().PostgresVersion.String() != "17.10" {
		t.Fatalf("expected initial default 17.10, got %s", loader.Versions().Default().PostgresVersion)
	}

	// Swap what the server returns, then trigger a refresh.
	serve = updated
	loader.refresh(context.Background())

	if loader.Versions().Default().PostgresVersion.String() != "16.14" {
		t.Errorf("expected refreshed default 16.14, got %s", loader.Versions().Default().PostgresVersion)
	}
}

// TestManifestLoader_RefreshFailure verifies that a failing refresh leaves
// in-memory versions unchanged.
func TestManifestLoader_RefreshFailure(t *testing.T) {
	cfg, cachePath := testCfg(t)
	manifest := validManifest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(manifest)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	origDefault := loader.Versions().Default().PostgresVersion.String()

	// Now make the server return garbage.
	manifest = []byte(`{invalid}`)
	loader.refresh(context.Background())

	if loader.Versions().Default().PostgresVersion.String() != origDefault {
		t.Error("versions changed after a failed refresh")
	}
}

// TestBuildVersions_MatchesNewVersions verifies that buildVersions produces
// the same set of supported versions as the hardcoded NewVersions function
// when given the embedded manifest.
func TestBuildVersions_MatchesNewVersions(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}

	var mf versionManifest
	if err := json.Unmarshal(embeddedManifest, &mf); err != nil {
		t.Fatalf("unmarshal embedded manifest: %v", err)
	}

	got, err := buildVersions(cfg, &mf)
	if err != nil {
		t.Fatalf("buildVersions: %v", err)
	}

	want := NewVersions(cfg)

	if len(got.Supported()) != len(want.Supported()) {
		t.Errorf("Supported() len = %d, want %d", len(got.Supported()), len(want.Supported()))
	}

	for _, wv := range want.Supported() {
		imgs, err := got.GetImages(wv)
		if err != nil {
			t.Errorf("GetImages(%s) not found in manifest-built Versions: %v", wv, err)
			continue
		}
		wantImgs, _ := want.GetImages(wv)
		if imgs.PgEdgeImage != wantImgs.PgEdgeImage {
			t.Errorf("GetImages(%s).PgEdgeImage = %q, want %q", wv, imgs.PgEdgeImage, wantImgs.PgEdgeImage)
		}
	}

	if got.Default().PostgresVersion.String() != want.Default().PostgresVersion.String() {
		t.Errorf("Default() = %s, want %s", got.Default().PostgresVersion, want.Default().PostgresVersion)
	}
}

// TestBuildServiceVersions_MatchesNewServiceVersions verifies that
// buildServiceVersions produces the same registrations as NewServiceVersions
// for the embedded manifest.
func TestBuildServiceVersions_MatchesNewServiceVersions(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}

	var mf versionManifest
	if err := json.Unmarshal(embeddedManifest, &mf); err != nil {
		t.Fatalf("unmarshal embedded manifest: %v", err)
	}

	got, err := buildServiceVersions(cfg, &mf)
	if err != nil {
		t.Fatalf("buildServiceVersions: %v", err)
	}

	want := NewServiceVersions(cfg)

	serviceTypes := []string{"mcp", "postgrest", "rag"}
	for _, svc := range serviceTypes {
		gotVers, err := got.SupportedServiceVersions(svc)
		if err != nil {
			t.Errorf("SupportedServiceVersions(%q) error: %v", svc, err)
			continue
		}
		wantVers, _ := want.SupportedServiceVersions(svc)
		if len(gotVers) != len(wantVers) {
			t.Errorf("SupportedServiceVersions(%q) len = %d, want %d", svc, len(gotVers), len(wantVers))
		}

		for _, ver := range wantVers {
			gotImg, err := got.GetServiceImage(svc, ver)
			if err != nil {
				t.Errorf("GetServiceImage(%q, %q) not found: %v", svc, ver, err)
				continue
			}
			wantImg, _ := want.GetServiceImage(svc, ver)
			if gotImg.Tag != wantImg.Tag {
				t.Errorf("GetServiceImage(%q, %q).Tag = %q, want %q", svc, ver, gotImg.Tag, wantImg.Tag)
			}
		}
	}
}

// TestManifestLoader_RealURL exercises the loader against a real HTTP server.
// Run with a local file server already serving version-manifest.json:
//
//	cd server/internal/orchestrator/swarm && python3 -m http.server 9090
//	MANIFEST_TEST_URL=http://localhost:9090/version-manifest.json \
//	  go test -v -run TestManifestLoader_RealURL ./server/internal/orchestrator/swarm/...
func TestManifestLoader_RealURL(t *testing.T) {
	url := os.Getenv("MANIFEST_TEST_URL")
	if url == "" {
		t.Skip("MANIFEST_TEST_URL not set; skipping real-URL integration test")
	}

	_, cachePath := testCfg(t)
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
			ManifestURL:         url,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	v := loader.Versions()
	if v.Default() == nil {
		t.Fatal("expected non-nil default version from real URL")
	}
	t.Logf("loaded %d supported versions; default=%s", len(v.Supported()), v.Default().PostgresVersion)

	// Cache file must have been written.
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file written at %s: %v", cachePath, err)
	}

	// Force a refresh and confirm versions are still intact.
	loader.refresh(context.Background())
	if loader.Versions().Default().PostgresVersion.String() != v.Default().PostgresVersion.String() {
		t.Error("default version changed unexpectedly after refresh")
	}
	t.Logf("refresh OK; default still %s", loader.Versions().Default().PostgresVersion)
}

// TestEmbeddedManifestValid fully parses the version-manifest.json embedded in
// the binary.  A failure here means NewManifestLoader would panic at startup —
// catching it in CI is much better than catching it in production.
func TestEmbeddedManifestValid(t *testing.T) {
	m := &ManifestLoader{logger: testutils.Logger(t)}
	v, sv, err := m.parseManifestData(embeddedManifest)
	if err != nil {
		t.Fatalf("embedded manifest cannot be parsed: %v", err)
	}
	if v.Default() == nil {
		t.Fatal("embedded manifest has no default version")
	}
	if len(v.Supported()) == 0 {
		t.Fatal("embedded manifest has no supported versions")
	}
	if _, err := sv.SupportedServiceVersions("mcp"); err != nil {
		t.Errorf("embedded manifest missing mcp service versions: %v", err)
	}
}

// TestValidateManifest covers schema_version and JSON validation.
func TestValidateManifest(t *testing.T) {
	m := &ManifestLoader{logger: testutils.Logger(t)}

	if err := m.validateManifest(embeddedManifest); err != nil {
		t.Errorf("embedded manifest should be valid: %v", err)
	}

	if err := m.validateManifest([]byte(`{"schema_version":2,"images":{}}`)); err == nil {
		t.Error("expected error for schema_version 2")
	}

	if err := m.validateManifest([]byte(`not json`)); err == nil {
		t.Error("expected error for non-JSON input")
	}
}

// TestManifestLoader_ImageTagsHaveRegistryPrefix verifies that all image tags
// returned by Versions and ServiceVersions include the configured registry
// host.
func TestManifestLoader_ImageTagsHaveRegistryPrefix(t *testing.T) {
	cfg, cachePath := testCfg(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg.DockerSwarm.ManifestURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loader, err := NewManifestLoader(ctx, cfg, testutils.LoggerFactory(t),
		withCachePath(cachePath),
		withHTTPClient(srv.Client()),
		withEmbeddedFallback(),
	)
	if err != nil {
		t.Fatalf("NewManifestLoader: %v", err)
	}

	for _, pv := range loader.Versions().Supported() {
		imgs, err := loader.Versions().GetImages(pv)
		if err != nil {
			t.Errorf("GetImages(%s): %v", pv, err)
			continue
		}
		if !strings.HasPrefix(imgs.PgEdgeImage, "ghcr.io/pgedge/") {
			t.Errorf("PgEdgeImage %q missing registry prefix", imgs.PgEdgeImage)
		}
	}

	for _, svc := range []string{"mcp", "postgrest", "rag"} {
		vers, _ := loader.ServiceVersions().SupportedServiceVersions(svc)
		for _, ver := range vers {
			img, err := loader.ServiceVersions().GetServiceImage(svc, ver)
			if err != nil {
				t.Errorf("GetServiceImage(%q, %q): %v", svc, ver, err)
				continue
			}
			if !strings.HasPrefix(img.Tag, "ghcr.io/pgedge/") {
				t.Errorf("service %q version %q Tag %q missing registry prefix", svc, ver, img.Tag)
			}
		}
	}
}
