package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestPostgRESTPreflightResource_ResourceVersion(t *testing.T) {
	r := &PostgRESTPreflightResource{}
	assert.Equal(t, "1", r.ResourceVersion())
}

func TestPostgRESTPreflightResource_DiffIgnore(t *testing.T) {
	r := &PostgRESTPreflightResource{}
	assert.Nil(t, r.DiffIgnore())
}

func TestPostgRESTPreflightResourceIdentifier(t *testing.T) {
	id := PostgRESTPreflightResourceIdentifier("my-service")
	assert.Equal(t, "my-service", id.ID)
	assert.Equal(t, ResourceTypePostgRESTPreflightResource, id.Type)
}

func TestPostgRESTPreflightResource_Identifier(t *testing.T) {
	r := &PostgRESTPreflightResource{ServiceID: "api"}
	id := r.Identifier()
	assert.Equal(t, "api", id.ID)
	assert.Equal(t, ResourceTypePostgRESTPreflightResource, id.Type)
}

func TestPostgRESTPreflightResource_Executor(t *testing.T) {
	r := &PostgRESTPreflightResource{NodeName: "n1"}
	assert.Equal(t, resource.PrimaryExecutor("n1"), r.Executor())
}

func TestPostgRESTPreflightResource_TypeDependencies(t *testing.T) {
	r := &PostgRESTPreflightResource{}
	assert.Nil(t, r.TypeDependencies())
}

func TestPostgRESTPreflightResource_Dependencies(t *testing.T) {
	r := &PostgRESTPreflightResource{
		NodeName:     "n1",
		DatabaseName: "storefront",
	}
	deps := r.Dependencies()

	require.Len(t, deps, 1)
	assert.Equal(t, database.PostgresDatabaseResourceIdentifier("n1", "storefront"), deps[0])
}

func TestSplitSchemas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
		{
			name:  "single schema",
			input: "public",
			want:  []string{"public"},
		},
		{
			name:  "multiple schemas",
			input: "public,api,private",
			want:  []string{"public", "api", "private"},
		},
		{
			name:  "schemas with whitespace",
			input: " public , api , private ",
			want:  []string{"public", "api", "private"},
		},
		{
			name:  "trailing comma",
			input: "public,",
			want:  []string{"public"},
		},
		{
			name:  "leading comma",
			input: ",public",
			want:  []string{"public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSchemas(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
