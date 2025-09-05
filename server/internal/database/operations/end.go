package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// EndState computes the end state for the database, containing only the given
// nodes.
func EndState(nodes []*NodeResources) (*resource.State, error) {
	end := resource.NewState()
	for _, node := range nodes {
		instanceIDs := make([]string, len(node.InstanceResources))
		for i, inst := range node.InstanceResources {
			instanceIDs[i] = inst.InstanceID()
			state, err := instanceState(inst)
			if err != nil {
				return nil, err
			}
			end.Merge(state)
		}
		err := end.AddResource(&database.NodeResource{
			Name:        node.NodeName,
			InstanceIDs: instanceIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node resource: %w", err)
		}

		for _, peer := range nodes {
			if peer.NodeName == node.NodeName {
				continue
			}
			err := end.AddResource(&database.SubscriptionResource{
				SubscriberNode: peer.NodeName,
				ProviderNode:   node.NodeName,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to add subscription resource: %w", err)
			}
		}
	}

	return end, nil
}
