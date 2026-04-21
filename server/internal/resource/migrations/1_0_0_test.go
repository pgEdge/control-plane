package migrations_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v0_0_0"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestVersion_1_0_0(t *testing.T) {
	golden := &testutils.GoldenTest[*resource.State]{
		Compare: func(t testing.TB, expected, actual *resource.State) {
			// The json.RawValue ends up indented in our actual, so we'll round
			// trip the actual value to get the same indentation.
			raw, err := json.MarshalIndent(actual, "", "  ")
			require.NoError(t, err)

			var roundTrippedActual *resource.State
			require.NoError(t, json.Unmarshal(raw, &roundTrippedActual))

			require.Equal(t, expected, roundTrippedActual)
		},
	}
	for _, tc := range []struct {
		name string
		in   []*resource.ResourceData
	}{
		{
			name: "single node with replicas",
			in: []*resource.ResourceData{
				v0_0_0_node(t, "n1", "instance-1", "instance-2", "instance-3"),
				v0_0_0_instance(t, "instance-1", "host-1", "n1"),
				v0_0_0_instance(t, "instance-2", "host-2", "n1"),
				v0_0_0_instance(t, "instance-3", "host-3", "n1"),
			},
		},
		{
			name: "three nodes",
			in: []*resource.ResourceData{
				v0_0_0_node(t, "n1", "instance-1"),
				v0_0_0_node(t, "n2", "instance-2"),
				v0_0_0_node(t, "n3", "instance-3"),
				v0_0_0_instance(t, "instance-1", "host-1", "n1"),
				v0_0_0_instance(t, "instance-2", "host-2", "n2"),
				v0_0_0_instance(t, "instance-3", "host-3", "n3"),
				v0_0_0_replicationSlotResource(t, "n1", "n2"),
				v0_0_0_replicationSlotResource(t, "n1", "n3"),
				v0_0_0_replicationSlotResource(t, "n2", "n1"),
				v0_0_0_replicationSlotResource(t, "n2", "n3"),
				v0_0_0_replicationSlotResource(t, "n3", "n1"),
				v0_0_0_replicationSlotResource(t, "n3", "n2"),
				v0_0_0_subscriptionResource(t, "n1", "n2"),
				v0_0_0_subscriptionResource(t, "n1", "n3"),
				v0_0_0_subscriptionResource(t, "n2", "n1"),
				v0_0_0_subscriptionResource(t, "n2", "n3"),
				v0_0_0_subscriptionResource(t, "n3", "n1"),
				v0_0_0_subscriptionResource(t, "n3", "n2"),
			},
		},
		{
			// Databases created before v0.7.0 will not have replication slot
			// resources.
			name: "three nodes without slots",
			in: []*resource.ResourceData{
				v0_0_0_node(t, "n1", "instance-1"),
				v0_0_0_node(t, "n2", "instance-2"),
				v0_0_0_node(t, "n3", "instance-3"),
				v0_0_0_instance(t, "instance-1", "host-1", "n1"),
				v0_0_0_instance(t, "instance-2", "host-2", "n2"),
				v0_0_0_instance(t, "instance-3", "host-3", "n3"),
				v0_0_0_subscriptionResource(t, "n1", "n2"),
				v0_0_0_subscriptionResource(t, "n1", "n3"),
				v0_0_0_subscriptionResource(t, "n2", "n1"),
				v0_0_0_subscriptionResource(t, "n2", "n3"),
				v0_0_0_subscriptionResource(t, "n3", "n1"),
				v0_0_0_subscriptionResource(t, "n3", "n2"),
			},
		},
		{
			// This is what it would look like if we were to migrate a state
			// while it's partway through an "add node" operation
			name: "populate n3 with n1 source",
			in: []*resource.ResourceData{
				v0_0_0_node(t, "n1", "instance-1"),
				v0_0_0_node(t, "n2", "instance-2"),
				v0_0_0_node(t, "n3", "instance-3"),
				v0_0_0_instance(t, "instance-1", "host-1", "n1"),
				v0_0_0_instance(t, "instance-2", "host-2", "n2"),
				v0_0_0_instance(t, "instance-3", "host-3", "n3"),
				v0_0_0_replicationSlotResource(t, "n1", "n2"),
				v0_0_0_replicationSlotResource(t, "n2", "n1"),
				v0_0_0_subscriptionResource(t, "n1", "n2"),
				v0_0_0_subscriptionResource(t, "n2", "n1"),
				// peer resources
				v0_0_0_replicationSlotResource(t, "n2", "n3"),
				v0_0_0_subscriptionDisabledResource(t, "n2", "n3"),
				v0_0_0_replicationSlotCreateResource(t, "n2", "n3"),
				v0_0_0_peerSyncEventResource(t, "n2", "n1", "n3"),
				v0_0_0_waitForSyncResource(t, "n2", "n1"),
				v0_0_0_lagTracker(t, "n1", "n2", "n3"),
				v0_0_0_replicationSlotAdvanceResource(t, "n2", "n3"),
				// sync resources
				v0_0_0_replicationSlotResource(t, "n1", "n3"),
				v0_0_0_subscriptionSyncResource(t, "n1", "n3", "n2"),
				v0_0_0_sourceSyncEventResource(t, "n1", "n3"),
				v0_0_0_waitForSyncResource(t, "n1", "n3"),
			},
		},
		{
			name: "with restore config",
			in: []*resource.ResourceData{
				v0_0_0_node(t, "n1", "instance-1"),
				{
					Executor:        resource.HostExecutor("host-1"),
					Identifier:      v0_0_0.InstanceResourceIdentifier("instance-1"),
					ResourceVersion: "1",
					DiffIgnore: []string{
						"/primary_instance_id",
						"/connection_info",
					},
					Attributes: mustJSON(t, map[string]any{
						"spec": map[string]any{
							"database_id":   "database-1",
							"instance_id":   "instance-1",
							"database_name": "test",
							"host_id":       "host-1",
							"node_name":     "n1",
							"database_users": []map[string]any{
								{
									"username": "admin",
									"db_owner": true,
								},
							},
							"restore_config": map[string]any{
								"source_database_id":   "database-0",
								"source_node_name":     "n3",
								"source_database_name": "prod",
							},
						},
					}),
				},
			},
		},
		{
			name: "empty",
		},
		{
			name: "no nodes",
			in: []*resource.ResourceData{
				v0_0_0_instance(t, "instance-1", "host-1", "n1"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := resource.NewState()
			state.Add(tc.in...)

			migration := &migrations.Version_1_0_0{}
			migration.Run(state)

			golden.Run(t, state, update)

			// Validate that the dependencies are correct.
			_, err := state.PlanRefresh()
			require.NoError(t, err)

			_, err = state.PlanAll(resource.PlanOptions{}, state)
			require.NoError(t, err)
		})
	}
}

func v0_0_0_node(t testing.TB, nodeName string, instanceIDs ...string) *resource.ResourceData {
	deps := make([]resource.Identifier, len(instanceIDs))
	for i, id := range instanceIDs {
		deps[i] = v0_0_0.InstanceResourceIdentifier(id)
	}

	return &resource.ResourceData{
		Executor:        resource.AnyExecutor(),
		Identifier:      v0_0_0.NodeResourceIdentifier(nodeName),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.NodeResource{
			Name:              nodeName,
			InstanceIDs:       instanceIDs,
			PrimaryInstanceID: instanceIDs[0],
		}),
		Dependencies: deps,
	}
}

func v0_0_0_instance(t testing.TB, instanceID, hostID, nodeName string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.HostExecutor(hostID),
		Identifier:      v0_0_0.InstanceResourceIdentifier(instanceID),
		ResourceVersion: "1",
		DiffIgnore: []string{
			"/primary_instance_id",
			"/connection_info",
		},
		// Using a map here because we only use a subset of instance properties
		// and the inline structs are pretty unpleasant to use.
		Attributes: mustJSON(t, map[string]any{
			"spec": map[string]any{
				"database_id":   "database-1",
				"instance_id":   instanceID,
				"database_name": "test",
				"host_id":       hostID,
				"node_name":     nodeName,
				"database_users": []map[string]any{
					{
						"username": "admin",
						"db_owner": true,
					},
				},
			},
		}),
	}
}

func v0_0_0_replicationSlotResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(providerNode),
		Identifier:      v0_0_0.ReplicationSlotResourceIdentifier(providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.ReplicationSlotCreateResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(providerNode),
		},
	}
}

// This is the normal subscription resource
func v0_0_0_subscriptionResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(subscriberNode),
		Identifier:      v0_0_0.SubscriptionResourceIdentifier(providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.SubscriptionResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(subscriberNode),
			v0_0_0.NodeResourceIdentifier(providerNode),
			v0_0_0.ReplicationSlotResourceIdentifier(providerNode, subscriberNode),
		},
	}
}

// This is a disabled subscription we make during "add node"
func v0_0_0_subscriptionDisabledResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(subscriberNode),
		Identifier:      v0_0_0.SubscriptionResourceIdentifier(providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.SubscriptionResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
			Disabled:       true,
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(subscriberNode),
			v0_0_0.NodeResourceIdentifier(providerNode),
			v0_0_0.ReplicationSlotResourceIdentifier(providerNode, subscriberNode),
		},
	}
}

// This is a subscription with sync enabled that we make during "add node"
func v0_0_0_subscriptionSyncResource(t testing.TB, sourceNode, newNode string, peerNodes ...string) *resource.ResourceData {
	extraDepIdentifiers := make([]resource.Identifier, len(peerNodes))
	extraDeps := make([]struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}, len(peerNodes))
	for i, peer := range peerNodes {
		extraDepIdentifiers[i] = v0_0_0.WaitForSyncEventResourceIdentifier(peer, sourceNode)
		extraDeps[i] = struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}{
			ID:   extraDepIdentifiers[i].ID,
			Type: extraDepIdentifiers[i].Type.String(),
		}
	}
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(newNode),
		Identifier:      v0_0_0.SubscriptionResourceIdentifier(sourceNode, newNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.SubscriptionResource{
			ProviderNode:      sourceNode,
			SubscriberNode:    newNode,
			SyncStructure:     true,
			SyncData:          true,
			ExtraDependencies: extraDeps,
		}),
		Dependencies: slices.Concat(
			[]resource.Identifier{
				v0_0_0.NodeResourceIdentifier(newNode),
				v0_0_0.NodeResourceIdentifier(sourceNode),
				v0_0_0.ReplicationSlotResourceIdentifier(sourceNode, newNode),
			},
			extraDepIdentifiers,
		),
	}
}

func v0_0_0_lagTracker(t testing.TB, sourceNode, originNode, receiverNode string) *resource.ResourceData {
	waitForSyncID := v0_0_0.WaitForSyncEventResourceIdentifier(sourceNode, receiverNode)
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(receiverNode),
		Identifier:      v0_0_0.LagTrackerCommitTSIdentifier(originNode, receiverNode),
		ResourceVersion: "1",
		// Using a map here because we only use a subset of instance properties
		// and the inline structs are pretty unpleasant to use.
		Attributes: mustJSON(t, v0_0_0.LagTrackerCommitTimestampResource{
			OriginNode:   originNode,
			ReceiverNode: receiverNode,
			ExtraDependencies: []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				{
					ID:   waitForSyncID.ID,
					Type: waitForSyncID.Type.String(),
				},
			},
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(receiverNode),
			waitForSyncID,
		},
	}
}

func v0_0_0_replicationSlotAdvanceResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(providerNode),
		Identifier:      v0_0_0.ReplicationSlotAdvanceFromCTSResourceIdentifier(providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.ReplicationSlotAdvanceFromCTSResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(providerNode),
			v0_0_0.LagTrackerCommitTSIdentifier(providerNode, subscriberNode),
		},
	}
}

func v0_0_0_peerSyncEventResource(t testing.TB, peerNode, sourceNode, newNode string) *resource.ResourceData {
	replicationSlotIdentifier := v0_0_0.ReplicationSlotCreateResourceIdentifier("test", peerNode, newNode)
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(peerNode),
		Identifier:      v0_0_0.SyncEventResourceIdentifier(peerNode, sourceNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.SyncEventResource{
			ProviderNode:   peerNode,
			SubscriberNode: sourceNode,
			ExtraDependencies: []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				{
					ID:   replicationSlotIdentifier.ID,
					Type: replicationSlotIdentifier.Type.String(),
				},
			},
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(peerNode),
			v0_0_0.SubscriptionResourceIdentifier(peerNode, sourceNode),
			replicationSlotIdentifier,
		},
	}
}

func v0_0_0_sourceSyncEventResource(t testing.TB, sourceNode, newNode string) *resource.ResourceData {
	// This subscription was duplicated in the old version. We deduplicate it
	// in the migration.
	subIdentifier := v0_0_0.SubscriptionResourceIdentifier(sourceNode, newNode)
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(sourceNode),
		Identifier:      v0_0_0.SyncEventResourceIdentifier(sourceNode, newNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.SyncEventResource{
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
			ExtraDependencies: []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				{
					ID:   subIdentifier.ID,
					Type: subIdentifier.Type.String(),
				},
			},
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.NodeResourceIdentifier(sourceNode),
			v0_0_0.SubscriptionResourceIdentifier(sourceNode, newNode),
			subIdentifier,
		},
	}
}

func v0_0_0_waitForSyncResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(subscriberNode),
		Identifier:      v0_0_0.WaitForSyncEventResourceIdentifier(providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.WaitForSyncEventResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.SyncEventResourceIdentifier(providerNode, subscriberNode),
		},
	}
}

func v0_0_0_replicationSlotCreateResource(t testing.TB, providerNode, subscriberNode string) *resource.ResourceData {
	return &resource.ResourceData{
		Executor:        resource.PrimaryExecutor(providerNode),
		Identifier:      v0_0_0.ReplicationSlotCreateResourceIdentifier("test", providerNode, subscriberNode),
		ResourceVersion: "1",
		Attributes: mustJSON(t, v0_0_0.ReplicationSlotCreateResource{
			ProviderNode:   providerNode,
			SubscriberNode: subscriberNode,
			DatabaseName:   "test",
		}),
		Dependencies: []resource.Identifier{
			v0_0_0.SyncEventResourceIdentifier(providerNode, subscriberNode),
		},
	}
}

func mustJSON(t testing.TB, data any) json.RawMessage {
	out, err := json.Marshal(data)
	require.NoError(t, err)

	return out
}
