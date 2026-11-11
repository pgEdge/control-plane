package common

import (
	"flag"
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const (
	testMCPConfigDir = "/etc/pgedge-mcp/inst-1"
	testMCPDataDir   = "/var/lib/pgedge/postgres-mcp/inst-1"
)

func testMCPPaths() MCPConfigPaths {
	return MCPConfigPaths{
		HTTPAddress: ":9000",
		TokenFile:   testMCPDataDir + "/tokens.yaml",
		UserFile:    testMCPDataDir + "/users.yaml",
		TraceFile:   testMCPDataDir + "/audit-trace.log",
		DataDir:     testMCPDataDir,
	}
}

func testMCPConfigResource(cfg *database.MCPServiceConfig) *MCPConfigResource {
	return &MCPConfigResource{
		ServiceInstanceID:   "inst-1",
		ServiceID:           "mcp-server",
		HostID:              "host-1",
		ConfigDirResourceID: "inst-1-config",
		DataDirResourceID:   "inst-1-data",
		Config:              cfg,
		DatabaseName:        "mydb",
		DatabaseHosts:       []database.ServiceHostEntry{{Host: "10.0.0.1", Port: 5432}},
		ConnectAsUsername:   "app",
		ConnectAsPassword:   "secret",
		Paths:               testMCPPaths(),
		OwnerUID:            999,
		OwnerGID:            999,
	}
}

func mcpConfigTestContext(t *testing.T) (*resource.Context, afero.Fs, *reloadRecorder) {
	t.Helper()
	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll(testMCPConfigDir, 0o700))
	require.NoError(t, afs.MkdirAll(testMCPDataDir, 0o700))

	orch := &reloadRecorder{}
	injector := do.New()
	do.ProvideValue[afero.Fs](injector, afs)
	do.ProvideValue[database.Orchestrator](injector, orch)
	do.ProvideValue(injector, zerolog.Nop())

	state := resource.NewState()
	for id, path := range map[string]string{"inst-1-config": testMCPConfigDir, "inst-1-data": testMCPDataDir} {
		dir, err := resource.ToResourceData(&filesystem.DirResource{ID: id, HostID: "host-1", Path: path, FullPath: path})
		require.NoError(t, err)
		state.Add(dir)
	}

	return &resource.Context{State: state, Injector: injector}, afs, orch
}

func TestMCPConfigResource_Dependencies(t *testing.T) {
	r := testMCPConfigResource(&database.MCPServiceConfig{})
	assert.ElementsMatch(t, []resource.Identifier{
		filesystem.DirResourceIdentifier("inst-1-config"),
		filesystem.DirResourceIdentifier("inst-1-data"),
	}, r.Dependencies())

	r.DataDirResourceID = r.ConfigDirResourceID
	assert.Equal(t, []resource.Identifier{filesystem.DirResourceIdentifier("inst-1-config")}, r.Dependencies())
}

func TestMCPConfigResource_Create(t *testing.T) {
	rc, afs, _ := mcpConfigTestContext(t)
	r := testMCPConfigResource(&database.MCPServiceConfig{
		InitToken: utils.PointerTo("bootstrap"),
	})
	require.NoError(t, r.Create(t.Context(), rc))

	config, err := afero.ReadFile(afs, filepath.Join(testMCPConfigDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(config), `address: :9000`)
	assert.Contains(t, string(config), "data_dir: "+testMCPDataDir)

	tokens, err := afero.ReadFile(afs, filepath.Join(testMCPDataDir, "tokens.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(tokens), "bootstrap-token")
	users, err := afero.ReadFile(afs, filepath.Join(testMCPDataDir, "users.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(users), "users: {}")

	// The auth stores are application-owned, so they're never overwritten.
	require.NoError(t, afero.WriteFile(afs, filepath.Join(testMCPDataDir, "tokens.yaml"), []byte("tokens: {}\n"), 0o600))
	require.NoError(t, r.Create(t.Context(), rc))
	tokens, err = afero.ReadFile(afs, filepath.Join(testMCPDataDir, "tokens.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "tokens: {}\n", string(tokens))
}

func TestMCPConfigResource_RefreshDetectsDrift(t *testing.T) {
	rc, afs, _ := mcpConfigTestContext(t)
	r := testMCPConfigResource(&database.MCPServiceConfig{})
	require.NoError(t, r.Create(t.Context(), rc))

	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.False(t, r.ConfigDrifted, "unchanged file should not be drifted")

	require.NoError(t, afero.WriteFile(afs, filepath.Join(testMCPConfigDir, "config.yaml"), []byte("http: {}\n"), 0o600))
	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.True(t, r.ConfigDrifted, "edited file should be drifted")
}

func TestMCPConfigResource_UpdateFrom(t *testing.T) {
	llm := func(model string) *database.MCPServiceConfig {
		return &database.MCPServiceConfig{
			LLMEnabled:      utils.PointerTo(true),
			LLMProvider:     "anthropic",
			LLMModel:        model,
			AnthropicAPIKey: utils.PointerTo("sk-ant"),
		}
	}
	withHosts := func(r *MCPConfigResource, hosts ...database.ServiceHostEntry) *MCPConfigResource {
		r.DatabaseHosts = hosts
		return r
	}
	withPort := func(r *MCPConfigResource, address string) *MCPConfigResource {
		r.Paths.HTTPAddress = address
		return r
	}
	drifted := testMCPConfigResource(llm("model-a"))
	drifted.ConfigDrifted = true

	for _, tc := range []struct {
		name           string
		previous       *MCPConfigResource
		previousFailed bool
		current        *MCPConfigResource
		expected       []database.ReloadMode
	}{
		{
			name:    "no previous state",
			current: testMCPConfigResource(llm("model-a")),
		},
		{
			name:     "no change",
			previous: testMCPConfigResource(llm("model-a")),
			current:  testMCPConfigResource(llm("model-a")),
		},
		{
			name:     "database connection change is reloaded",
			previous: testMCPConfigResource(llm("model-a")),
			current: withHosts(testMCPConfigResource(llm("model-a")),
				database.ServiceHostEntry{Host: "10.0.0.2", Port: 5432},
				database.ServiceHostEntry{Host: "10.0.0.1", Port: 5432}),
			expected: []database.ReloadMode{database.ReloadModeSighup},
		},
		{
			name:     "llm change restarts",
			previous: testMCPConfigResource(llm("model-a")),
			current:  testMCPConfigResource(llm("model-b")),
			expected: []database.ReloadMode{database.ReloadModeRestart},
		},
		{
			name:     "listen address change restarts",
			previous: testMCPConfigResource(llm("model-a")),
			current:  withPort(testMCPConfigResource(llm("model-a")), ":9001"),
			expected: []database.ReloadMode{database.ReloadModeRestart},
		},
		{
			name:           "retry after a failed update restarts",
			previous:       testMCPConfigResource(llm("model-a")),
			previousFailed: true,
			current:        testMCPConfigResource(llm("model-a")),
			expected:       []database.ReloadMode{database.ReloadModeRestart},
		},
		{
			name:     "drifted file restarts",
			previous: drifted,
			current:  testMCPConfigResource(llm("model-a")),
			expected: []database.ReloadMode{database.ReloadModeRestart},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, orch := mcpConfigTestContext(t)
			require.NoError(t, tc.current.UpdateFrom(t.Context(), rc, tc.previous, tc.previousFailed))
			assert.Equal(t, tc.expected, orch.modes)
		})
	}
}

func TestMCPReloadModes(t *testing.T) {
	render := func(cfg *database.MCPServiceConfig, hosts []database.ServiceHostEntry) []byte {
		t.Helper()
		out, err := GenerateMCPConfig(&MCPConfigParams{
			Config:        cfg,
			DatabaseName:  "mydb",
			DatabaseHosts: hosts,
			Username:      "app",
			Password:      "secret",
			Paths:         testMCPPaths(),
		})
		require.NoError(t, err)
		return out
	}
	oneHost := []database.ServiceHostEntry{{Host: "10.0.0.1", Port: 5432}}
	trace := func(metadataOnly bool) *database.MCPServiceConfig {
		return &database.MCPServiceConfig{
			AuditTraceEnabled:      utils.PointerTo(true),
			AuditTraceMetadataOnly: utils.PointerTo(metadataOnly),
		}
	}

	modes, err := mcpReloadModes(render(trace(true), oneHost), render(trace(false), oneHost))
	require.NoError(t, err)
	assert.Equal(t, []database.ReloadMode{database.ReloadModeSighup}, modes, "trace_metadata_only is applied on SIGHUP")

	modes, err = mcpReloadModes(render(&database.MCPServiceConfig{}, oneHost), render(trace(true), oneHost))
	require.NoError(t, err)
	assert.Equal(t, []database.ReloadMode{database.ReloadModeRestart}, modes, "trace_file needs a restart")

	modes, err = mcpReloadModes(render(&database.MCPServiceConfig{}, oneHost), render(&database.MCPServiceConfig{PoolMaxConns: utils.PointerTo(8)}, oneHost))
	require.NoError(t, err)
	assert.Equal(t, []database.ReloadMode{database.ReloadModeSighup}, modes, "database settings are applied on SIGHUP")
}

func TestMCPConfigResource_CheckKBFile(t *testing.T) {
	const kbPath = "/srv/kb/kb.db"

	t.Run("disabled", func(t *testing.T) {
		r := testMCPConfigResource(&database.MCPServiceConfig{})
		assert.NoError(t, r.checkKBFile(afero.NewMemMapFs()))
	})
	t.Run("missing", func(t *testing.T) {
		r := testMCPConfigResource(&database.MCPServiceConfig{})
		r.KBHostPath = kbPath
		assert.ErrorContains(t, r.checkKBFile(afero.NewMemMapFs()), "KB database file not found at /srv/kb/kb.db")
	})
	t.Run("directory", func(t *testing.T) {
		afs := afero.NewMemMapFs()
		require.NoError(t, afs.MkdirAll(kbPath, 0o755))
		r := testMCPConfigResource(&database.MCPServiceConfig{})
		r.KBHostPath = kbPath
		assert.ErrorContains(t, r.checkKBFile(afs), "is a directory, not a file")
	})
	t.Run("present", func(t *testing.T) {
		afs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(afs, kbPath, nil, 0o644))
		r := testMCPConfigResource(&database.MCPServiceConfig{})
		r.KBHostPath = kbPath
		assert.NoError(t, r.checkKBFile(afs))
	})
}

type fakeFileInfo struct {
	mode     fs.FileMode
	uid, gid uint32
}

func (f fakeFileInfo) Name() string       { return "kb.db" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid, Gid: f.gid} }

func TestReadableBy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		info     fakeFileInfo
		expected bool
	}{
		{name: "owner can read", info: fakeFileInfo{mode: 0o400, uid: 999, gid: 0}, expected: true},
		{name: "owner can't read", info: fakeFileInfo{mode: 0o044, uid: 999, gid: 999}, expected: false},
		{name: "group can read", info: fakeFileInfo{mode: 0o040, uid: 0, gid: 999}, expected: true},
		{name: "other can read", info: fakeFileInfo{mode: 0o004, uid: 0, gid: 0}, expected: true},
		{name: "root-only file", info: fakeFileInfo{mode: 0o600, uid: 0, gid: 0}, expected: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, readableBy(tc.info, 999, 999))
		})
	}
}

// TestMCPConfigGolden pins the config.yaml rendered for systemd hosts, where
// the server reads and writes its files at their host paths.
func TestMCPConfigGolden(t *testing.T) {
	// The update flag is registered by TestMain in the external test package.
	update := flag.Lookup("update").Value.String() == "true"
	golden := &testutils.GoldenTest[string]{
		FileExtension: ".yaml",
		Marshal: func(v any) ([]byte, error) {
			return []byte(v.(string)), nil
		},
		Unmarshal: func(data []byte, v any) error {
			*v.(*string) = string(data)
			return nil
		},
	}

	for _, tc := range []struct {
		name   string
		config *database.MCPServiceConfig
		kbPath string
	}{
		{
			name:   "systemd minimal",
			config: &database.MCPServiceConfig{},
		},
		{
			name: "systemd kb and audit trace",
			config: &database.MCPServiceConfig{
				KBEnabled:           utils.PointerTo(true),
				KBEmbeddingProvider: utils.PointerTo("voyage"),
				KBEmbeddingModel:    utils.PointerTo("voyage-3"),
				KBEmbeddingAPIKey:   utils.PointerTo("kb-key"),
				AuditTraceEnabled:   utils.PointerTo(true),
			},
			kbPath: "/srv/kb/kb.db",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := testMCPPaths()
			paths.KBDatabasePath = tc.kbPath
			out, err := GenerateMCPConfig(&MCPConfigParams{
				Config: tc.config,
				DatabaseHosts: []database.ServiceHostEntry{
					{Host: "10.0.0.1", Port: 6432},
					{Host: "10.0.0.2", Port: 6433},
				},
				TargetSessionAttrs: "read-write",
				DatabaseName:       "mydb",
				Username:           "app",
				Password:           "secret",
				Paths:              paths,
			})
			require.NoError(t, err)
			golden.Run(t, string(out), update)
		})
	}
}
