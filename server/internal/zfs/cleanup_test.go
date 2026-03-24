package zfs

import (
	"context"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpockCleanup_Identifier(t *testing.T) {
	c := &SpockCleanup{CloneInstanceID: "dst", HostID: "host-a", ServiceID: "svc-1"}
	id := c.Identifier()
	assert.Equal(t, ResourceTypeCleanup, id.Type)
	assert.Equal(t, "dst", id.ID)
}

func TestSpockCleanup_Executor(t *testing.T) {
	c := &SpockCleanup{HostID: "host-e"}
	exec := c.Executor()
	assert.Equal(t, resource.ExecutorTypeHost, exec.Type)
	assert.Equal(t, "host-e", exec.ID)
}

func TestSpockCleanup_Dependencies(t *testing.T) {
	c := &SpockCleanup{CloneInstanceID: "dst", ServiceID: "svc-42"}
	deps := c.Dependencies()
	require.Len(t, deps, 1)
	assert.Equal(t, resource.Type("swarm.postgres_service"), deps[0].Type)
	assert.Equal(t, "svc-42", deps[0].ID)
}

func TestSpockCleanup_TypeDependencies(t *testing.T) {
	c := &SpockCleanup{}
	assert.Nil(t, c.TypeDependencies())
}

func TestSpockCleanup_ResourceVersion(t *testing.T) {
	c := &SpockCleanup{}
	assert.Equal(t, "1", c.ResourceVersion())
}

func TestSpockCleanup_Refresh_Stub(t *testing.T) {
	c := &SpockCleanup{CloneInstanceID: "dst"}
	err := c.Refresh(context.Background(), nil)
	require.NoError(t, err)
}

func TestSpockCleanup_Create_Stub(t *testing.T) {
	c := &SpockCleanup{CloneInstanceID: "dst"}
	err := c.Create(context.Background(), nil)
	require.NoError(t, err)
}

func TestSpockCleanup_Delete_NoOp(t *testing.T) {
	c := &SpockCleanup{CloneInstanceID: "dst"}
	err := c.Delete(context.Background(), nil)
	require.NoError(t, err)
}
