package operations_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
)

func TestPopulateNode(t *testing.T) {
	instance1 := makeInstance(t, "n1", 1)

	for _, tc := range []struct {
		name              string
		node              *operations.NodeResources
		existingNodeNames []string
		expected          *resource.State
		expectedErr       string
	}{
		{
			name: "populate new node in two node db",
			node: &operations.NodeResources{
				DatabaseName:      "test",
				NodeName:          "n2",
				SourceNode:        "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{instance1},
			},
			existingNodeNames: []string{"n1"},
			// Since there are no other nodes besides the new node and the
			// source node, this will just have the sync resources.
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n2",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
				},
				nil,
			),
		},
		{
			name: "populate new node in three node db",
			node: &operations.NodeResources{
				DatabaseName:      "test",
				NodeName:          "n3",
				SourceNode:        "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{instance1},
			},
			existingNodeNames: []string{"n1", "n2"},
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n3",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n3",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n2",
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								"test",
								"n2",
								"n3",
							),
						},
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1", "test"),
						},
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.LagTrackerCommitTimestampResource{
						DatabaseName: "test",
						OriginNode:   "n2",
						ReceiverNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n3", "test"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n3",
					},
				},
				nil,
			),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := operations.PopulateNode(tc.node, tc.existingNodeNames)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}

func TestPopulateNodes(t *testing.T) {
	n1Instance1 := makeInstance(t, "n1", 1)
	n2Instance1 := makeInstance(t, "n2", 1)
	n3Instance1 := makeInstance(t, "n3", 1)
	n4Instance1 := makeInstance(t, "n4", 1)

	for _, tc := range []struct {
		name        string
		existing    []*operations.NodeResources
		new         []*operations.NodeResources
		expected    *resource.State
		expectedErr string
	}{
		{
			name: "one new node and one existing node",
			existing: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n2",
					SourceNode:        "n1",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			// This should look identical to the equivalent PopulateNode test
			// output.
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(n2Instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n2",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
				},
				nil,
			),
		},
		{
			name: "one new node and two existing nodes",
			existing: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					DatabaseName:      "test",
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n3",
					SourceNode:        "n1",
					PrimaryInstanceID: n3Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
			// This should look identical to the equivalent PopulateNode test
			// output.
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(n3Instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n3",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n3",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n2",
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								"test",
								"n2",
								"n3",
							),
						},
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1", "test"),
						},
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.LagTrackerCommitTimestampResource{
						DatabaseName: "test",
						OriginNode:   "n2",
						ReceiverNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n3", "test"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n3",
					},
				},
				nil,
			),
		},
		{
			name: "one new node and three existing nodes",
			existing: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					DatabaseName:      "test",
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
				{
					DatabaseName:      "test",
					NodeName:          "n3",
					PrimaryInstanceID: n3Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n4",
					SourceNode:        "n1",
					PrimaryInstanceID: n4Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n4Instance1},
				},
			},
			// Should have additional sync resources for each peer.
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(n4Instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n4",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n4",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n4",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n3",
						SubscriberNode: "n4",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n4",
						ProviderNode:   "n3",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   "test",
						SubscriberNode: "n4",
						ProviderNode:   "n2",
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   "test",
						SubscriberNode: "n4",
						ProviderNode:   "n3",
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								"test",
								"n2",
								"n4",
							),
						},
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n3",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								"test",
								"n3",
								"n4",
							),
						},
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n3",
						SubscriberNode: "n1",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n4",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n4",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1", "test"),
							database.WaitForSyncEventResourceIdentifier("n3", "n1", "test"),
						},
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n4",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n4",
					},
					&database.LagTrackerCommitTimestampResource{
						DatabaseName: "test",
						OriginNode:   "n2",
						ReceiverNode: "n4",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n4", "test"),
						},
					},
					&database.LagTrackerCommitTimestampResource{
						DatabaseName: "test",
						OriginNode:   "n3",
						ReceiverNode: "n4",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n4", "test"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						DatabaseName:   "test",
						ProviderNode:   "n2",
						SubscriberNode: "n4",
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						DatabaseName:   "test",
						ProviderNode:   "n3",
						SubscriberNode: "n4",
					},
				},
				nil,
			),
		},
		{
			name: "two new nodes and one existing node",
			existing: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					DatabaseName:      "test",
					NodeName:          "n2",
					SourceNode:        "n1",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
				{
					DatabaseName:      "test",
					NodeName:          "n3",
					SourceNode:        "n1",
					PrimaryInstanceID: n3Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
			// Should only have sync resources
			expected: makeState(t,
				[]resource.Resource{
					makeMonitorResource(n2Instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n2",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					makeMonitorResource(n3Instance1),
					&database.PostgresDatabaseResource{
						NodeName:     "n3",
						DatabaseName: "test",
					},
					&database.ReplicationSlotResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.SubscriptionResource{
						DatabaseName:   "test",
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.WaitForSyncEventResource{
						DatabaseName:   "test",
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
				},
				nil,
			),
		},
		{
			name: "no source node",
			existing: []*operations.NodeResources{
				{
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			// Shouldn't return a state if no nodes need to be populated.
			expected: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := operations.PopulateNodes(tc.existing, tc.new)
			if tc.expectedErr != "" {
				assert.Nil(t, out)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}
