package zfs

import (
	"context"
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClone_Identifier(t *testing.T) {
	c := &Clone{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		HostID:           "host-a",
		Pool:             "tank",
	}
	id := c.Identifier()
	assert.Equal(t, ResourceTypeClone, id.Type)
	assert.Equal(t, "dst", id.ID)
}

func TestClone_Executor(t *testing.T) {
	c := &Clone{HostID: "host-c"}
	exec := c.Executor()
	assert.Equal(t, resource.ExecutorTypeHost, exec.Type)
	assert.Equal(t, "host-c", exec.ID)
}

func TestClone_Dependencies(t *testing.T) {
	c := &Clone{CloneInstanceID: "dst"}
	deps := c.Dependencies()
	require.Len(t, deps, 1)
	assert.Equal(t, SnapshotIdentifier("dst"), deps[0])
}

func TestClone_Refresh_Exists(t *testing.T) {
	c := &Clone{
		CloneInstanceID: "dst",
		Pool:            "tank",
		Run: func(args ...string) (string, error) {
			return "tank/instances/dst\t-\t-", nil
		},
	}
	err := c.Refresh(context.Background(), nil)
	require.NoError(t, err)
}

func TestClone_Refresh_NotFound(t *testing.T) {
	c := &Clone{
		CloneInstanceID: "dst",
		Pool:            "tank",
		Run: func(args ...string) (string, error) {
			return "", fmt.Errorf("zfs list: dataset does not exist: %w", fmt.Errorf("exit status 1"))
		},
	}
	err := c.Refresh(context.Background(), nil)
	assert.ErrorIs(t, err, resource.ErrNotFound)
}

func TestClone_Create(t *testing.T) {
	var capturedArgs []string
	c := &Clone{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		Pool:             "tank",
		MountPoint:       "/data/dst",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := c.Create(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"clone", "-o", "mountpoint=/data/dst", "tank/instances/src@clone-dst", "tank/instances/dst"}, capturedArgs)
}

func TestClone_Delete(t *testing.T) {
	var capturedArgs []string
	c := &Clone{
		CloneInstanceID: "dst",
		Pool:            "tank",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := c.Delete(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"destroy", "tank/instances/dst"}, capturedArgs)
}

func TestClone_ResourceVersion(t *testing.T) {
	c := &Clone{}
	assert.Equal(t, "1", c.ResourceVersion())
}

func TestClone_TypeDependencies(t *testing.T) {
	c := &Clone{}
	assert.Nil(t, c.TypeDependencies())
}
