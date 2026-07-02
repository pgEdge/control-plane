package swarm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "embed"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/logging"
)

//go:embed version-manifest.json
var embeddedManifest []byte

const (
	refreshInterval        = time.Hour
	fetchTimeout           = 10 * time.Second
	supportedSchemaVersion = 1
)

// manifestCachePath returns the cache file path for a given manifest URL.
// The filename embeds a short hash of the URL so that switching to a different
// URL automatically uses a fresh cache file instead of a stale one.
func manifestCachePath(dir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(dir, fmt.Sprintf("version-manifest-cache-%s.json", hex.EncodeToString(sum[:8])))
}

// versionManifestImages is the typed images block inside version-manifest.json.
type versionManifestImages struct {
	Postgres   []manifestPostgresEntry `json:"postgres"`
	PostgREST  []manifestServiceEntry  `json:"postgrest"`
	MCP        []manifestServiceEntry  `json:"mcp"`
	RAG        []manifestServiceEntry  `json:"rag"`
	Lakekeeper []manifestServiceEntry  `json:"lakekeeper"`
}

// versionManifest is the top-level structure of version-manifest.json.
type versionManifest struct {
	SchemaVersion int                   `json:"schema_version"`
	Images        versionManifestImages `json:"images"`
}

// manifestPostgresEntry is one entry under images.postgres in the manifest.
type manifestPostgresEntry struct {
	PostgresVersion string `json:"postgres_version"`
	SpockVersion    string `json:"spock_version"`
	Image           string `json:"image"`
	Stability       string `json:"stability"`
	Default         bool   `json:"default"`
}

// manifestServiceEntry is one entry under images.<service> in the manifest.
type manifestServiceEntry struct {
	Version   string `json:"version"`
	Image     string `json:"image"`
	Stability string `json:"stability"`
	Default   bool   `json:"default"`
}

// manifestLoaderOption is a functional option for ManifestLoader, used in
// tests to inject test doubles.
type manifestLoaderOption func(*ManifestLoader)

func withCachePath(path string) manifestLoaderOption {
	return func(m *ManifestLoader) { m.cachePath = path }
}

func withHTTPClient(c *http.Client) manifestLoaderOption {
	return func(m *ManifestLoader) { m.httpClient = c }
}

func withTickerC(ch <-chan time.Time) manifestLoaderOption {
	return func(m *ManifestLoader) { m.tickerC = ch }
}

// withEmbeddedFallback enables the embedded-manifest fallback regardless of
// the configured URL.  Used in tests that simulate URL failure while still
// exercising the default-URL resolution chain.
func withEmbeddedFallback() manifestLoaderOption {
	return func(m *ManifestLoader) { m.embeddedFallback = true }
}

// ManifestLoader loads the version manifest from a remote URL (with disk
// caching and hourly refresh) or from a local file, and exposes the parsed
// *Versions and *ServiceVersions it produces.
//
// Resolution chains:
//
//  1. manifest_path set (local file): local file only — no fallback, returns
//     error if the file is missing or invalid.
//
//  2. Custom manifest_url (differs from DefaultManifestURL): remote URL →
//     disk cache — no embedded fallback; returns error if all sources fail.
//
//  3. Default manifest_url: remote URL → disk cache → embedded manifest
//     (always succeeds; panics only if the embedded JSON is corrupt, which
//     indicates a broken build).
type ManifestLoader struct {
	cfg              config.Config
	logger           zerolog.Logger
	cachePath        string
	httpClient       *http.Client
	tickerC          <-chan time.Time // nil → use default hourly ticker; injectable for tests
	embeddedFallback bool             // set when using the default URL; injectable for tests

	mu          sync.RWMutex
	versions    *Versions
	svcVersions *ServiceVersions
}

// NewManifestLoader creates and starts a ManifestLoader.  It loads the
// manifest synchronously before returning so callers always get a valid
// *Versions / *ServiceVersions immediately.  Returns an error when using a
// custom manifest_path or manifest_url and the manifest cannot be loaded.
// Background refresh (if applicable) is started as a goroutine tied to ctx.
func NewManifestLoader(ctx context.Context, cfg config.Config, loggerFactory *logging.Factory, opts ...manifestLoaderOption) (*ManifestLoader, error) {
	m := &ManifestLoader{
		cfg:              cfg,
		logger:           loggerFactory.Logger(logging.ComponentManifestLoader),
		cachePath:        manifestCachePath(filepath.Join(cfg.DataDir, "manifests"), cfg.DockerSwarm.ManifestURL),
		embeddedFallback: cfg.DockerSwarm.ManifestURL == config.DefaultManifestURL,
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
	}
	for _, o := range opts {
		o(m)
	}

	if err := m.load(ctx); err != nil {
		return nil, err
	}

	if cfg.DockerSwarm.ManifestPath == "" {
		go m.refreshLoop(ctx)
	}

	return m, nil
}

// Versions returns a snapshot of the current parsed Versions.
func (m *ManifestLoader) Versions() Versions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.versions
}

// ServiceVersions returns a snapshot of the current parsed ServiceVersions.
func (m *ManifestLoader) ServiceVersions() ServiceVersions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.svcVersions
}

// load performs the initial synchronous load.
func (m *ManifestLoader) load(ctx context.Context) error {
	data, src, err := m.resolve(ctx)
	if err != nil {
		return err
	}
	v, sv, err := m.parseManifestData(data)
	if err != nil {
		if src == "embedded" {
			// The embedded manifest is corrupt — this is a build-time error.
			panic(fmt.Sprintf("manifest_loader: corrupt embedded manifest: %v", err))
		}
		return fmt.Errorf("parse manifest from %s: %w", src, err)
	}
	m.mu.Lock()
	m.versions = v
	m.svcVersions = sv
	m.mu.Unlock()
	m.logger.Info().Str("source", src).Msg("version manifest loaded")
	return nil
}

// resolve selects the appropriate resolution chain and returns raw manifest
// bytes plus a human-readable source label.
func (m *ManifestLoader) resolve(ctx context.Context) ([]byte, string, error) {
	if p := m.cfg.DockerSwarm.ManifestPath; p != "" {
		return m.resolveLocalPath(p)
	}
	return m.resolveURL(ctx)
}

// resolveLocalPath reads and validates a manifest from a local file path.
// No fallback — returns an error if the file is missing or invalid.
func (m *ManifestLoader) resolveLocalPath(p string) ([]byte, string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, "", fmt.Errorf("manifest_path %s: %w", p, err)
	}
	if err = m.validateManifest(data); err != nil {
		return nil, "", fmt.Errorf("manifest_path %s validation failed: %w", p, err)
	}
	return data, "file:" + p, nil
}

// resolveURL tries to load the manifest from the configured URL, falling back
// to the disk cache.  For the default URL it also falls back to the embedded
// manifest.  For a custom URL it returns an error if all sources fail so the
// operator knows their configuration is broken.
func (m *ManifestLoader) resolveURL(ctx context.Context) ([]byte, string, error) {
	u := m.cfg.DockerSwarm.ManifestURL

	data, err := m.fetchURL(ctx, u)
	if err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("failed to fetch manifest from URL; trying disk cache")
	} else if err = m.validateManifest(data); err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("remote manifest validation failed; trying disk cache")
	} else {
		_ = m.writeCache(data)
		return data, "url:" + u, nil
	}

	if cached, err := os.ReadFile(m.cachePath); err == nil {
		if err = m.validateManifest(cached); err != nil {
			m.logger.Warn().Err(err).Str("path", m.cachePath).Msg("cached manifest validation failed; skipping cache")
		} else {
			m.logger.Info().Str("path", m.cachePath).Msg("using cached manifest")
			return cached, "cache:" + m.cachePath, nil
		}
	}

	if m.embeddedFallback {
		m.logger.Warn().Msg("using embedded manifest; consider checking connectivity to manifest URL")
		return embeddedManifest, "embedded", nil
	}

	return nil, "", fmt.Errorf("failed to load manifest from %s: URL and cache both unavailable", u)
}

// fetchURL fetches bytes from the given URL using the configured http.Client.
func (m *ManifestLoader) fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// writeCache persists data to the cache path, creating parent directories as
// needed.  Errors are logged but not returned (caching is best-effort).
func (m *ManifestLoader) writeCache(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0o700); err != nil {
		m.logger.Warn().Err(err).Str("path", m.cachePath).Msg("failed to create manifest cache directory")
		return err
	}
	if err := os.WriteFile(m.cachePath, data, 0o600); err != nil {
		m.logger.Warn().Err(err).Str("path", m.cachePath).Msg("failed to write manifest cache")
		return err
	}
	return nil
}

// refreshLoop runs in the background and refreshes the manifest every hour.
// It stops when ctx is cancelled.
func (m *ManifestLoader) refreshLoop(ctx context.Context) {
	tickC := m.tickerC
	var ticker *time.Ticker
	if tickC == nil {
		ticker = time.NewTicker(refreshInterval)
		defer ticker.Stop()
		tickC = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tickC:
			m.refresh(ctx)
		}
	}
}

// refresh fetches a fresh manifest from the URL, validates it, updates the
// disk cache, and swaps the in-memory versions atomically.  Failures are
// logged but do not disturb the current in-memory versions.
func (m *ManifestLoader) refresh(ctx context.Context) {
	u := m.cfg.DockerSwarm.ManifestURL
	data, err := m.fetchURL(ctx, u)
	if err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("manifest refresh: fetch failed; keeping current versions")
		return
	}
	if err = m.validateManifest(data); err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("manifest refresh: validation failed; keeping current versions")
		return
	}
	v, sv, err := m.parseManifestData(data)
	if err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("manifest refresh: parse failed; keeping current versions")
		return
	}
	_ = m.writeCache(data)

	m.mu.Lock()
	m.versions = v
	m.svcVersions = sv
	m.mu.Unlock()
	m.logger.Info().Str("url", u).Msg("version manifest refreshed")
}

// validateManifest checks that data is valid JSON, has a supported
// schema_version, and can be fully parsed into *Versions/*ServiceVersions.
// Performing a full parse here ensures resolve() only returns data that will
// succeed in parseManifestData.
func (m *ManifestLoader) validateManifest(data []byte) error {
	var mf versionManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if mf.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (want %d)", mf.SchemaVersion, supportedSchemaVersion)
	}
	if _, _, err := m.parseManifestData(data); err != nil {
		return fmt.Errorf("manifest not parseable: %w", err)
	}
	return nil
}

// parseManifestData unmarshals data into *Versions and *ServiceVersions.
func (m *ManifestLoader) parseManifestData(data []byte) (*Versions, *ServiceVersions, error) {
	var mf versionManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	v, err := buildVersions(m.cfg, &mf)
	if err != nil {
		return nil, nil, fmt.Errorf("build versions: %w", err)
	}

	sv, err := buildServiceVersions(m.cfg, &mf)
	if err != nil {
		return nil, nil, fmt.Errorf("build service versions: %w", err)
	}

	return v, sv, nil
}

// buildVersions parses the images.postgres section of the manifest and
// returns a *Versions equivalent to NewVersions(cfg) for the same data.
func buildVersions(cfg config.Config, mf *versionManifest) (*Versions, error) {
	entries := mf.Images.Postgres
	if len(entries) == 0 {
		return nil, fmt.Errorf("manifest missing images.postgres")
	}

	versions := &Versions{
		cfg:    cfg,
		images: make(map[string]map[string]*Images),
	}

	var defaultVer *ds.PgEdgeVersion
	for _, e := range entries {
		pv, err := ds.ParsePgEdgeVersion(e.PostgresVersion, e.SpockVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid version entry {postgres:%s spock:%s}: %w",
				e.PostgresVersion, e.SpockVersion, err)
		}
		img := &Images{
			PgEdgeImage: serviceImageTag(cfg, e.Image),
		}
		versions.addImage(pv, img)
		if e.Default {
			defaultVer = pv
		}
	}

	if defaultVer == nil {
		// Fall back to the last entry if no default is marked.
		defaultVer = versions.supportedVersions[len(versions.supportedVersions)-1]
	}
	versions.defaultVersion = defaultVer

	return versions, nil
}

// buildServiceVersions parses all non-postgres image sections and returns a
// *ServiceVersions equivalent to NewServiceVersions(cfg) for the same data.
func buildServiceVersions(cfg config.Config, mf *versionManifest) (*ServiceVersions, error) {
	sv := &ServiceVersions{
		cfg:    cfg,
		images: make(map[string]map[string]*ServiceImage),
	}

	type svcSection struct {
		name    string
		entries []manifestServiceEntry
	}
	for _, s := range []svcSection{
		{"postgrest", mf.Images.PostgREST},
		{"mcp", mf.Images.MCP},
		{"rag", mf.Images.RAG},
		{"lakekeeper", mf.Images.Lakekeeper},
	} {
		var defaultImage, lastImage *ServiceImage
		for _, e := range s.entries {
			img := &ServiceImage{Tag: serviceImageTag(cfg, e.Image)}
			sv.addServiceImage(s.name, e.Version, img)
			if e.Default {
				defaultImage = img
			}
			lastImage = img
		}
		if defaultImage == nil {
			defaultImage = lastImage
		}
		if defaultImage != nil {
			sv.addServiceImage(s.name, "latest", defaultImage)
		}
	}

	return sv, nil
}
