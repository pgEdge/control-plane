package swarm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/samber/do"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// mcpRCAndFs returns a resource.Context backed by an in-memory afero.Fs
// with the given data directory pre-created, and the Fs itself for file setup.
func mcpRCAndFs(t *testing.T, dirResourceID, dirPath string) (*resource.Context, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(dirPath, 0o700))

	injector := do.New()
	do.Provide(injector, func(i *do.Injector) (afero.Fs, error) {
		return fs, nil
	})

	dirRes := &filesystem.DirResource{
		ID:       dirResourceID,
		HostID:   "host-1",
		Path:     dirPath,
		FullPath: dirPath,
	}
	data, err := resource.ToResourceData(dirRes)
	if err != nil {
		t.Fatalf("ToResourceData() error = %v", err)
	}
	state := resource.NewState()
	state.Add(data)
	return &resource.Context{State: state, Injector: injector}, fs
}

func TestMCPConfigResource_ResourceVersion(t *testing.T) {
	r := &MCPConfigResource{}
	assert.Equal(t, "3", r.ResourceVersion())
}

func TestMCPConfigResource_Identifier(t *testing.T) {
	r := &MCPConfigResource{ServiceInstanceID: "db1-mcp-host1"}
	id := r.Identifier()
	assert.Equal(t, "db1-mcp-host1", id.ID)
	assert.Equal(t, ResourceTypeMCPConfig, id.Type)
}

func TestMCPConfigResource_Executor(t *testing.T) {
	r := &MCPConfigResource{HostID: "host-1"}
	assert.Equal(t, resource.HostExecutor("host-1"), r.Executor())
}

func TestMCPConfigResource_DiffIgnore(t *testing.T) {
	r := &MCPConfigResource{}
	assert.Nil(t, r.DiffIgnore())
}

func TestMCPConfigResource_Dependencies(t *testing.T) {
	r := &MCPConfigResource{
		ServiceInstanceID: "db1-mcp-host1",
		DirResourceID:     "db1-mcp-host1-data",
	}
	deps := r.Dependencies()
	require.Len(t, deps, 1)
	assert.Equal(t, filesystem.DirResourceIdentifier("db1-mcp-host1-data"), deps[0])
}

// The KB file existence check lives in Create and Update, NOT Refresh.
// Refresh is only invoked by the planner for resources already in state —
// it is never called for the initial deployment of a new service instance.
// Putting the KB check in Refresh would silently skip it on first deploy.

func TestMCPConfigResource_Create_KBDisabled(t *testing.T) {
	// KBHostPath empty (KB not enabled) — Create must not block on any KB check.
	dirID := "inst-data"
	dirPath := "/var/lib/pgedge/services/inst-1"
	rc, _ := mcpRCAndFs(t, dirID, dirPath)

	r := &MCPConfigResource{
		ServiceInstanceID: "inst-1",
		HostID:            "host-1",
		DirResourceID:     dirID,
		Config:            &database.MCPServiceConfig{},
		KBHostPath:        "", // not set
	}
	err := r.Create(context.Background(), rc)
	require.NoError(t, err)
}

func TestMCPConfigResource_Create_KBFilePresent(t *testing.T) {
	// KBHostPath set and file exists → Create succeeds.
	dirID := "inst-data"
	dirPath := "/var/lib/pgedge/services/inst-kb"
	kbPath := "/var/lib/pgedge/kb/nla-kb.db"
	rc, fs := mcpRCAndFs(t, dirID, dirPath)

	require.NoError(t, fs.MkdirAll("/var/lib/pgedge/kb", 0o700))
	require.NoError(t, afero.WriteFile(fs, kbPath, []byte("SQLite"), 0o600))

	r := &MCPConfigResource{
		ServiceInstanceID: "inst-kb",
		HostID:            "host-1",
		DirResourceID:     dirID,
		Config:            &database.MCPServiceConfig{},
		KBHostPath:        kbPath,
	}
	err := r.Create(context.Background(), rc)
	require.NoError(t, err)
}

func TestMCPConfigResource_Create_KBFileMissing(t *testing.T) {
	// KBHostPath set but file does not exist → plain error, NOT ErrNotFound.
	// Covers the initial-deploy gap that the Refresh-based check used to miss.
	dirID := "inst-data"
	dirPath := "/var/lib/pgedge/services/inst-kb-missing-new"
	kbPath := "/var/lib/pgedge/kb/nla-kb.db"
	rc, _ := mcpRCAndFs(t, dirID, dirPath)
	// Deliberately do NOT create the KB file.

	r := &MCPConfigResource{
		ServiceInstanceID: "inst-kb-missing-new",
		HostID:            "host-1",
		DirResourceID:     dirID,
		Config:            &database.MCPServiceConfig{},
		KBHostPath:        kbPath,
	}
	err := r.Create(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kbPath)
	// Must NOT be ErrNotFound — a missing KB file blocks deployment, not triggers Create.
	assert.False(t, errors.Is(err, resource.ErrNotFound), "missing KB file must not return ErrNotFound")
}

func TestMCPConfigResource_Update_KBFileMissing(t *testing.T) {
	// KBHostPath set but file does not exist on the update path → blocked.
	dirID := "inst-data"
	dirPath := "/var/lib/pgedge/services/inst-kb-missing"
	kbPath := "/var/lib/pgedge/kb/nla-kb.db"
	rc, fs := mcpRCAndFs(t, dirID, dirPath)

	// config.yaml present (update path) but KB file not staged.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dirPath, "config.yaml"), []byte("x: 1"), 0o600))

	r := &MCPConfigResource{
		ServiceInstanceID: "inst-kb-missing",
		HostID:            "host-1",
		DirResourceID:     dirID,
		Config:            &database.MCPServiceConfig{},
		KBHostPath:        kbPath,
	}
	err := r.Update(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kbPath)
	assert.False(t, errors.Is(err, resource.ErrNotFound), "missing KB file must not return ErrNotFound")
}
