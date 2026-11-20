package common

import (
	"flag"
	"path/filepath"
	"syscall"
	"testing"

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

var testRAGConfigPath = filepath.Join(testRAGConfigDir, "pgedge-rag-server.yaml")

func testRAGServiceConfig() *database.RAGServiceConfig {
	return &database.RAGServiceConfig{
		Pipelines: []database.RAGPipeline{
			{
				Name: "default",
				Tables: []database.RAGPipelineTable{
					{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
				},
				EmbeddingLLM: database.RAGPipelineLLMConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
					APIKey:   utils.PointerTo("sk-embed"),
				},
				RAGLLM: database.RAGPipelineLLMConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-5",
					APIKey:   utils.PointerTo("sk-ant"),
				},
			},
		},
	}
}

func testRAGConfigResource(cfg *database.RAGServiceConfig) *RAGConfigResource {
	return &RAGConfigResource{
		ServiceInstanceID: "inst-1",
		ServiceID:         "rag",
		HostID:            "host-1",
		DirResourceID:     "inst-1-config",
		Config:            cfg,
		DatabaseName:      "storefront",
		DatabaseHost:      "10.0.0.1",
		DatabasePort:      6432,
		ConnectAsUsername: "rag_user",
		ConnectAsPassword: "secret",
		KeysDir:           testRAGKeysDir,
		ListenAddress:     "0.0.0.0",
		Port:              3300,
		OwnerUID:          999,
		OwnerGID:          998,
	}
}

func ragConfigTestContext(t *testing.T) (*resource.Context, afero.Fs, *reloadRecorder) {
	t.Helper()
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	orch := &reloadRecorder{}
	do.ProvideValue[database.Orchestrator](rc.Injector, orch)
	do.ProvideValue(rc.Injector, zerolog.Nop())
	return rc, afs, orch
}

func TestRAGConfigResource_Metadata(t *testing.T) {
	r := testRAGConfigResource(testRAGServiceConfig())
	assert.Equal(t, resource.Identifier{ID: "inst-1", Type: "common.rag_config"}, r.Identifier())
	assert.Equal(t, resource.HostExecutor("host-1"), r.Executor())
	assert.Equal(t, []resource.Identifier{
		filesystem.DirResourceIdentifier("inst-1-config"),
		RAGServiceKeysResourceIdentifier("inst-1"),
		RAGPreflightResourceIdentifier("inst-1"),
	}, r.Dependencies())
}

func TestRAGConfigResource_Create(t *testing.T) {
	rc, afs, orch := ragConfigTestContext(t)
	r := testRAGConfigResource(testRAGServiceConfig())

	assert.ErrorIs(t, r.Refresh(t.Context(), rc), resource.ErrNotFound)
	require.NoError(t, r.Create(t.Context(), rc))

	content, err := afero.ReadFile(afs, testRAGConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "port: 3300")
	assert.Contains(t, string(content), "host: 10.0.0.1")
	assert.Contains(t, string(content), "openai: "+testRAGKeysDir+"/default_embedding.key")
	assert.NotContains(t, string(content), "sk-embed", "key values must stay out of the config file")
	info, err := afs.Stat(testRAGConfigPath)
	require.NoError(t, err)
	assert.Equal(t, 0o600, int(info.Mode().Perm()))
	exists, err := afero.Exists(afs, testRAGConfigPath+".tmp")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, orch.modes, "creating the config doesn't restart anything")

	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.False(t, r.ConfigDrifted)
}

func TestRAGConfigResource_RefreshDetectsDrift(t *testing.T) {
	rc, afs, _ := ragConfigTestContext(t)
	r := testRAGConfigResource(testRAGServiceConfig())
	require.NoError(t, r.Create(t.Context(), rc))

	require.NoError(t, afero.WriteFile(afs, testRAGConfigPath, []byte("edited by hand\n"), 0o600))
	require.NoError(t, r.Refresh(t.Context(), rc))
	assert.True(t, r.ConfigDrifted)
}

func TestRAGConfigResource_UpdateFrom(t *testing.T) {
	withKey := func(key string) *database.RAGServiceConfig {
		cfg := testRAGServiceConfig()
		cfg.Pipelines[0].RAGLLM.APIKey = utils.PointerTo(key)
		return cfg
	}

	for _, tc := range []struct {
		name           string
		previous       func() *RAGConfigResource
		previousFailed bool
		current        func() *RAGConfigResource
		restart        bool
	}{
		{
			name:     "no previous state",
			previous: func() *RAGConfigResource { return nil },
			current:  func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
		},
		{
			name:     "unchanged",
			previous: func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			current:  func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
		},
		{
			name:     "pipeline change",
			previous: func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			current: func() *RAGConfigResource {
				cfg := testRAGServiceConfig()
				cfg.Pipelines[0].TopN = utils.PointerTo(5)
				return testRAGConfigResource(cfg)
			},
			restart: true,
		},
		{
			name:     "key rotation only",
			previous: func() *RAGConfigResource { return testRAGConfigResource(withKey("sk-ant")) },
			current:  func() *RAGConfigResource { return testRAGConfigResource(withKey("sk-ant-rotated")) },
			restart:  true,
		},
		{
			name:     "database host change",
			previous: func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			current: func() *RAGConfigResource {
				r := testRAGConfigResource(testRAGServiceConfig())
				r.DatabasePort = 6433
				return r
			},
			restart: true,
		},
		{
			name:     "port change",
			previous: func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			current: func() *RAGConfigResource {
				r := testRAGConfigResource(testRAGServiceConfig())
				r.Port = 3301
				return r
			},
			restart: true,
		},
		{
			name:           "previous update failed",
			previous:       func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			previousFailed: true,
			current:        func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			restart:        true,
		},
		{
			name: "previous file drifted",
			previous: func() *RAGConfigResource {
				r := testRAGConfigResource(testRAGServiceConfig())
				r.ConfigDrifted = true
				return r
			},
			current: func() *RAGConfigResource { return testRAGConfigResource(testRAGServiceConfig()) },
			restart: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, afs, orch := ragConfigTestContext(t)
			current := tc.current()

			require.NoError(t, current.UpdateFrom(t.Context(), rc, tc.previous(), tc.previousFailed))

			if tc.restart {
				assert.Equal(t, []database.ReloadMode{database.ReloadModeRestart}, orch.modes)
			} else {
				assert.Empty(t, orch.modes)
			}
			expected, err := current.generateConfig()
			require.NoError(t, err)
			content, err := afero.ReadFile(afs, testRAGConfigPath)
			require.NoError(t, err)
			assert.Equal(t, string(expected), string(content))
		})
	}
}

func TestRAGConfigResource_UpdateUsesStoredState(t *testing.T) {
	rc, _, orch := ragConfigTestContext(t)
	previous := testRAGConfigResource(testRAGServiceConfig())
	data, err := resource.ToResourceData(previous)
	require.NoError(t, err)
	rc.State.Add(data)

	cfg := testRAGServiceConfig()
	cfg.Pipelines[0].TopN = utils.PointerTo(5)
	require.NoError(t, testRAGConfigResource(cfg).Update(t.Context(), rc))
	assert.Equal(t, []database.ReloadMode{database.ReloadModeRestart}, orch.modes)
}

func TestRAGConfigResource_FailedWriteLeavesNoPartialFile(t *testing.T) {
	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll(testRAGConfigDir, 0o700))
	r := testRAGConfigResource(testRAGServiceConfig())

	err := r.writeFile(chownFailingFs{Fs: afs}, testRAGConfigPath, []byte("x"))
	require.ErrorIs(t, err, syscall.EPERM)
	for _, p := range []string{testRAGConfigPath, testRAGConfigPath + ".tmp"} {
		exists, err := afero.Exists(afs, p)
		require.NoError(t, err)
		assert.False(t, exists, "%s should not exist after a failed write", p)
	}
}

// TestRAGConfigGolden pins the config file that systemd hosts get. The swarm
// output is pinned by the equivalence test in the swarm package.
func TestRAGConfigGolden(t *testing.T) {
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
		config func() *database.RAGServiceConfig
	}{
		{
			name:   "systemd minimal",
			config: testRAGServiceConfig,
		},
		{
			name: "systemd rerank and defaults",
			config: func() *database.RAGServiceConfig {
				cfg := testRAGServiceConfig()
				cfg.Pipelines[0].Rerank = &database.RAGRerankConfig{
					Provider: "voyage",
					Model:    "rerank-2",
					APIKey:   utils.PointerTo("sk-voyage"),
				}
				cfg.Defaults = &database.RAGDefaults{TopN: utils.PointerTo(10)}
				return cfg
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := testRAGConfigResource(tc.config()).generateConfig()
			require.NoError(t, err)
			golden.Run(t, string(content), update)
		})
	}
}
