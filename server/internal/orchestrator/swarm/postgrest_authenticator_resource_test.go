package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestPostgRESTAuthenticatorResource_ResourceVersion(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{}
	assert.Equal(t, "1", r.ResourceVersion())
}

func TestPostgRESTAuthenticatorResource_DiffIgnore(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{}
	assert.Nil(t, r.DiffIgnore())
}

func TestPostgRESTAuthenticatorResourceIdentifier(t *testing.T) {
	id := PostgRESTAuthenticatorIdentifier("api", "n1")
	assert.Equal(t, "api-auth-n1", id.ID)
	assert.Equal(t, ResourceTypePostgRESTAuthenticator, id.Type)
}

func TestPostgRESTAuthenticatorResource_Identifier(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{ServiceID: "api", NodeName: "n2"}
	id := r.Identifier()
	assert.Equal(t, "api-auth-n2", id.ID)
	assert.Equal(t, ResourceTypePostgRESTAuthenticator, id.Type)
}

func TestPostgRESTAuthenticatorResource_Executor(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{NodeName: "n1"}
	assert.Equal(t, resource.PrimaryExecutor("n1"), r.Executor())
}

func TestPostgRESTAuthenticatorResource_TypeDependencies(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{}
	assert.Nil(t, r.TypeDependencies())
}

func TestPostgRESTAuthenticatorResource_Dependencies(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{
		NodeName:     "n1",
		DatabaseName: "storefront",
	}
	deps := r.Dependencies()

	require.Len(t, deps, 2)
	assert.Equal(t, database.NodeResourceIdentifier("n1"), deps[0])
	assert.Equal(t, database.PostgresDatabaseResourceIdentifier("n1", "storefront"), deps[1])
}

func TestPostgRESTAuthenticatorResource_DesiredAnonRole(t *testing.T) {
	t.Run("empty string falls back to default", func(t *testing.T) {
		r := &PostgRESTAuthenticatorResource{DBAnonRole: ""}
		assert.Equal(t, "pgedge_application_read_only", r.desiredAnonRole())
	})

	t.Run("custom anon role is preserved", func(t *testing.T) {
		r := &PostgRESTAuthenticatorResource{DBAnonRole: "web_anon"}
		assert.Equal(t, "web_anon", r.desiredAnonRole())
	})
}

func TestPostgRESTAuthenticatorResource_AuthenticatorUsername(t *testing.T) {
	r := &PostgRESTAuthenticatorResource{ConnectAsUsername: "app"}
	assert.Equal(t, "app", r.authenticatorUsername())
}

func TestPostgRESTAuthenticatorIdentifier_DifferentNodes(t *testing.T) {
	id1 := PostgRESTAuthenticatorIdentifier("api", "n1")
	id2 := PostgRESTAuthenticatorIdentifier("api", "n2")
	assert.NotEqual(t, id1, id2, "different nodes should produce different identifiers")
}

func TestPostgRESTAuthenticatorIdentifier_DifferentServices(t *testing.T) {
	id1 := PostgRESTAuthenticatorIdentifier("svc-a", "n1")
	id2 := PostgRESTAuthenticatorIdentifier("svc-b", "n1")
	assert.NotEqual(t, id1, id2, "different services should produce different identifiers")
}
