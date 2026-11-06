package common

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestPostgRESTConfigResource_ResourceVersion(t *testing.T) {
	r := &PostgRESTConfigResource{}
	assert.Equal(t, "1", r.ResourceVersion())
}

func TestPostgRESTConfigResource_DiffIgnore(t *testing.T) {
	r := &PostgRESTConfigResource{}
	assert.Nil(t, r.DiffIgnore())
}

func TestPostgRESTConfigResourceIdentifier(t *testing.T) {
	id := PostgRESTConfigResourceIdentifier("inst-1")
	assert.Equal(t, "inst-1", id.ID)
	assert.Equal(t, ResourceTypePostgRESTConfig, id.Type)
}

func TestPostgRESTConfigResource_Identifier(t *testing.T) {
	r := &PostgRESTConfigResource{ServiceInstanceID: "inst-abc"}
	id := r.Identifier()
	assert.Equal(t, "inst-abc", id.ID)
	assert.Equal(t, ResourceTypePostgRESTConfig, id.Type)
}

func TestPostgRESTConfigResource_Executor(t *testing.T) {
	r := &PostgRESTConfigResource{HostID: "host-2"}
	assert.Equal(t, resource.HostExecutor("host-2"), r.Executor())
}

func TestPostgRESTConfigResource_TypeDependencies(t *testing.T) {
	r := &PostgRESTConfigResource{}
	assert.Nil(t, r.TypeDependencies())
}

func TestPostgRESTConfigResource_Dependencies(t *testing.T) {
	r := &PostgRESTConfigResource{
		ServiceInstanceID: "inst-1",
		DirResourceID:     "inst-1-data",
	}
	deps := r.Dependencies()

	require.Len(t, deps, 1)
	assert.Equal(t, filesystem.DirResourceIdentifier("inst-1-data"), deps[0])
}

func TestPostgRESTConfigResource_WriteConfigFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	dirPath := "/var/lib/pgedge/services/inst-1"
	require.NoError(t, fs.MkdirAll(dirPath, 0o755))

	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     10,
		MaxRows:    1000,
	}
	r := &PostgRESTConfigResource{
		Config:            cfg,
		ConnectAsUsername: "myapp",
		ConnectAsPassword: "s3cr3t",
		DatabaseName:      "mydb",
		DatabaseHosts:     []database.ServiceHostEntry{{Host: "pg-host1", Port: 5432}},
	}

	err := r.writeConfigFile(fs, dirPath)
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, filepath.Join(dirPath, "postgrest.conf"))
	require.NoError(t, err, "postgrest.conf must exist after writeConfigFile")

	content := string(data)
	assert.Contains(t, content, "db-uri")
	assert.Contains(t, content, "myapp", "username must appear in db-uri")
	assert.Contains(t, content, "s3cr3t", "password must appear in db-uri")
	assert.Contains(t, content, "pg-host1", "host must appear in db-uri")
	assert.Contains(t, content, "mydb", "database name must appear in db-uri")
	assert.Contains(t, content, "db-schemas")
	assert.Contains(t, content, "public")
	assert.Contains(t, content, "db-anon-role")
	assert.Contains(t, content, "web_anon")
}

func TestPostgRESTConfigResource_WriteConfigFile_JWTFields(t *testing.T) {
	fs := afero.NewMemMapFs()
	dirPath := "/var/lib/pgedge/services/inst-jwt"
	require.NoError(t, fs.MkdirAll(dirPath, 0o755))

	secret := "a-very-long-jwt-secret-that-is-at-least-32-chars"
	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     5,
		MaxRows:    500,
		JWTSecret:  &secret,
	}
	r := &PostgRESTConfigResource{
		Config:            cfg,
		ConnectAsUsername: "app",
		ConnectAsPassword: "pass",
		DatabaseName:      "mydb",
		DatabaseHosts:     []database.ServiceHostEntry{{Host: "pg-host1", Port: 5432}},
	}

	err := r.writeConfigFile(fs, dirPath)
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, filepath.Join(dirPath, "postgrest.conf"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "jwt-secret")
	assert.Contains(t, content, secret)
}

func TestPostgRESTConfigResource_WriteConfigFile_MultiHost(t *testing.T) {
	fs := afero.NewMemMapFs()
	dirPath := "/var/lib/pgedge/services/inst-multi"
	require.NoError(t, fs.MkdirAll(dirPath, 0o755))

	cfg := &database.PostgRESTServiceConfig{
		DBSchemas:  "public",
		DBAnonRole: "web_anon",
		DBPool:     10,
		MaxRows:    1000,
	}
	r := &PostgRESTConfigResource{
		Config:            cfg,
		ConnectAsUsername: "app",
		ConnectAsPassword: "pass",
		DatabaseName:      "mydb",
		DatabaseHosts: []database.ServiceHostEntry{
			{Host: "pg-host1", Port: 5432},
			{Host: "pg-host2", Port: 5432},
		},
		TargetSessionAttrs: "read-write",
	}

	err := r.writeConfigFile(fs, dirPath)
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, filepath.Join(dirPath, "postgrest.conf"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "pg-host1")
	assert.Contains(t, content, "pg-host2")
	assert.True(t, strings.Contains(content, "target_session_attrs") ||
		strings.Contains(content, "read-write"),
		"multi-host URI should contain target_session_attrs or read-write")
}

func TestPostgRESTReloadModes(t *testing.T) {
	base := &PostgRESTConfigResource{
		Config: &database.PostgRESTServiceConfig{
			DBSchemas:  "public",
			DBAnonRole: "web_anon",
			DBPool:     10,
			MaxRows:    1000,
		},
		ConnectAsUsername: "app",
		ConnectAsPassword: "pass",
		DatabaseName:      "mydb",
		DatabaseHosts:     []database.ServiceHostEntry{{Host: "pg-host1", Port: 5432}},
	}
	render := func(t *testing.T, modify func(r *PostgRESTConfigResource)) []byte {
		t.Helper()
		r := *base
		cfg := *base.Config
		r.Config = &cfg
		modify(&r)
		content, err := r.generateConf()
		require.NoError(t, err)
		return content
	}
	reload := []database.ReloadMode{database.ReloadModeSigusr2, database.ReloadModeSigusr1}
	restart := []database.ReloadMode{database.ReloadModeRestart}
	origins := "https://example.com"

	for _, tc := range []struct {
		name     string
		previous func(t *testing.T) []byte
		modify   func(r *PostgRESTConfigResource)
		expected []database.ReloadMode
	}{
		{
			name:     "no previous config",
			previous: func(*testing.T) []byte { return nil },
			modify:   func(*PostgRESTConfigResource) {},
		},
		{
			name:   "unchanged",
			modify: func(*PostgRESTConfigResource) {},
		},
		{
			name:     "max rows changed",
			modify:   func(r *PostgRESTConfigResource) { r.Config.MaxRows = 500 },
			expected: reload,
		},
		{
			name:     "schemas changed",
			modify:   func(r *PostgRESTConfigResource) { r.Config.DBSchemas = "api" },
			expected: reload,
		},
		{
			name:     "pool changed",
			modify:   func(r *PostgRESTConfigResource) { r.Config.DBPool = 20 },
			expected: restart,
		},
		{
			name:     "password changed",
			modify:   func(r *PostgRESTConfigResource) { r.ConnectAsPassword = "new-pass" },
			expected: restart,
		},
		{
			name: "hosts changed",
			modify: func(r *PostgRESTConfigResource) {
				r.DatabaseHosts = append(r.DatabaseHosts, database.ServiceHostEntry{Host: "pg-host2", Port: 5432})
			},
			expected: restart,
		},
		{
			name:     "cors origins changed",
			modify:   func(r *PostgRESTConfigResource) { r.Config.ServerCORSAllowedOrigins = &origins },
			expected: restart,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var previous []byte
			if tc.previous != nil {
				previous = tc.previous(t)
			} else {
				previous = render(t, func(*PostgRESTConfigResource) {})
			}
			current := render(t, tc.modify)

			assert.Equal(t, tc.expected, postgrestReloadModes(previous, current))
		})
	}
}

type reloadRecorder struct {
	database.Orchestrator
	modes []database.ReloadMode
}

func (r *reloadRecorder) ReloadServiceInstance(_ context.Context, _ string, mode database.ReloadMode) error {
	r.modes = append(r.modes, mode)
	return nil
}

func postgrestConfigTestContext(t *testing.T) (*resource.Context, afero.Fs, *reloadRecorder, string) {
	t.Helper()
	const dirID, dirPath = "inst-1-data", "/etc/postgrest/inst-1"
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(dirPath, 0o700))

	orch := &reloadRecorder{}
	injector := do.New()
	do.ProvideValue[afero.Fs](injector, fs)
	do.ProvideValue[database.Orchestrator](injector, orch)
	do.ProvideValue(injector, zerolog.Nop())

	dir, err := resource.ToResourceData(&filesystem.DirResource{ID: dirID, HostID: "host-1", Path: dirPath, FullPath: dirPath})
	require.NoError(t, err)
	state := resource.NewState()
	state.Add(dir)

	return &resource.Context{State: state, Injector: injector}, fs, orch, filepath.Join(dirPath, "postgrest.conf")
}

func testPostgRESTConfig(maxRows int) *PostgRESTConfigResource {
	return &PostgRESTConfigResource{
		ServiceInstanceID: "inst-1",
		HostID:            "host-1",
		DirResourceID:     "inst-1-data",
		Config:            &database.PostgRESTServiceConfig{DBSchemas: "public", DBAnonRole: "web_anon", DBPool: 10, MaxRows: maxRows},
		ConnectAsUsername: "app",
		ConnectAsPassword: "secret",
		DatabaseName:      "db",
		DatabaseHosts:     []database.ServiceHostEntry{{Host: "10.0.0.1", Port: 5432}},
	}
}

func TestPostgRESTConfigResource_RefreshDetectsDrift(t *testing.T) {
	rc, fs, _, confPath := postgrestConfigTestContext(t)
	r := testPostgRESTConfig(100)
	require.NoError(t, r.Create(t.Context(), rc))

	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.False(t, r.ConfigDrifted, "unchanged file should not be drifted")

	require.NoError(t, afero.WriteFile(fs, confPath, []byte("db-pool = 1\n"), 0o600))
	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.True(t, r.ConfigDrifted, "edited file should be drifted")
}

func TestPostgRESTConfigResource_UpdateFrom(t *testing.T) {
	drifted := testPostgRESTConfig(100)
	drifted.ConfigDrifted = true

	for _, tc := range []struct {
		name           string
		previous       *PostgRESTConfigResource
		previousFailed bool
		expected       []database.ReloadMode
	}{
		{
			name:     "reloadable change reloads in place",
			previous: testPostgRESTConfig(100),
			expected: []database.ReloadMode{database.ReloadModeSigusr2, database.ReloadModeSigusr1},
		},
		{
			name:     "no change",
			previous: testPostgRESTConfig(200),
		},
		{
			name: "no previous state",
		},
		{
			name:           "retry after a failed update restarts",
			previous:       testPostgRESTConfig(200),
			previousFailed: true,
			expected:       []database.ReloadMode{database.ReloadModeRestart},
		},
		{
			name:     "drifted file restarts",
			previous: drifted,
			expected: []database.ReloadMode{database.ReloadModeRestart},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, orch, _ := postgrestConfigTestContext(t)
			require.NoError(t, testPostgRESTConfig(200).UpdateFrom(t.Context(), rc, tc.previous, tc.previousFailed))
			assert.Equal(t, tc.expected, orch.modes)
		})
	}
}
