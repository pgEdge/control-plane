package zfs

import (
	"context"
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataset_Identifier(t *testing.T) {
	d := &Dataset{ID: "inst-1", HostID: "host-a", Pool: "tank"}
	id := d.Identifier()
	assert.Equal(t, ResourceTypeDataset, id.Type)
	assert.Equal(t, "inst-1", id.ID)
}

func TestDataset_Executor(t *testing.T) {
	d := &Dataset{ID: "inst-1", HostID: "host-a", Pool: "tank"}
	exec := d.Executor()
	assert.Equal(t, resource.ExecutorTypeHost, exec.Type)
	assert.Equal(t, "host-a", exec.ID)
}

func TestDataset_Dependencies_NoParent(t *testing.T) {
	d := &Dataset{ID: "inst-1", HostID: "host-a", Pool: "tank"}
	assert.Nil(t, d.Dependencies())
}

func TestDataset_Dependencies_WithParent(t *testing.T) {
	d := &Dataset{ID: "inst-1", HostID: "host-a", Pool: "tank", ParentID: "dir-root"}
	deps := d.Dependencies()
	require.Len(t, deps, 1)
	assert.Equal(t, filesystem.DirResourceIdentifier("dir-root"), deps[0])
}

func TestDataset_Refresh_Exists(t *testing.T) {
	d := &Dataset{
		ID:   "inst-1",
		Pool: "tank",
		Run: func(args ...string) (string, error) {
			return "tank/instances/inst-1\t-\t-", nil
		},
	}
	err := d.Refresh(context.Background(), nil)
	require.NoError(t, err)
}

func TestDataset_Refresh_NotFound(t *testing.T) {
	d := &Dataset{
		ID:   "inst-1",
		Pool: "tank",
		Run: func(args ...string) (string, error) {
			return "", fmt.Errorf("zfs list: dataset does not exist: %w", fmt.Errorf("exit status 1"))
		},
	}
	err := d.Refresh(context.Background(), nil)
	assert.ErrorIs(t, err, resource.ErrNotFound)
}

func TestDataset_Create(t *testing.T) {
	var capturedArgs []string
	d := &Dataset{
		ID:         "inst-1",
		Pool:       "tank",
		MountPoint: "/data/inst-1",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := d.Create(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"create", "-o", "mountpoint=/data/inst-1", "tank/instances/inst-1"}, capturedArgs)
}

func TestDataset_Delete(t *testing.T) {
	var capturedArgs []string
	d := &Dataset{
		ID:   "inst-1",
		Pool: "tank",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := d.Delete(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"destroy", "tank/instances/inst-1"}, capturedArgs)
}

func TestDataset_ResourceVersion(t *testing.T) {
	d := &Dataset{}
	assert.Equal(t, "1", d.ResourceVersion())
}

func TestDataset_TypeDependencies(t *testing.T) {
	d := &Dataset{}
	assert.Nil(t, d.TypeDependencies())
}
