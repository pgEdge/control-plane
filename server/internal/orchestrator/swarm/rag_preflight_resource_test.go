package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
)

func TestRAGPreflightResource_Dependencies(t *testing.T) {
	r := &RAGPreflightResource{
		ServiceInstanceID: "storefront-rag-host1",
		NodeName:          "n1",
		DatabaseName:      "storefront",
	}
	deps := r.Dependencies()

	require.Len(t, deps, 2)
	assert.Equal(t, database.NodeResourceIdentifier("n1"), deps[0])
	assert.Equal(t, database.PostgresDatabaseResourceIdentifier("n1", "storefront"), deps[1])
}
