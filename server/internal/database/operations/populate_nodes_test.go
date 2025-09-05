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
					&database.SubscriptionResource{
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n2",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n2"),
						},
					},
					&database.WaitForSyncEventResource{
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
				NodeName:          "n3",
				SourceNode:        "n1",
				PrimaryInstanceID: instance1.InstanceID(),
				InstanceResources: []*database.InstanceResources{instance1},
			},
			existingNodeNames: []string{"n1", "n2"},
			expected: makeState(t,
				[]resource.Resource{
					&database.SubscriptionResource{
						SubscriberNode: "n3",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   instance1.DatabaseName(),
						SubscriberNode: "n3",
						ProviderNode:   "n2",
					},
					&database.SyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								instance1.DatabaseName(),
								"n2",
								"n3",
							),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.SubscriptionResource{
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1"),
						},
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n3"),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.LagTrackerCommitTimestampResource{
						OriginNode:   "n2",
						ReceiverNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n3"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						ProviderNode:   "n2",
						SubscriberNode: "n3",
					},
				},
				nil,
			),
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
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
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
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
					&database.SubscriptionResource{
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n2",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n2"),
						},
					},
					&database.WaitForSyncEventResource{
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
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
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
					&database.SubscriptionResource{
						SubscriberNode: "n3",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   n3Instance1.DatabaseName(),
						SubscriberNode: "n3",
						ProviderNode:   "n2",
					},
					&database.SyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								n3Instance1.DatabaseName(),
								"n2",
								"n3",
							),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.SubscriptionResource{
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1"),
						},
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n3"),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n3",
					},
					&database.LagTrackerCommitTimestampResource{
						OriginNode:   "n2",
						ReceiverNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n3"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
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
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
				{
					NodeName:          "n2",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
				{
					NodeName:          "n3",
					PrimaryInstanceID: n3Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					NodeName:          "n4",
					SourceNode:        "n1",
					PrimaryInstanceID: n4Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n4Instance1},
				},
			},
			// Should have additional sync resources for each peer.
			expected: makeState(t,
				[]resource.Resource{
					&database.SubscriptionResource{
						SubscriberNode: "n4",
						ProviderNode:   "n2",
						Disabled:       true,
					},
					&database.SubscriptionResource{
						SubscriberNode: "n4",
						ProviderNode:   "n3",
						Disabled:       true,
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   n4Instance1.DatabaseName(),
						SubscriberNode: "n4",
						ProviderNode:   "n2",
					},
					&database.ReplicationSlotCreateResource{
						DatabaseName:   n4Instance1.DatabaseName(),
						SubscriberNode: "n4",
						ProviderNode:   "n3",
					},
					&database.SyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								n4Instance1.DatabaseName(),
								"n2",
								"n4",
							),
						},
					},
					&database.SyncEventResource{
						ProviderNode:   "n3",
						SubscriberNode: "n1",
						ExtraDependencies: []resource.Identifier{
							database.ReplicationSlotCreateResourceIdentifier(
								n4Instance1.DatabaseName(),
								"n3",
								"n4",
							),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n2",
						SubscriberNode: "n1",
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n3",
						SubscriberNode: "n1",
					},
					&database.SubscriptionResource{
						SubscriberNode: "n4",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n2", "n1"),
							database.WaitForSyncEventResourceIdentifier("n3", "n1"),
						},
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n4",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n4"),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n4",
					},
					&database.LagTrackerCommitTimestampResource{
						OriginNode:   "n2",
						ReceiverNode: "n4",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n4"),
						},
					},
					&database.LagTrackerCommitTimestampResource{
						OriginNode:   "n3",
						ReceiverNode: "n4",
						ExtraDependencies: []resource.Identifier{
							database.WaitForSyncEventResourceIdentifier("n1", "n4"),
						},
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
						ProviderNode:   "n2",
						SubscriberNode: "n4",
					},
					&database.ReplicationSlotAdvanceFromCTSResource{
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
					NodeName:          "n1",
					PrimaryInstanceID: n1Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n1Instance1},
				},
			},
			new: []*operations.NodeResources{
				{
					NodeName:          "n2",
					SourceNode:        "n1",
					PrimaryInstanceID: n2Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n2Instance1},
				},
				{
					NodeName:          "n3",
					SourceNode:        "n1",
					PrimaryInstanceID: n3Instance1.InstanceID(),
					InstanceResources: []*database.InstanceResources{n3Instance1},
				},
			},
			// Should only have sync resources
			expected: makeState(t,
				[]resource.Resource{
					&database.SubscriptionResource{
						SubscriberNode: "n2",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n2",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n2"),
						},
					},
					&database.WaitForSyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n2",
					},
					&database.SubscriptionResource{
						SubscriberNode: "n3",
						ProviderNode:   "n1",
						SyncStructure:  true,
						SyncData:       true,
					},
					&database.SyncEventResource{
						ProviderNode:   "n1",
						SubscriberNode: "n3",
						ExtraDependencies: []resource.Identifier{
							database.SubscriptionResourceIdentifier("n1", "n3"),
						},
					},
					&database.WaitForSyncEventResource{
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
		t.Run(t.Name(), func(t *testing.T) {
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
