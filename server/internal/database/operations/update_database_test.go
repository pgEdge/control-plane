package operations_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
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
			&database.SubscriptionResource{
				SubscriberNode: "n1",
				ProviderNode:   "n2",
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

	for _, tc := range []struct {
		name        string
		options     operations.UpdateDatabaseOptions
		start       *resource.State
		nodes       []*operations.NodeResources
		expected    []resource.Plan
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
			expected: []resource.Plan{},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: n1Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n1",
								InstanceIDs: []string{n1Instance1.InstanceID()},
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n1",
								InstanceIDs: []string{n1Instance1.InstanceID()},
							}),
						},
					},
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
			// Adds happen simultaneously, followed by subscriptions since this
			// configuration has more than one node.
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1.Resources[0],
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n1",
								InstanceIDs: []string{n1Instance1.InstanceID()},
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n2",
								InstanceIDs: []string{n2Instance1.InstanceID()},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n2",
								InstanceIDs: []string{n2Instance1.InstanceID()},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								SyncStructure:  true,
								SyncData:       true,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance2.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance2.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance2)),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1.InstanceID(),
									n1Instance2.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SwitchoverResource{
								HostID:     n1Instance1.HostID(),
								InstanceID: n1Instance1.InstanceID(),
								TargetRole: patroni.InstanceRolePrimary,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithNewDependency.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1WithNewDependency.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithNewDependency)),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1WithNewDependency.InstanceID(),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
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
			// Forced update will force all resources to update at the end. Some
			// of the operations are redundant, but it should be safe.
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithNewDependency.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1WithNewDependency.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithNewDependency)),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1WithNewDependency.InstanceID(),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: n1Instance1WithNewDependency.Resources[0],
						},
						{
							Type:     resource.EventTypeUpdate,
							Resource: n1Instance1WithNewDependency.Resources[1],
						},
						{
							Type:     resource.EventTypeUpdate,
							Resource: n2Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1WithNewDependency.Instance),
						},
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithNewDependency)),
						},
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1WithNewDependency.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n2",
								InstanceIDs: []string{
									n2Instance1.InstanceID(),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance2.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance2.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance2)),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1.InstanceID(),
									n1Instance2.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SwitchoverResource{
								HostID:     n1Instance1.HostID(),
								InstanceID: n1Instance1.InstanceID(),
								TargetRole: patroni.InstanceRolePrimary,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance2.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance2.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance2)),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n2",
								InstanceIDs: []string{
									n2Instance1.InstanceID(),
									n2Instance2.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SwitchoverResource{
								HostID:     n2Instance1.HostID(),
								InstanceID: n2Instance1.InstanceID(),
								TargetRole: patroni.InstanceRolePrimary,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance2.Resources[0],
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance2.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance2.Instance),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance2.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance2)),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance2)),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1.InstanceID(),
									n1Instance2.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n2",
								InstanceIDs: []string{
									n2Instance1.InstanceID(),
									n2Instance2.InstanceID(),
								},
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SwitchoverResource{
								HostID:     n1Instance1.HostID(),
								InstanceID: n1Instance1.InstanceID(),
								TargetRole: patroni.InstanceRolePrimary,
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SwitchoverResource{
								HostID:     n2Instance1.HostID(),
								InstanceID: n2Instance1.InstanceID(),
								TargetRole: patroni.InstanceRolePrimary,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
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
			expected: []resource.Plan{
				{
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:              "n2",
								PrimaryInstanceID: n2Instance1.InstanceID(),
								InstanceIDs:       []string{n2Instance1.InstanceID()},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n2Instance1.Resources[0],
						},
					},
				},
			},
		},
		{
			name:    "add, update, and remove node",
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
			// Should update first, then add, then delete
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithNewDependency.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1WithNewDependency.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithNewDependency)),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1WithNewDependency.InstanceID(),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n3Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n3Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n3Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n3",
								InstanceIDs: []string{n3Instance1.InstanceID()},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n3",
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:              "n2",
								PrimaryInstanceID: n2Instance1.InstanceID(),
								InstanceIDs:       []string{n2Instance1.InstanceID()},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n2Instance1.Resources[0],
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n2",
								InstanceIDs: []string{n2Instance1.InstanceID()},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								SyncStructure:  true,
								SyncData:       true,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n3Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n3Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n3Instance1)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n3",
								InstanceIDs: []string{n3Instance1.InstanceID()},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n3",
								ProviderNode:   "n2",
								Disabled:       true,
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.ReplicationSlotCreateResource{
								DatabaseName:   n3Instance1.DatabaseName(),
								SubscriberNode: "n3",
								ProviderNode:   "n2",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
								ExtraDependencies: []resource.Identifier{
									database.ReplicationSlotCreateResourceIdentifier(
										n3Instance1.DatabaseName(),
										"n2",
										"n3",
									),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
								SyncStructure:  true,
								SyncData:       true,
								ExtraDependencies: []resource.Identifier{
									database.WaitForSyncEventResourceIdentifier("n2", "n1"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n3"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.LagTrackerCommitTimestampResource{
								ReceiverNode: "n3",
								OriginNode:   "n2",
								ExtraDependencies: []resource.Identifier{
									database.WaitForSyncEventResourceIdentifier("n1", "n3"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.ReplicationSlotAdvanceFromCTSResource{
								SubscriberNode: "n3",
								ProviderNode:   "n2",
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n3",
							}),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n3",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n3",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.ReplicationSlotAdvanceFromCTSResource{
								SubscriberNode: "n3",
								ProviderNode:   "n2",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.LagTrackerCommitTimestampResource{
								ReceiverNode: "n3",
								OriginNode:   "n2",
								ExtraDependencies: []resource.Identifier{
									database.WaitForSyncEventResourceIdentifier("n1", "n3"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n3",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n3"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
								ExtraDependencies: []resource.Identifier{
									database.ReplicationSlotCreateResourceIdentifier(
										n3Instance1.DatabaseName(),
										"n2",
										"n3",
									),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.ReplicationSlotCreateResource{
								DatabaseName:   n3Instance1.DatabaseName(),
								SubscriberNode: "n3",
								ProviderNode:   "n2",
							}),
						},
					},
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
			expected: []resource.Plan{
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance1.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance2.Resources[0],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance2.Instance),
						},
					},
					{

						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance2)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n2",
								InstanceIDs: []string{
									n2Instance1.InstanceID(),
									n2Instance2.InstanceID(),
								},
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								SyncStructure:  true,
								SyncData:       true,
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
				},
				{
					{
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeUpdate,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.WaitForSyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SyncEventResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
								ExtraDependencies: []resource.Identifier{
									database.SubscriptionResourceIdentifier("n1", "n2"),
								},
							}),
						},
					},
				},
			},
		},
		{
			name:    "delete two node database",
			options: operations.UpdateDatabaseOptions{},
			start:   twoNodeState,
			expected: []resource.Plan{
				{
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n1",
								ProviderNode:   "n2",
							}),
						},
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.SubscriptionResource{
								SubscriberNode: "n2",
								ProviderNode:   "n1",
							}),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1)),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:              "n1",
								PrimaryInstanceID: n1Instance1.InstanceID(),
								InstanceIDs:       []string{n1Instance1.InstanceID()},
							}),
						},
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:              "n2",
								PrimaryInstanceID: n2Instance1.InstanceID(),
								InstanceIDs:       []string{n2Instance1.InstanceID()},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance1.Resources[0],
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: n2Instance1.Resources[0],
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := operations.UpdateDatabase(
				tc.options,
				tc.start,
				tc.nodes,
			)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				require.Equal(t, len(tc.expected), len(out), "Plan different lengths: %s", asJSON(t, tc.expected, out))
				assertPlansEqual(t, tc.expected, out)
			}
		})
	}
}
