package common

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/samber/do"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const testRAGConfigDir = "/etc/pgedge-rag/inst-1"

var testRAGKeysDir = filepath.Join(testRAGConfigDir, "keys")

// ragTestContext returns a resource context backed by the given filesystem,
// with the RAG config directory resource in state.
func ragTestContext(t *testing.T, afs afero.Fs) *resource.Context {
	t.Helper()
	require.NoError(t, afs.MkdirAll(testRAGConfigDir, 0o700))

	injector := do.New()
	do.ProvideValue(injector, afs)

	state := resource.NewState()
	dir, err := resource.ToResourceData(&filesystem.DirResource{
		ID:       "inst-1-config",
		HostID:   "host-1",
		Path:     testRAGConfigDir,
		FullPath: testRAGConfigDir,
	})
	require.NoError(t, err)
	state.Add(dir)

	return &resource.Context{State: state, Injector: injector}
}

func testRAGKeysResource(keys map[string]string) *RAGServiceKeysResource {
	return &RAGServiceKeysResource{
		ServiceInstanceID: "inst-1",
		HostID:            "host-1",
		ParentID:          "inst-1-config",
		Keys:              keys,
		OwnerUID:          999,
		OwnerGID:          998,
	}
}

func TestRAGServiceKeysResource_Metadata(t *testing.T) {
	r := testRAGKeysResource(nil)
	assert.Equal(t, resource.Identifier{ID: "inst-1", Type: "common.rag_service_keys"}, r.Identifier())
	assert.Equal(t, resource.HostExecutor("host-1"), r.Executor())
	assert.Equal(t, []resource.Identifier{filesystem.DirResourceIdentifier("inst-1-config")}, r.Dependencies())
}

func TestRAGServiceKeysResource_CreateRefreshDelete(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	r := testRAGKeysResource(map[string]string{
		"default_embedding.key": "sk-embed",
		"default_rag.key":       "sk-ant",
	})

	// The keys directory doesn't exist yet.
	assert.ErrorIs(t, r.Refresh(t.Context(), rc), resource.ErrNotFound)

	require.NoError(t, r.Create(t.Context(), rc))

	info, err := afs.Stat(testRAGKeysDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, 0o700, int(info.Mode().Perm()))
	for name, value := range r.Keys {
		path := filepath.Join(testRAGKeysDir, name)
		content, err := afero.ReadFile(afs, path)
		require.NoError(t, err)
		assert.Equal(t, value, string(content))
		info, err := afs.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, 0o600, int(info.Mode().Perm()))
		exists, err := afero.Exists(afs, path+".tmp")
		require.NoError(t, err)
		assert.False(t, exists)
	}
	require.NoError(t, r.Refresh(t.Context(), rc))

	// A missing key file makes the resource be recreated.
	require.NoError(t, afs.Remove(filepath.Join(testRAGKeysDir, "default_rag.key")))
	assert.ErrorIs(t, r.Refresh(t.Context(), rc), resource.ErrNotFound)

	require.NoError(t, r.Delete(t.Context(), rc))
	exists, err := afero.Exists(afs, testRAGKeysDir)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRAGServiceKeysResource_UpdateRemovesStaleKeys(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	require.NoError(t, testRAGKeysResource(map[string]string{
		"old_embedding.key": "sk-old",
		"default_rag.key":   "sk-ant",
	}).Create(t.Context(), rc))

	updated := testRAGKeysResource(map[string]string{
		"default_rag.key": "sk-ant-rotated",
	})
	require.NoError(t, updated.Update(t.Context(), rc))

	exists, err := afero.Exists(afs, filepath.Join(testRAGKeysDir, "old_embedding.key"))
	require.NoError(t, err)
	assert.False(t, exists, "the removed pipeline's key file should be deleted")
	content, err := afero.ReadFile(afs, filepath.Join(testRAGKeysDir, "default_rag.key"))
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-rotated", string(content))
}

func TestRAGServiceKeysResource_RefreshDetectsChangedKeys(t *testing.T) {
	path := filepath.Join(testRAGKeysDir, "default_rag.key")
	for name, tamper := range map[string]func(afero.Fs) error{
		"changed": func(afs afero.Fs) error {
			return afero.WriteFile(afs, path, []byte("sk-other"), 0o600)
		},
		"truncated": func(afs afero.Fs) error {
			return afero.WriteFile(afs, path, nil, 0o600)
		},
		"replaced by a directory": func(afs afero.Fs) error {
			if err := afs.Remove(path); err != nil {
				return err
			}
			return afs.Mkdir(path, 0o700)
		},
	} {
		t.Run(name, func(t *testing.T) {
			afs := afero.NewMemMapFs()
			rc := ragTestContext(t, afs)
			r := testRAGKeysResource(map[string]string{"default_rag.key": "sk-ant"})
			require.NoError(t, r.Create(t.Context(), rc))
			require.NoError(t, r.Refresh(t.Context(), rc))

			require.NoError(t, tamper(afs))
			assert.ErrorIs(t, r.Refresh(t.Context(), rc), resource.ErrNotFound)
		})
	}
}

func TestRAGServiceKeysResource_CreateRemovesStaleKeys(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	require.NoError(t, testRAGKeysResource(map[string]string{
		"old_embedding.key": "sk-old",
		"keep_rag.key":      "sk-ant",
	}).Create(t.Context(), rc))

	// A missing key file makes the resource be recreated rather than
	// updated, and the recreation must still remove the old pipeline's key.
	require.NoError(t, afs.Remove(filepath.Join(testRAGKeysDir, "keep_rag.key")))
	recreated := testRAGKeysResource(map[string]string{"keep_rag.key": "sk-ant"})
	require.ErrorIs(t, recreated.Refresh(t.Context(), rc), resource.ErrNotFound)
	require.NoError(t, recreated.Create(t.Context(), rc))

	exists, err := afero.Exists(afs, filepath.Join(testRAGKeysDir, "old_embedding.key"))
	require.NoError(t, err)
	assert.False(t, exists, "the removed pipeline's key file should be deleted")
	require.NoError(t, recreated.Refresh(t.Context(), rc))
}

func TestRAGServiceKeysResource_FailedUpdateKeepsCurrentKeys(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	require.NoError(t, testRAGKeysResource(map[string]string{"old_rag.key": "sk-old"}).Create(t.Context(), rc))

	// The new key can't be written, so the key that the installed config
	// references must not be removed.
	do.OverrideValue[afero.Fs](rc.Injector, renameFailingFs{Fs: afs})
	err := testRAGKeysResource(map[string]string{"new_rag.key": "sk-new"}).Update(t.Context(), rc)
	require.ErrorIs(t, err, syscall.EPERM)

	content, err := afero.ReadFile(afs, filepath.Join(testRAGKeysDir, "old_rag.key"))
	require.NoError(t, err)
	assert.Equal(t, "sk-old", string(content))
}

// renameFailingFs fails every rename, to simulate a key file that can't be
// moved into place.
type renameFailingFs struct {
	afero.Fs
}

func (renameFailingFs) Rename(string, string) error {
	return syscall.EPERM
}

func TestRAGServiceKeysResource_InvalidFilenameIsNonDestructive(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	require.NoError(t, testRAGKeysResource(map[string]string{"default_rag.key": "sk-ant"}).Create(t.Context(), rc))

	err := testRAGKeysResource(map[string]string{"../escape.key": "x"}).Update(t.Context(), rc)
	assert.ErrorContains(t, err, `invalid key filename "../escape.key"`)

	exists, err := afero.Exists(afs, filepath.Join(testRAGKeysDir, "default_rag.key"))
	require.NoError(t, err)
	assert.True(t, exists, "existing keys must survive a rejected update")

	err = testRAGKeysResource(map[string]string{"../escape.key": "x"}).Refresh(t.Context(), rc)
	assert.ErrorContains(t, err, "invalid key filename in state")
}

func TestRAGServiceKeysResource_FailedWriteLeavesNoPartialFile(t *testing.T) {
	afs := afero.NewMemMapFs()
	rc := ragTestContext(t, afs)
	require.NoError(t, afs.MkdirAll(testRAGKeysDir, 0o700))
	r := testRAGKeysResource(map[string]string{"default_rag.key": "sk-ant"})

	err := r.writeKeyFiles(chownFailingFs{Fs: afs}, testRAGKeysDir)
	require.ErrorIs(t, err, syscall.EPERM)
	path := filepath.Join(testRAGKeysDir, "default_rag.key")
	for _, p := range []string{path, path + ".tmp"} {
		exists, err := afero.Exists(afs, p)
		require.NoError(t, err)
		assert.False(t, exists, "%s should not exist after a failed write", p)
	}

	require.NoError(t, r.Create(t.Context(), rc))
	content, err := afero.ReadFile(afs, path)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant", string(content))
}

func TestValidateKeyFilename(t *testing.T) {
	for _, name := range []string{"default_rag.key", "a-b_c.key"} {
		assert.NoError(t, validateKeyFilename(name), name)
	}
	for _, name := range []string{".", "..", "../x.key", "/abs.key", `a\b.key`, "a/b.key", "./x.key"} {
		assert.Error(t, validateKeyFilename(name), name)
	}
}

func TestExtractRAGAPIKeys(t *testing.T) {
	pipeline := func(name string, embedding, rag database.RAGPipelineLLMConfig, rerank *database.RAGRerankConfig) database.RAGPipeline {
		return database.RAGPipeline{Name: name, EmbeddingLLM: embedding, RAGLLM: rag, Rerank: rerank}
	}
	llm := func(provider string, key *string) database.RAGPipelineLLMConfig {
		return database.RAGPipelineLLMConfig{Provider: provider, Model: "m", APIKey: key}
	}

	for _, tc := range []struct {
		name     string
		config   *database.RAGServiceConfig
		expected map[string]string
	}{
		{
			name: "embedding and rag keys",
			config: &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{
				pipeline("default", llm("openai", utils.PointerTo("sk-embed")), llm("anthropic", utils.PointerTo("sk-ant")), nil),
			}},
			expected: map[string]string{"default_embedding.key": "sk-embed", "default_rag.key": "sk-ant"},
		},
		{
			name: "ollama needs no keys",
			config: &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{
				pipeline("local", llm("ollama", nil), llm("ollama", nil), nil),
			}},
			expected: map[string]string{},
		},
		{
			name: "multiple pipelines",
			config: &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{
				pipeline("a", llm("openai", utils.PointerTo("sk-a")), llm("ollama", nil), nil),
				pipeline("b", llm("ollama", nil), llm("anthropic", utils.PointerTo("sk-b")), nil),
			}},
			expected: map[string]string{"a_embedding.key": "sk-a", "b_rag.key": "sk-b"},
		},
		{
			name: "rerank gets its own key file",
			config: &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{
				pipeline("default", llm("openai", utils.PointerTo("sk-embed")), llm("anthropic", utils.PointerTo("sk-ant")),
					&database.RAGRerankConfig{Provider: "voyage", Model: "rerank-2", APIKey: utils.PointerTo("sk-voyage")}),
			}},
			expected: map[string]string{"default_embedding.key": "sk-embed", "default_rag.key": "sk-ant", "default_rerank.key": "sk-voyage"},
		},
		{
			name: "rerank reuses the embedding voyage key",
			config: &database.RAGServiceConfig{Pipelines: []database.RAGPipeline{
				pipeline("default", llm("voyage", utils.PointerTo("sk-voyage")), llm("anthropic", utils.PointerTo("sk-ant")),
					&database.RAGRerankConfig{Provider: "voyage", Model: "rerank-2", APIKey: utils.PointerTo("sk-voyage")}),
			}},
			expected: map[string]string{"default_embedding.key": "sk-voyage", "default_rag.key": "sk-ant"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ExtractRAGAPIKeys(tc.config))
		})
	}
}

func TestRAGPreflightResource_Dependencies(t *testing.T) {
	r := &RAGPreflightResource{
		ServiceInstanceID: "inst-1",
		NodeName:          "n1",
		DatabaseName:      "storefront",
	}
	assert.Equal(t, resource.Identifier{ID: "inst-1", Type: "common.rag_preflight"}, r.Identifier())
	assert.Equal(t, resource.PrimaryExecutor("n1"), r.Executor())
	assert.Equal(t, []resource.Identifier{
		database.NodeResourceIdentifier("n1"),
		database.PostgresDatabaseResourceIdentifier("n1", "storefront"),
	}, r.Dependencies())
}
