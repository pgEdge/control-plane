package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations"
)

func TestVersion_2_0_0(t *testing.T) {
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

		migration := &migrations.Version_2_0_0{}
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

		migration := &migrations.Version_2_0_0{}
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

		migration := &migrations.Version_2_0_0{}
		err := migration.Run(state)
		require.NoError(t, err)
	})
}
