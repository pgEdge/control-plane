package migrations

import (
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.StateMigration = (*Version_1_1_0)(nil)

// Version_1_1_0 removes swarm.service_user_role resources and scrubs
// references to them from all other resources' dependency lists.
// Services now use connect_as to reference database_users directly.
type Version_1_1_0 struct{}

func (v *Version_1_1_0) Version() *ds.Version {
	return resource.StateVersion_1_1_0
}

func (v *Version_1_1_0) Run(state *resource.State) error {
	const serviceUserRoleType resource.Type = "swarm.service_user_role"

	// 1. Delete all service_user_role resources from state
	delete(state.Resources, serviceUserRoleType)

	// 2. Remove service_user_role from all other resources' dependency lists
	for _, resources := range state.Resources {
		for _, data := range resources {
			filtered := data.Dependencies[:0]
			for _, dep := range data.Dependencies {
				if dep.Type != serviceUserRoleType {
					filtered = append(filtered, dep)
				}
			}
			data.Dependencies = filtered
		}
	}

	return nil
}
