package operations_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestUpdateDatabase(t *testing.T) {
	n1Instance1 := makeInstance(t, "n1", 1)
	n1Instance2 := makeInstance(t, "n1", 2)
	n2Instance1 := makeInstance(t, "n2", 1)
	n2Instance2 := makeInstance(t, "n2", 2)
	n1Instance1WithNewDependency := makeInstance(t, "n1", 1,
		makeOrchestratorResource(t, "n1", 1, 1),
		makeOrchestratorResource(t, "n1", 1, 2),
	)
	n3Instance1 := makeInstance(t, "n3", 1)

	singleNodeState := makeState(t,
		[]resource.Resource{
			n1Instance1.Instance,
			makeMonitorResource(n1Instance1),
			&database.NodeResource{
				Name:              "n1",
				PrimaryInstanceID: n1Instance1.InstanceID(),
				InstanceIDs:       []string{n1Instance1.InstanceID()},
			},
		},
		n1Instance1.Resources,
	)
	twoNodeState := makeState(t,
		[]resource.Resource{
			n1Instance1.Instance,
			makeMonitorResource(n1Instance1),
			&database.NodeResource{
				Name:              "n1",
				PrimaryInstanceID: n1Instance1.InstanceID(),
				InstanceIDs:       []string{n1Instance1.InstanceID()},
			},
			n2Instance1.Instance,
			makeMonitorResource(n2Instance1),
			&database.NodeResource{
				Name:              "n2",
				PrimaryInstanceID: n2Instance1.InstanceID(),
				InstanceIDs:       []string{n2Instance1.InstanceID()},
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n2",
				SubscriberNode: "n1",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n1",
				ProviderNode:   "n2",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n1",
				SubscriberNode: "n2",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n2",
				ProviderNode:   "n1",
			},
		},
		slices.Concat(
			n1Instance1.Resources,
			n2Instance1.Resources,
		),
	)

	threeNodeState := makeState(t,
		[]resource.Resource{
			n1Instance1.Instance,
			makeMonitorResource(n1Instance1),
			&database.NodeResource{
				Name:              "n1",
				PrimaryInstanceID: n1Instance1.InstanceID(),
				InstanceIDs:       []string{n1Instance1.InstanceID()},
			},
			n2Instance1.Instance,
			makeMonitorResource(n2Instance1),
			&database.NodeResource{
				Name:              "n2",
				PrimaryInstanceID: n2Instance1.InstanceID(),
				InstanceIDs:       []string{n2Instance1.InstanceID()},
			},
			n3Instance1.Instance,
			makeMonitorResource(n3Instance1),
			&database.NodeResource{
				Name:              "n3",
				PrimaryInstanceID: n3Instance1.InstanceID(),
				InstanceIDs:       []string{n3Instance1.InstanceID()},
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n2",
				SubscriberNode: "n1",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n1",
				ProviderNode:   "n2",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n1",
				SubscriberNode: "n2",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n2",
				ProviderNode:   "n1",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n3",
				SubscriberNode: "n1",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n1",
				ProviderNode:   "n3",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n1",
				SubscriberNode: "n3",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n3",
				ProviderNode:   "n1",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n3",
				SubscriberNode: "n2",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n2",
				ProviderNode:   "n3",
			},
			&database.ReplicationSlotResource{
				ProviderNode:   "n2",
				SubscriberNode: "n3",
			},
			&database.SubscriptionResource{
				SubscriberNode: "n3",
				ProviderNode:   "n2",
			},
		},
		slices.Concat(
			n1Instance1.Resources,
			n2Instance1.Resources,
			n3Instance1.Resources,
		),
	)

	svcRes := makeServiceResources(t, "database-id", "test-svc", "host-1-id", nil)

	singleNodeWithServiceState := makeState(t,
		[]resource.Resource{
			n1Instance1.Instance,
			makeMonitorResource(n1Instance1),
			&database.NodeResource{
				Name:              "n1",
				PrimaryInstanceID: n1Instance1.InstanceID(),
				InstanceIDs:       []string{n1Instance1.InstanceID()},
			},
			svcRes.MonitorResource,
		},
		slices.Concat(
			n1Instance1.Resources,
			svcRes.Resources,
		),
	)

	for _, tc := range []struct {
		name        string
		options     operations.UpdateDatabaseOptions
		start       *resource.State
		nodes       []*operations.NodeResources
		services    []*operations.ServiceResources
		expectedErr string
	}{
		{
			name:    "no-op",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
		},
		{
			name: "forced update",
			options: operations.UpdateDatabaseOptions{
				PlanOptions: resource.PlanOptions{
					ForceUpdate: true,
				},
			},
			start: singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
		},
		{
			name:    "single node from empty",
			options: operations.UpdateDatabaseOptions{},
			start:   resource.NewState(),
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
		},
		{
			name:    "multiple nodes from empty",
			options: operations.UpdateDatabaseOptions{},
			start:   resource.NewState(),
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "one node to two nodes with default source node",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "adding a replica",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			// Both subscriptions get update events, even though only one node
			// is changing, because n1 is a dependency for both subscriptions.
			// The subscription's update function will exit early when it sees
			// that the subscription does not need to be updated.
			nodes: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "add an instance dependency",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1WithNewDependency,
					},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name: "add an instance dependency with forced update",
			options: operations.UpdateDatabaseOptions{
				PlanOptions: resource.PlanOptions{
					ForceUpdate: true,
				},
			},
			start: twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1WithNewDependency,
					},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name: "adding multiple replicas rolling",
			// Rolling is the default
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName: "n2",
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
		},
		{
			name: "adding multiple replicas concurrent",
			options: operations.UpdateDatabaseOptions{
				NodeUpdateStrategy: operations.NodeUpdateStrategyConcurrent,
			},
			start: twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName: "n1",
					InstanceResources: []*database.InstanceResources{
						n1Instance1,
						n1Instance2,
					},
				},
				{
					NodeName: "n2",
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
		},
		{
			name:    "remove node",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
		},
		{
			name:    "add update and remove node",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1WithNewDependency},
				},
				{
					NodeName:          "n3",
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
		},
		{
			name:    "one node to two nodes with populate",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					SourceNode:        "n1",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "two nodes to three nodes with populate",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
				{
					NodeName:          "n3",
					SourceNode:        "n1",
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
		},
		{
			name:    "one node to two nodes with populate and replica",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:   "n2",
					SourceNode: "n1",
					InstanceResources: []*database.InstanceResources{
						n2Instance1,
						n2Instance2,
					},
				},
			},
		},
		{
			name:    "remove one node from three node database",
			options: operations.UpdateDatabaseOptions{},
			start:   threeNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "remove two nodes from three node database",
			options: operations.UpdateDatabaseOptions{},
			start:   threeNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n2",
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
		},
		{
			name:    "delete two node database",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
		},
		{
			name:    "single node with service from empty",
			options: operations.UpdateDatabaseOptions{},
			start:   resource.NewState(),
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			services: []*operations.ServiceResources{
				makeServiceResources(t, "database-id", "test-svc", "host-1-id", nil),
			},
		},
		{
			name:    "single node with service no-op",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeWithServiceState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			services: []*operations.ServiceResources{svcRes},
		},
		{
			name:    "add service to existing database",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			services: []*operations.ServiceResources{
				makeServiceResources(t, "database-id", "test-svc", "host-1-id", nil),
			},
		},
		{
			name:    "remove service from existing database",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeWithServiceState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			services: nil,
		},
		{
			name:    "update database node with unchanged service",
			options: operations.UpdateDatabaseOptions{},
			start:   singleNodeWithServiceState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n1",
					InstanceResources: []*database.InstanceResources{n1Instance1WithNewDependency},
				},
			},
			services: []*operations.ServiceResources{svcRes},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans, err := operations.UpdateDatabase(
				tc.options,
				tc.start,
				tc.nodes,
				tc.services,
			)
			if tc.expectedErr != "" {
				assert.Nil(t, plans)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)

				actual := resource.SummarizePlans(plans)
				golden := &testutils.GoldenTest[[]resource.PlanSummary]{
					Compare: assertPlansEqual,
				}
				golden.Run(t, actual, update)
			}
		})
	}
}
