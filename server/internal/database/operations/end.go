package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// EndState computes the end state for the database, containing only the given
// nodes.
func EndState(nodes []*NodeResources, services []*ServiceResources) (*resource.State, error) {
	end := resource.NewState()
	for _, node := range nodes {
		var resources []resource.Resource

		for _, inst := range node.InstanceResources {
			state, err := inst.InstanceState()
			if err != nil {
				return nil, err
			}
			end.Merge(state)
		}

		nodeState, err := node.nodeResourceState()
		if err != nil {
			return nil, err
		}
		end.Merge(nodeState)

		databaseState, err := node.databaseResourceState()
		if err != nil {
			return nil, err
		}
		end.Merge(databaseState)

		for _, peer := range nodes {
			if peer.NodeName == node.NodeName {
				continue
			}
			resources = append(resources,
				&database.ReplicationSlotResource{
					DatabaseName:   node.DatabaseName,
					ProviderNode:   node.NodeName,
					SubscriberNode: peer.NodeName,
				},
				&database.SubscriptionResource{
					DatabaseName:   node.DatabaseName,
					SubscriberNode: peer.NodeName,
					ProviderNode:   node.NodeName,
				},
			)
		}

		if err := end.AddResource(resources...); err != nil {
			return nil, fmt.Errorf("failed to add end state resource: %w", err)
		}
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
