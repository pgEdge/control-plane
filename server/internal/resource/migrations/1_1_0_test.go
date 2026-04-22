package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations"
)

func TestVersion_1_1_0(t *testing.T) {
	serviceUserRoleType := resource.Type("swarm.service_user_role")
	mcpConfigType := resource.Type("swarm.mcp_config")
	serviceInstanceSpecType := resource.Type("swarm.service_instance_spec")
	networkType := resource.Type("swarm.network")
	dirType := resource.Type("swarm.dir")

	svcRoleRO := resource.Identifier{ID: "appmcp-ro", Type: serviceUserRoleType}
	svcRoleRW := resource.Identifier{ID: "appmcp-rw", Type: serviceUserRoleType}
	networkDep := resource.Identifier{ID: "db-network", Type: networkType}
	dirDep := resource.Identifier{ID: "data-dir", Type: dirType}

	t.Run("removes service_user_role resources", func(t *testing.T) {
		state := &resource.State{
			Version: resource.StateVersion_1_0_0.Clone(),
			Resources: map[resource.Type]map[string]*resource.ResourceData{
				serviceUserRoleType: {
					"appmcp-ro": {Identifier: svcRoleRO},
					"appmcp-rw": {Identifier: svcRoleRW},
				},
				mcpConfigType: {
					"mcp-cfg": {
						Identifier:   resource.Identifier{ID: "mcp-cfg", Type: mcpConfigType},
						Dependencies: []resource.Identifier{dirDep, svcRoleRO, svcRoleRW},
					},
				},
				serviceInstanceSpecType: {
					"svc-spec": {
						Identifier:   resource.Identifier{ID: "svc-spec", Type: serviceInstanceSpecType},
						Dependencies: []resource.Identifier{networkDep, svcRoleRO, svcRoleRW},
					},
				},
			},
		}

		migration := &migrations.Version_1_1_0{}
		err := migration.Run(state)
		require.NoError(t, err)

		// service_user_role resources should be gone
		_, exists := state.Resources[serviceUserRoleType]
		assert.False(t, exists, "service_user_role resources should be deleted")

		// mcp_config should have service_user_role deps removed
		mcpCfg := state.Resources[mcpConfigType]["mcp-cfg"]
		require.NotNil(t, mcpCfg)
		assert.Equal(t, []resource.Identifier{dirDep}, mcpCfg.Dependencies)

		// service_instance_spec should have service_user_role deps removed
		svcSpec := state.Resources[serviceInstanceSpecType]["svc-spec"]
		require.NotNil(t, svcSpec)
		assert.Equal(t, []resource.Identifier{networkDep}, svcSpec.Dependencies)
	})

	t.Run("removes service_user_role resources across all service types", func(t *testing.T) {
		ragConfigType := resource.Type("swarm.rag_config")
		postgrestConfigType := resource.Type("swarm.postgrest_config")

		mcpRO := resource.Identifier{ID: "appmcp-ro", Type: serviceUserRoleType}
		mcpRW := resource.Identifier{ID: "appmcp-rw", Type: serviceUserRoleType}
		ragRO := resource.Identifier{ID: "apprag-ro", Type: serviceUserRoleType}
		prstRO := resource.Identifier{ID: "appprst-ro", Type: serviceUserRoleType}
		prstRW := resource.Identifier{ID: "appprst-rw", Type: serviceUserRoleType}

		state := &resource.State{
			Version: resource.StateVersion_1_0_0.Clone(),
			Resources: map[resource.Type]map[string]*resource.ResourceData{
				serviceUserRoleType: {
					"appmcp-ro":  {Identifier: mcpRO},
					"appmcp-rw":  {Identifier: mcpRW},
					"apprag-ro":  {Identifier: ragRO},
					"appprst-ro": {Identifier: prstRO},
					"appprst-rw": {Identifier: prstRW},
				},
				mcpConfigType: {
					"mcp-cfg": {
						Identifier:   resource.Identifier{ID: "mcp-cfg", Type: mcpConfigType},
						Dependencies: []resource.Identifier{dirDep, mcpRO, mcpRW},
					},
				},
				ragConfigType: {
					"rag-cfg": {
						Identifier:   resource.Identifier{ID: "rag-cfg", Type: ragConfigType},
						Dependencies: []resource.Identifier{dirDep, ragRO},
					},
				},
				postgrestConfigType: {
					"prst-cfg": {
						Identifier:   resource.Identifier{ID: "prst-cfg", Type: postgrestConfigType},
						Dependencies: []resource.Identifier{dirDep, prstRO, prstRW},
					},
				},
				serviceInstanceSpecType: {
					"svc-spec": {
						Identifier:   resource.Identifier{ID: "svc-spec", Type: serviceInstanceSpecType},
						Dependencies: []resource.Identifier{networkDep, mcpRO, mcpRW, ragRO, prstRO, prstRW},
					},
				},
			},
		}

		migration := &migrations.Version_1_1_0{}
		err := migration.Run(state)
		require.NoError(t, err)

		// All service_user_role resources should be gone
		_, exists := state.Resources[serviceUserRoleType]
		assert.False(t, exists, "service_user_role resources should be deleted")

		// MCP config: only dirDep remains
		assert.Equal(t, []resource.Identifier{dirDep}, state.Resources[mcpConfigType]["mcp-cfg"].Dependencies)

		// RAG config: only dirDep remains
		assert.Equal(t, []resource.Identifier{dirDep}, state.Resources[ragConfigType]["rag-cfg"].Dependencies)

		// PostgREST config: only dirDep remains
		assert.Equal(t, []resource.Identifier{dirDep}, state.Resources[postgrestConfigType]["prst-cfg"].Dependencies)

		// service_instance_spec: only networkDep remains
		assert.Equal(t, []resource.Identifier{networkDep}, state.Resources[serviceInstanceSpecType]["svc-spec"].Dependencies)
	})

	t.Run("no-op when no service_user_role resources exist", func(t *testing.T) {
		state := &resource.State{
			Version: resource.StateVersion_1_0_0.Clone(),
			Resources: map[resource.Type]map[string]*resource.ResourceData{
				mcpConfigType: {
					"mcp-cfg": {
						Identifier:   resource.Identifier{ID: "mcp-cfg", Type: mcpConfigType},
						Dependencies: []resource.Identifier{dirDep},
					},
				},
			},
		}

		migration := &migrations.Version_1_1_0{}
		err := migration.Run(state)
		require.NoError(t, err)

		// mcp_config should be untouched
		mcpCfg := state.Resources[mcpConfigType]["mcp-cfg"]
		require.NotNil(t, mcpCfg)
		assert.Equal(t, []resource.Identifier{dirDep}, mcpCfg.Dependencies)
	})

	t.Run("empty state", func(t *testing.T) {
		state := &resource.State{
			Version:   resource.StateVersion_1_0_0.Clone(),
			Resources: map[resource.Type]map[string]*resource.ResourceData{},
		}

		migration := &migrations.Version_1_1_0{}
		err := migration.Run(state)
		require.NoError(t, err)
	})
}
