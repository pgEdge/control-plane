package zfs

import (
	"context"
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_Identifier(t *testing.T) {
	s := &Snapshot{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		HostID:           "host-a",
		Pool:             "tank",
	}
	id := s.Identifier()
	assert.Equal(t, ResourceTypeSnapshot, id.Type)
	assert.Equal(t, "dst", id.ID)
}

func TestSnapshot_Executor(t *testing.T) {
	s := &Snapshot{HostID: "host-b"}
	exec := s.Executor()
	assert.Equal(t, resource.ExecutorTypeHost, exec.Type)
	assert.Equal(t, "host-b", exec.ID)
}

func TestSnapshot_Dependencies(t *testing.T) {
	s := &Snapshot{CloneInstanceID: "dst"}
	assert.Nil(t, s.Dependencies())
}

func TestSnapshot_Refresh_Exists(t *testing.T) {
	s := &Snapshot{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		Pool:             "tank",
		Run: func(args ...string) (string, error) {
			return "tank/instances/src@clone-dst\t-\t-", nil
		},
	}
	err := s.Refresh(context.Background(), nil)
	require.NoError(t, err)
}

func TestSnapshot_Refresh_NotFound(t *testing.T) {
	s := &Snapshot{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		Pool:             "tank",
		Run: func(args ...string) (string, error) {
			return "", fmt.Errorf("zfs list: dataset does not exist: %w", fmt.Errorf("exit status 1"))
		},
	}
	err := s.Refresh(context.Background(), nil)
	assert.ErrorIs(t, err, resource.ErrNotFound)
}

func TestSnapshot_Create(t *testing.T) {
	var capturedArgs []string
	s := &Snapshot{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		Pool:             "tank",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := s.Create(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"snapshot", "tank/instances/src@clone-dst"}, capturedArgs)
}

func TestSnapshot_Delete(t *testing.T) {
	var capturedArgs []string
	s := &Snapshot{
		SourceInstanceID: "src",
		CloneInstanceID:  "dst",
		Pool:             "tank",
		Run: func(args ...string) (string, error) {
			capturedArgs = args
			return "", nil
		},
	}
	err := s.Delete(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"destroy", "tank/instances/src@clone-dst"}, capturedArgs)
}

func TestSnapshot_ResourceVersion(t *testing.T) {
	s := &Snapshot{}
	assert.Equal(t, "1", s.ResourceVersion())
}

func TestSnapshot_TypeDependencies(t *testing.T) {
	s := &Snapshot{}
	assert.Nil(t, s.TypeDependencies())
}
