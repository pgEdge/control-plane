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
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-1",
		NodeName:     "n1",
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	require.NotNil(t, result.ServiceInstance)
	assert.Equal(t, spec.ServiceInstanceID, result.ServiceInstance.ServiceInstanceID)
	assert.Equal(t, spec.HostID, result.ServiceInstance.HostID)
	assert.Equal(t, database.ServiceInstanceStateCreating, result.ServiceInstance.State)

	// Single node: Network + canonical RO + DirResource + Keys + Config + InstanceSpec + ServiceInstance = 7.
	require.Len(t, result.Resources, 7)
	assert.Equal(t, ResourceTypeNetwork, result.Resources[0].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceUserRole, result.Resources[1].Identifier.Type)
	assert.Equal(t, ServiceUserRoleIdentifier("rag", ServiceUserRoleRO), result.Resources[1].Identifier)
	assert.Equal(t, filesystem.ResourceTypeDir, result.Resources[2].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGServiceKeys, result.Resources[3].Identifier.Type)
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
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-1",
		NodeName:     "n1",
		DatabaseNodes: []*database.NodeInstances{
			{NodeName: "n1"},
			{NodeName: "n2"},
			{NodeName: "n3"},
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	// 3 nodes → Network + canonical(n1) + per-node(n2) + per-node(n3) + dir + keys + config + spec + instance = 9.
	require.Len(t, result.Resources, 9)

	// Resources[0] is Network; Resources[1..3] are ServiceUserRole resources.
	for i := 1; i < 4; i++ {
		assert.Equal(t, ResourceTypeServiceUserRole, result.Resources[i].Identifier.Type)
	}

	// Canonical is index 1 and has no CredentialSource.
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[1])
	require.NoError(t, err)
	assert.Nil(t, canonical.CredentialSource)
	assert.Equal(t, ServiceUserRoleRO, canonical.Mode)

	// Per-node resources point back to canonical.
	canonicalID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	for i, rd := range result.Resources[2:4] {
		perNode, err := resource.ToResource[*ServiceUserRole](rd)
		require.NoErrorf(t, err, "ToResource per-node[%d]", i)
		assert.Equalf(t, &canonicalID, perNode.CredentialSource, "per-node[%d].CredentialSource", i)
		assert.Equalf(t, ServiceUserRoleRO, perNode.Mode, "per-node[%d].Mode", i)
	}

	assert.Equal(t, filesystem.ResourceTypeDir, result.Resources[4].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGServiceKeys, result.Resources[5].Identifier.Type)
	assert.Equal(t, ResourceTypeRAGConfig, result.Resources[6].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstanceSpec, result.Resources[7].Identifier.Type)
	assert.Equal(t, ResourceTypeServiceInstance, result.Resources[8].Identifier.Type)
}

func TestGenerateRAGInstanceResources_MultiNode_CanonicalNotFirst(t *testing.T) {
	o := newTestOrchestrator()
	spec := &database.ServiceInstanceSpec{
		ServiceInstanceID: "storefront-rag-host2",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "rag",
			ServiceType: "rag",
			Version:     "latest",
			Config:      minimalRAGConfig(),
		},
		DatabaseID:   "storefront",
		DatabaseName: "storefront",
		HostID:       "host-2",
		NodeName:     "n2", // canonical is n2, not at index 0
		DatabaseNodes: []*database.NodeInstances{
			{NodeName: "n1"},
			{NodeName: "n2"},
			{NodeName: "n3"},
		},
	}

	result, err := o.generateRAGInstanceResources(spec)
	require.NoError(t, err)

	// 3 nodes → Network + canonical(n2) + per-node(n1) + per-node(n3) + dir + keys + config + spec + instance = 9.
	require.Len(t, result.Resources, 9)

	// Canonical (index 1, after Network) must be n2 with no CredentialSource.
	canonical, err := resource.ToResource[*ServiceUserRole](result.Resources[1])
	require.NoError(t, err)
	assert.Nil(t, canonical.CredentialSource)
	assert.Equal(t, "n2", canonical.NodeName)

	// Per-node resources must cover n1 and n3, not n2.
	canonicalID := ServiceUserRoleIdentifier("rag", ServiceUserRoleRO)
	perNodeNames := make(map[string]bool)
	for i, rd := range result.Resources[2:4] {
		perNode, err := resource.ToResource[*ServiceUserRole](rd)
		require.NoErrorf(t, err, "ToResource per-node[%d]", i)
		assert.Equalf(t, &canonicalID, perNode.CredentialSource, "per-node[%d].CredentialSource", i)
		perNodeNames[perNode.NodeName] = true
	}
	assert.False(t, perNodeNames["n2"], "canonical node n2 must not appear in per-node resources")
	assert.True(t, perNodeNames["n1"], "n1 must be a per-node resource")
	assert.True(t, perNodeNames["n3"], "n3 must be a per-node resource")
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
		},
		DatabaseID:   "db1",
		DatabaseName: "db1",
		HostID:       "host-1",
		NodeName:     "n1",
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
		},
		DatabaseID:    "db1",
		DatabaseName:  "db1",
		HostID:        "host-1",
		NodeName:      "n1",
		PgEdgeVersion: ds.MustPgEdgeVersion("17", "5.0.0"),
	}

	_, err := o.generateRAGInstanceResources(spec)
	require.ErrorContains(t, err, "not compatible")
}
