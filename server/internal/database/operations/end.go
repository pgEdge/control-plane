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

		// TODO(PLAT-665): Commented out to fix rolling update failures caused by
		// the Spock 5.x replication slot race condition. The end-state
		// switchover that restores the original primary runs before Spock's
		// worker has had time to create failover slots (~60 s), breaking all
		// subscriptions to that node. Re-enable this block to restore
		// "retain primary" behavior once the race is fully resolved. When
		// re-enabling, verify that the "patroni" import is present and that
		// SwitchoverResource is also re-enabled in update_nodes.go.
		//
		// if len(node.InstanceResources) > 1 {
		// 	primary := node.primaryInstance()
		// 	if primary != nil {
		// 		// Primary will be non-nil for existing nodes. Adding the
		// 		// switchover resource to the end state prevents "permadrift"
		// 		// where this resource is created and deleted even if the
		// 		// update is a no-op.
		// 		resources = append(resources, &database.SwitchoverResource{
		// 			HostID:     primary.HostID(),
		// 			InstanceID: primary.InstanceID(),
		// 			TargetRole: patroni.InstanceRolePrimary,
		// 		})
		// 	}
		// }

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
