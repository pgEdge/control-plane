package zfs

import (
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasetName(t *testing.T) {
	assert.Equal(t, "tank/instances/abc123", DatasetName("tank", "abc123"))
}

func TestSnapshotName(t *testing.T) {
	assert.Equal(t, "tank/instances/src@clone-dst", SnapshotName("tank", "src", "dst"))
}

func TestDatasetIdentifier(t *testing.T) {
	id := DatasetIdentifier("myinstance")
	assert.Equal(t, ResourceTypeDataset, id.Type)
	assert.Equal(t, "myinstance", id.ID)
}

func TestSnapshotIdentifier(t *testing.T) {
	id := SnapshotIdentifier("cloneinstance")
	assert.Equal(t, ResourceTypeSnapshot, id.Type)
	assert.Equal(t, "cloneinstance", id.ID)
}

func TestCloneIdentifier(t *testing.T) {
	id := CloneIdentifier("cloneinstance")
	assert.Equal(t, ResourceTypeClone, id.Type)
	assert.Equal(t, "cloneinstance", id.ID)
}

func TestScrubIdentifier(t *testing.T) {
	id := ScrubIdentifier("cloneinstance")
	assert.Equal(t, ResourceTypeScrub, id.Type)
	assert.Equal(t, "cloneinstance", id.ID)
}

func TestCleanupIdentifier(t *testing.T) {
	id := CleanupIdentifier("cloneinstance")
	assert.Equal(t, ResourceTypeCleanup, id.Type)
	assert.Equal(t, "cloneinstance", id.ID)
}

func TestRegisterResourceTypes(t *testing.T) {
	registry := resource.NewRegistry()
	RegisterResourceTypes(registry)

	for _, typ := range []resource.Type{
		ResourceTypeDataset,
		ResourceTypeSnapshot,
		ResourceTypeClone,
		ResourceTypeScrub,
		ResourceTypeCleanup,
	} {
		data := &resource.ResourceData{
			Identifier: resource.Identifier{Type: typ, ID: "test"},
			Attributes: []byte("{}"),
		}
		r, err := registry.Resource(data)
		require.NoError(t, err, "resource type %s should be registered", typ)
		assert.NotNil(t, r)
	}
}

func TestDatasetExists_Found(t *testing.T) {
	run := func(args ...string) (string, error) {
		return "tank/instances/abc\t-\t-", nil
	}
	exists, err := datasetExists(run, "tank/instances/abc")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestDatasetExists_NotFound(t *testing.T) {
	run := func(args ...string) (string, error) {
		return "", fmt.Errorf("zfs list -t all -H tank/instances/abc: cannot open 'tank/instances/abc': dataset does not exist: %w", fmt.Errorf("exit status 1"))
	}
	exists, err := datasetExists(run, "tank/instances/abc")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDatasetExists_Error(t *testing.T) {
	run := func(args ...string) (string, error) {
		return "", fmt.Errorf("zfs list -t all -H tank/instances/abc: some other error: %w", fmt.Errorf("exit status 1"))
	}
	exists, err := datasetExists(run, "tank/instances/abc")
	require.Error(t, err)
	assert.False(t, exists)
}
