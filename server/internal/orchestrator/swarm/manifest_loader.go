package swarm

import (
	"context"
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
)

//go:embed version-manifest.json
var embeddedManifest []byte

const (
	// DefaultManifestURL is the URL used when no manifest_url is configured.
	// TODO(PLAT-598): Replace with the real  URL once the hosting location is confirmed.
	DefaultManifestURL = "https://download.pgedge.com/manifests/version-manifest.json"

	manifestCacheFilename  = "version-manifest-cache.json"
	refreshInterval        = time.Hour
	fetchTimeout           = 10 * time.Second
	supportedSchemaVersion = 1
)

// versionManifest is the top-level structure of version-manifest.json.
type versionManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	Images        map[string]json.RawMessage `json:"images"`
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

// ManifestLoader loads the version manifest from a remote URL (with disk
// caching and hourly refresh) or from a local file, and exposes the parsed
// *Versions and *ServiceVersions it produces.
//
// Resolution order on startup:
//  1. Local file (cfg.DockerSwarm.ManifestPath), if set — no network fetch, no refresh.
//  2. Remote URL (cfg.DockerSwarm.ManifestURL, defaulting to DefaultManifestURL).
//  3. Disk cache at defaultCachePath (or the path set by withCachePath).
//  4. Embedded binary manifest (always succeeds; panics only if the embedded
//     JSON is corrupt, which would indicate a broken build).
type ManifestLoader struct {
	cfg        config.Config
	logger     zerolog.Logger
	cachePath  string
	httpClient *http.Client
	tickerC    <-chan time.Time // nil → use default hourly ticker; injectable for tests

	mu          sync.RWMutex
	versions    *Versions
	svcVersions *ServiceVersions
}

// NewManifestLoader creates and starts a ManifestLoader.  It loads the
// manifest synchronously before returning so callers always get a valid
// *Versions / *ServiceVersions immediately.  Background refresh (if
// applicable) is started as a goroutine tied to ctx.
func NewManifestLoader(ctx context.Context, cfg config.Config, logger zerolog.Logger, opts ...manifestLoaderOption) *ManifestLoader {
	m := &ManifestLoader{
		cfg:       cfg,
		logger:    logger.With().Str("component", "manifest_loader").Logger(),
		cachePath: filepath.Join(cfg.DataDir, "manifests", manifestCacheFilename),
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
	}
	for _, o := range opts {
		o(m)
	}

	m.load()

	if cfg.DockerSwarm.ManifestPath == "" {
		go m.refreshLoop(ctx)
	}

	return m
}

// Versions returns the current parsed *Versions.
func (m *ManifestLoader) Versions() *Versions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.versions
}

// ServiceVersions returns the current parsed *ServiceVersions.
func (m *ManifestLoader) ServiceVersions() *ServiceVersions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.svcVersions
}

// load performs the initial synchronous load, trying each source in order.
func (m *ManifestLoader) load() {
	data, src := m.resolve()
	v, sv, err := m.parseManifestData(data)
	if err != nil {
		// resolve() fully parse-validates every non-embedded source before
		// returning it, so this branch is only reachable when the embedded
		// manifest itself is corrupt — a build-time error.
		panic(fmt.Sprintf("manifest_loader: failed to parse embedded manifest (%s): %v", src, err))
	}
	m.mu.Lock()
	m.versions = v
	m.svcVersions = sv
	m.mu.Unlock()
	m.logger.Info().Str("source", src).Msg("version manifest loaded")
}

// resolve returns the raw manifest bytes and a human-readable source label,
// falling back through the resolution order.
func (m *ManifestLoader) resolve() ([]byte, string) {
	// 1. Local file override.
	if p := m.cfg.DockerSwarm.ManifestPath; p != "" {
		data, err := os.ReadFile(p)
		if err != nil {
			m.logger.Warn().Err(err).Str("path", p).Msg("failed to read manifest_path; falling back to embedded manifest")
		} else if err = m.validateManifest(data); err != nil {
			m.logger.Warn().Err(err).Str("path", p).Msg("manifest_path validation failed; falling back to embedded manifest")
		} else {
			return data, "file:" + p
		}
		// manifest_path was set but unusable — skip URL/cache and use embedded.
		return embeddedManifest, "embedded"
	}

	// 2. Remote URL.
	u := m.manifestURL()
	data, err := m.fetchURL(context.Background(), u)
	if err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("failed to fetch manifest from URL; trying disk cache")
	} else if err = m.validateManifest(data); err != nil {
		m.logger.Warn().Err(err).Str("url", u).Msg("remote manifest validation failed; trying disk cache")
	} else {
		_ = m.writeCache(data)
		return data, "url:" + u
	}

	// 3. Disk cache.
	if cached, err := os.ReadFile(m.cachePath); err == nil {
		if err = m.validateManifest(cached); err != nil {
			m.logger.Warn().Err(err).Str("path", m.cachePath).Msg("cached manifest validation failed; falling back to embedded manifest")
		} else {
			m.logger.Info().Str("path", m.cachePath).Msg("using cached manifest")
			return cached, "cache:" + m.cachePath
		}
	}

	// 4. Embedded binary manifest.
	m.logger.Warn().Msg("using embedded manifest; consider checking connectivity to manifest URL")
	return embeddedManifest, "embedded"
}

// manifestURL returns the configured URL, or DefaultManifestURL if unset.
func (m *ManifestLoader) manifestURL() string {
	if u := m.cfg.DockerSwarm.ManifestURL; u != "" {
		return u
	}
	return DefaultManifestURL
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
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0o755); err != nil {
		m.logger.Warn().Err(err).Str("path", m.cachePath).Msg("failed to create manifest cache directory")
		return err
	}
	if err := os.WriteFile(m.cachePath, data, 0o644); err != nil {
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
	u := m.manifestURL()
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
// Performing a full parse here means resolve() only returns data that will
// succeed in parseManifestData, so load()'s panic is truly unreachable except
// for a corrupt embedded manifest.
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
	raw, ok := mf.Images["postgres"]
	if !ok {
		return nil, fmt.Errorf("manifest missing images.postgres")
	}

	var entries []manifestPostgresEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal images.postgres: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("images.postgres is empty")
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

	for svcType, raw := range mf.Images {
		if svcType == "postgres" {
			continue
		}
		var entries []manifestServiceEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("unmarshal images.%s: %w", svcType, err)
		}
		for _, e := range entries {
			sv.addServiceImage(svcType, e.Version, &ServiceImage{
				Tag: serviceImageTag(cfg, e.Image),
			})
		}
	}

	return sv, nil
}
