package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// nodeEndState computes the end state containing only database node resources
// (instances, subscriptions, replication slots, etc.) without service resources.
// Used as an intermediate plan step to ensure all nodes are healthy before
// service resources (such as service user roles) are created.
func nodeEndState(nodes []*NodeResources) (*resource.State, error) {
	end := resource.NewState()
	for _, node := range nodes {
		var resources []resource.Resource

		instanceIDs := make([]string, len(node.InstanceResources))
		for i, inst := range node.InstanceResources {
			instanceIDs[i] = inst.InstanceID()
			state, err := instanceState(inst)
			if err != nil {
				return nil, err
			}
			end.Merge(state)
		}
		resources = append(resources, &database.NodeResource{
			Name:        node.NodeName,
			InstanceIDs: instanceIDs,
		})

		if len(node.InstanceResources) > 1 {
			primary := node.primaryInstance()
			if primary != nil {
				// Primary will be non-nil for existing nodes. Adding the
				// switchover resource to the end state prevents "permadrift"
				// where this resource is created and deleted even if the update
				// is a no-op.
				resources = append(resources, &database.SwitchoverResource{
					HostID:     primary.HostID(),
					InstanceID: primary.InstanceID(),
					TargetRole: patroni.InstanceRolePrimary,
				})
			}
		}

		for _, peer := range nodes {
			if peer.NodeName == node.NodeName {
				continue
			}
			resources = append(resources,
				&database.ReplicationSlotResource{
					ProviderNode:   node.NodeName,
					SubscriberNode: peer.NodeName,
				},
				&database.SubscriptionResource{
					SubscriberNode: peer.NodeName,
					ProviderNode:   node.NodeName,
				},
			)
		}

		if err := end.AddResource(resources...); err != nil {
			return nil, fmt.Errorf("failed to add end state resource: %w", err)
		}
	}

	return end, nil
}

// EndState computes the end state for the database, containing only the given
// nodes.
func EndState(nodes []*NodeResources, services []*ServiceResources) (*resource.State, error) {
	end, err := nodeEndState(nodes)
	if err != nil {
		return nil, err
	}

	for _, svc := range services {
		state, err := svc.State()
		if err != nil {
			return nil, err
		}
		end.Merge(state)
	}

	return end, nil
}
