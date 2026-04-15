package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// newTestOrchestrator returns an Orchestrator with serviceVersions initialised
// from a minimal config, suitable for unit tests that call generateRAGInstanceResources.
func newTestOrchestrator() *Orchestrator {
	return &Orchestrator{
		serviceVersions: NewServiceVersions(config.Config{}),
	}
}

// minimalRAGConfig returns a minimal valid RAG service config suitable for unit tests.
func minimalRAGConfig() map[string]any {
	return map[string]any{
		"pipelines": []any{
			map[string]any{
				"name": "default",
				"tables": []any{
					map[string]any{
						"table":         "docs",
						"text_column":   "content",
						"vector_column": "embedding",
					},
				},
				"embedding_llm": map[string]any{
					"provider": "openai",
					"model":    "text-embedding-3-small",
					"api_key":  "sk-embed",
				},
				"rag_llm": map[string]any{
					"provider": "anthropic",
					"model":    "claude-sonnet-4-5",
					"api_key":  "sk-ant",
				},
			},
		},
	}
}

func TestGenerateRAGInstanceResources_ResourceList(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
			ConnectAs:   "app_read_only",
		},
		DatabaseID:        "storefront",
		DatabaseName:      "storefront",
		HostID:            "host-1",
		NodeName:          "n1",
		ConnectAsUsername: "app_read_only",
		ConnectAsPassword: "secret",
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	require.NotNil(t, result.ServiceInstance)
	assert.Equal(t, spec.ServiceInstanceID, result.ServiceInstance.ServiceInstanceID)
	assert.Equal(t, spec.HostID, result.ServiceInstance.HostID)
	assert.Equal(t, database.ServiceInstanceStateCreating, result.ServiceInstance.State)

	// Network + DirResource + Keys + Preflight + Config + InstanceSpec + ServiceInstance = 7.
	require.Len(t, result.Resources, 7)
	assert.Equal(t, ResourceTypeNetwork, result.Resources[0].Identifier.Type)
	assert.Equal(t, filesystem.ResourceTypeDir, result.Resources[1].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGServiceKeys, result.Resources[2].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGPreflightResource, result.Resources[3].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGConfig, result.Resources[4].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstanceSpec, result.Resources[5].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstance, result.Resources[6].Identifier.Type)
}

func TestGenerateRAGInstanceResources_MultiNode(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
			ConnectAs:   "app_read_only",
		},
		DatabaseID:        "storefront",
		DatabaseName:      "storefront",
		HostID:            "host-1",
		NodeName:          "n1",
		ConnectAsUsername: "app_read_only",
		ConnectAsPassword: "secret",
		DatabaseNodes: []*database.NodeInstances{
			{NodeName: "n1"},
			{NodeName: "n2"},
			{NodeName: "n3"},
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	// Multi-node with connect_as: no ServiceUserRole resources regardless of node count.
	// Network + DirResource + Keys + Preflight + Config + InstanceSpec + ServiceInstance = 7.
	require.Len(t, result.Resources, 7)
	assert.Equal(t, ResourceTypeNetwork, result.Resources[0].Identifier.Type)
	assert.Equal(t, filesystem.ResourceTypeDir, result.Resources[1].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGServiceKeys, result.Resources[2].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGPreflightResource, result.Resources[3].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGConfig, result.Resources[4].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstanceSpec, result.Resources[5].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstance, result.Resources[6].Identifier.Type)
}

func TestGenerateServiceInstanceResources_RAGDispatch(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
			ConnectAs:   "app_read_only",
		},
		DatabaseID:        "db1",
		DatabaseName:      "db1",
		HostID:            "host-1",
		NodeName:          "n1",
		ConnectAsUsername: "app_read_only",
		ConnectAsPassword: "secret",
	}

	result, err := o.GenerateServiceInstanceResources(spec)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGenerateServiceInstanceResources_UnknownTypeReturnsError(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-unknown-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "unknown",
			ServiceType: "unknown",
			Version:     "latest",
		},
		DatabaseID:   "db1",
		DatabaseName: "db1",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	_, err := o.GenerateServiceInstanceResources(spec)
	require.Error(t, err)
}

func TestGenerateRAGInstanceResources_ConnectAs_CredentialsPopulated(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
			ConnectAs:   "app_read_only",
		},
		DatabaseID:        "storefront",
		DatabaseName:      "storefront",
		HostID:            "host-1",
		NodeName:          "n1",
		ConnectAsUsername: "app_read_only",
		ConnectAsPassword: "secret",
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	// RAGConfigResource should carry the connect_as credentials.
	configRes, err := resource.ToResource[*RAGConfigResource](result.Resources[4])
	require.NoError(t, err)
	assert.Equal(t, "app_read_only", configRes.ConnectAsUsername)
	assert.Equal(t, "secret", configRes.ConnectAsPassword)

	// ServiceInstanceResource should also carry ConnectAsUsername.
	svcInst, err := resource.ToResource[*ServiceInstanceResource](result.Resources[6])
	require.NoError(t, err)
	assert.Equal(t, "app_read_only", svcInst.ConnectAsUsername)
}

func TestGenerateRAGInstanceResources_IncompatibleVersion(t *testing.T) {
	o := newTestOrchestrator()
	// Override the "rag/latest" image with a constraint requiring PG >= 18.
	o.serviceVersions.addServiceImage("rag", "latest", &ServiceImage{
		Tag: "rag-server:latest",
		PostgresConstraint: &ds.VersionConstraint{
			Min: ds.MustParseVersion("18"),
		},
	})

	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "db1-rag-host1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
			ConnectAs:   "app_read_only",
		},
		DatabaseID:        "db1",
		DatabaseName:      "db1",
		HostID:            "host-1",
		NodeName:          "n1",
		ConnectAsUsername: "app_read_only",
		ConnectAsPassword: "secret",
		PgEdgeVersion:     ds.MustPgEdgeVersion("17", "5.0.0"),
	}

	_, err := o.generateRAGInstanceResources(spec)
	require.ErrorContains(t, err, "not compatible")
}
