package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestRAGConfigResource_ResourceVersion(t *testing.T) {
	r := &RAGConfigResource{}
	assert.Equal(t, "2", r.ResourceVersion())
}

func TestRAGConfigResource_Identifier(t *testing.T) {
	r := &RAGConfigResource{ServiceInstanceID: "storefront-rag-host1"}
	id := r.Identifier()
	assert.Equal(t, "storefront-rag-host1", id.ID)
	assert.Equal(t, ResourceTypeRAGConfig, id.Type)
}

func TestRAGConfigResourceIdentifier(t *testing.T) {
	id := RAGConfigResourceIdentifier("my-instance")
	assert.Equal(t, "my-instance", id.ID)
	assert.Equal(t, ResourceTypeRAGConfig, id.Type)
}

func TestRAGConfigResource_Executor(t *testing.T) {
	r := &RAGConfigResource{HostID: "host-1"}
	assert.Equal(t, resource.HostExecutor("host-1"), r.Executor())
}

func TestRAGConfigResource_DiffIgnore(t *testing.T) {
	r := &RAGConfigResource{}
	assert.Empty(t, r.DiffIgnore())
}

func TestRAGConfigResource_Dependencies(t *testing.T) {
	r := &RAGConfigResource{
		ServiceInstanceID: "storefront-rag-host1",
		DirResourceID:     "storefront-rag-host1-data",
	}
	deps := r.Dependencies()

	require.Len(t, deps, 3)
	assert.Equal(t, filesystem.DirResourceIdentifier("storefront-rag-host1-data"), deps[0])
	assert.Equal(t, RAGServiceKeysResourceIdentifier("storefront-rag-host1"), deps[1])
	assert.Equal(t, RAGPreflightResourceIdentifier("storefront-rag-host1"), deps[2])
}
