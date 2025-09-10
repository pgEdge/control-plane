package operations_test

import (
	"slices"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
)

func TestRestoreDatabase(t *testing.T) {
	n1Instance1 := makeInstance(t, "n1", 1)
	n1Instance2 := makeInstance(t, "n1", 2)
	n2Instance1 := makeInstance(t, "n2", 1)
	n1Instance1WithRestore := makeInstance(t, "n1", 1,
		makeOrchestratorResource(t, "n1", 1, 1),
		makeRestoreResource(t, "n1", 1, 1),
	)
	n2Instance1WithRestore := makeInstance(t, "n2", 1,
		makeOrchestratorResource(t, "n2", 1, 1),
		makeRestoreResource(t, "n2", 1, 1),
	)

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
	twoNodeStateWithReplica := makeState(t,
		[]resource.Resource{
			n1Instance1.Instance,
			makeMonitorResource(n1Instance1),
			n1Instance2.Instance,
			makeMonitorResource(n1Instance2),
			&database.NodeResource{
				Name:              "n1",
				PrimaryInstanceID: n1Instance1.InstanceID(),
				InstanceIDs: []string{
					n1Instance1.InstanceID(),
					n1Instance2.InstanceID(),
				},
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
			n1Instance2.Resources,
			n2Instance1.Resources,
		),
	)

	for _, tc := range []struct {
		name        string
		start       *resource.State
		nodes       []*operations.NodeResources
		targets     []*operations.NodeRestoreResources
		expected    []resource.Plan
		expectedErr string
	}{
		{
			name:  "single node restore",
			start: singleNodeState,
			targets: []*operations.NodeRestoreResources{
				{
					NodeName:        "n1",
					PrimaryInstance: n1Instance1,
					RestoreInstance: n1Instance1WithRestore,
				},
			},
			// The instance is updated after it's created because we've removed
			// one of its dependencies: the restore resource. In practice, this
			// will check to see if updates are needed, but will otherwise be a
			// no-op.
			expected: []resource.Plan{
				{
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
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithRestore.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1WithRestore.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithRestore)),
						},
					},
				},
				{
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
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n1",
								InstanceIDs: []string{n1Instance1.InstanceID()},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance1WithRestore.Resources[1],
						},
					},
				},
			},
		},
		{
			name:  "single node restore in two-node db",
			start: twoNodeState,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			targets: []*operations.NodeRestoreResources{
				{
					NodeName:        "n1",
					PrimaryInstance: n1Instance1,
					RestoreInstance: n1Instance1WithRestore,
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
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
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
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithRestore.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1WithRestore.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithRestore)),
						},
					},
				},
				{
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
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:        "n1",
								InstanceIDs: []string{n1Instance1.InstanceID()},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance1WithRestore.Resources[1],
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
			name:  "single node restore in two-node db with replica",
			start: twoNodeStateWithReplica,
			nodes: []*operations.NodeResources{
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			targets: []*operations.NodeRestoreResources{
				{
					NodeName:         "n1",
					PrimaryInstance:  n1Instance1,
					RestoreInstance:  n1Instance1WithRestore,
					ReplicaInstances: []*database.InstanceResources{n1Instance2},
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
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance2)),
						},
					},
					{
						{
							Type: resource.EventTypeDelete,
							Resource: makeResourceData(t, &database.NodeResource{
								Name:              "n1",
								PrimaryInstanceID: n1Instance1.InstanceID(),
								InstanceIDs: []string{
									n1Instance1.InstanceID(),
									n1Instance2.InstanceID(),
								},
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
							Resource: makeResourceData(t, n1Instance2.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance2.Resources[0],
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithRestore.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1WithRestore.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithRestore)),
						},
					},
				},
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
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance2)),
						},
						{
							Type: resource.EventTypeCreate,
							Resource: makeResourceData(t, &database.NodeResource{
								Name: "n1",
								InstanceIDs: []string{
									n1Instance1.InstanceID(),
									n1Instance2.InstanceID(),
								},
							}),
						},
					},
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance1WithRestore.Resources[1],
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
			name:  "restore all nodes in two-node db",
			start: twoNodeState,
			targets: []*operations.NodeRestoreResources{
				{
					NodeName:        "n1",
					PrimaryInstance: n1Instance1,
					RestoreInstance: n1Instance1WithRestore,
				},
				{
					NodeName:        "n2",
					PrimaryInstance: n2Instance1,
					RestoreInstance: n2Instance1WithRestore,
				},
			},
			// The nodes are restored simultaneously.
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
				},
				{
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: n1Instance1WithRestore.Resources[1],
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: n2Instance1WithRestore.Resources[1],
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n1Instance1WithRestore.Instance),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, n2Instance1WithRestore.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1WithRestore)),
						},
						{
							Type:     resource.EventTypeCreate,
							Resource: makeResourceData(t, makeMonitorResource(n2Instance1WithRestore)),
						},
					},
				},
				{
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n1Instance1.Instance),
						},
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, n2Instance1.Instance),
						},
					},
					{
						{
							Type:     resource.EventTypeUpdate,
							Resource: makeResourceData(t, makeMonitorResource(n1Instance1)),
						},
						{
							Type:     resource.EventTypeUpdate,
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
					{
						{
							Type:     resource.EventTypeDelete,
							Resource: n1Instance1WithRestore.Resources[1],
						},
						{
							Type:     resource.EventTypeDelete,
							Resource: n2Instance1WithRestore.Resources[1],
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := operations.RestoreDatabase(
				tc.start,
				tc.nodes,
				tc.targets,
			)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assertPlansEqual(t, tc.expected, out)
			}
		})
	}
}
