package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// PopulateNode returns a diff that adds resources to sync the given node with
// its source node.
func PopulateNode(node *NodeResources, existingNodeNames []string) (*resource.State, error) {
	dbName := node.DatabaseName
	populate := resource.NewState()

	databaseState, err := node.databaseResourceState()
	if err != nil {
		return nil, err
	}
	populate.Merge(databaseState)

	var peerWaitForSync []resource.Identifier
	for _, peer := range existingNodeNames {
		if peer == node.NodeName || peer == node.SourceNode {
			continue
		}

		err := addPeerResources(
			populate,
			dbName,
			peer,
			node.SourceNode,
			node.NodeName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to add peer resources to 'populate' state: %w", err)
		}
		peerWaitForSync = append(
			peerWaitForSync,
			database.WaitForSyncEventResourceIdentifier(peer, node.SourceNode, dbName),
			database.PeerCatchupResourceIdentifier(node.SourceNode, peer, dbName),
		)
	}

	err = addSyncResources(
		populate,
		dbName,
		peerWaitForSync,
		node.SourceNode,
		node.NodeName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add sync resources to 'populate' state: %w", err)
	}

	return populate, nil
}

// PopulateNodes returns a diff that adds resources to sync the given nodes with
// their source nodes. The syncs are performed simultaneously.
func PopulateNodes(existing, new []*NodeResources) (*resource.State, error) {
	existingNodeNames := make([]string, len(existing))
	for i, n := range existing {
		existingNodeNames[i] = n.NodeName
	}

	var merged *resource.State
	for _, node := range new {
		if node.SourceNode == "" {
			continue
		}

		populate, err := PopulateNode(node, existingNodeNames)
		if err != nil {
			return nil, err
		}

		if merged == nil {
			merged = populate
		} else {
			merged.Merge(populate)
		}
	}

	return merged, nil
}

// EnablePeerSubscriptions returns a diff that re-enables the peer
// subscriptions addPeerResources creates disabled, and verifies each one
// actually starts replicating. It must be applied as a separate, later phase
// than PopulateNodes' own state.
//
// The re-enable here is not the only thing that flips these subscriptions
// back on: end.go's EndState() unconditionally redeclares every peer-pair
// SubscriptionResource as part of the final desired state of every
// create/update-database operation, which enables them regardless of what
// this phase does. So functionally this phase's enable is redundant with
// that later one — a no-op Update by the time end.go's phase runs.
//
// It's kept anyway because it's the anchor for
// VerifySubscriptionReplicatingResource below: that check needs a
// SubscriptionResource in the graph that is actually enabled by the time it
// runs, so we can fail loudly, right here, if a peer subscription never
// starts replicating — instead of only finding out much later (or not at
// all, since end.go's phase has no equivalent verification). Dropping the
// enable without relocating the verify step would leave the verify checking
// a subscription that's still deliberately disabled from the populate
// phase, so it would fail every time.
func EnablePeerSubscriptions(existing, new []*NodeResources) (*resource.State, error) {
	existingNodeNames := make([]string, len(existing))
	for i, n := range existing {
		existingNodeNames[i] = n.NodeName
	}

	enable := resource.NewState()
	for _, node := range new {
		if node.SourceNode == "" {
			continue
		}
		dbName := node.DatabaseName
		for _, peer := range existingNodeNames {
			if peer == node.NodeName || peer == node.SourceNode {
				continue
			}
			err := enable.AddResource(
				&database.SubscriptionResource{
					DatabaseName:   dbName,
					SubscriberNode: node.NodeName,
					ProviderNode:   peer,
					Disabled:       false,
					ExtraDependencies: []resource.Identifier{
						database.ReplicationOriginAdvanceResourceIdentifier(peer, node.NodeName, dbName),
					},
				},
				// Verify the enable actually took effect. Same phase, not a
				// separate one: this is a new resource type/identifier, not
				// a re-declaration of an existing one, so it can safely
				// depend on the SubscriptionResource declared just above
				// within this same state and run after it in the same
				// apply pass.
				&database.VerifySubscriptionReplicatingResource{
					DatabaseName:   dbName,
					SubscriberNode: node.NodeName,
					ProviderNode:   peer,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to add peer-enable resource to 'enable' state: %w", err)
			}
		}
	}

	return enable, nil
}

func addPeerResources(
	state *resource.State,
	dbName string,
	peerNode string,
	sourceNode string,
	newNode string,
) error {
	return state.AddResource(
		&database.ReplicationSlotResource{
			DatabaseName:   dbName,
			ProviderNode:   peerNode,
			SubscriberNode: newNode,
		},
		&database.SubscriptionResource{
			DatabaseName:   dbName,
			SubscriberNode: newNode,
			ProviderNode:   peerNode,
			Disabled:       true,
		},
		&database.ReplicationSlotCreateResource{
			DatabaseName:   dbName,
			SubscriberNode: newNode,
			ProviderNode:   peerNode,
		},
		&database.SyncEventResource{
			DatabaseName:   dbName,
			ProviderNode:   peerNode,
			SubscriberNode: sourceNode,
			ExtraDependencies: []resource.Identifier{
				database.ReplicationSlotCreateResourceIdentifier(
					dbName,
					peerNode,
					newNode,
				),
			},
		},
		&database.WaitForSyncEventResource{
			DatabaseName:   dbName,
			ProviderNode:   peerNode,
			SubscriberNode: sourceNode,
		},
		// Belt-and-suspenders: also wait using remote_lsn, which
		// tracks actual commit application rather than WAL receipt.
		&database.PeerCatchupResource{
			DatabaseName: dbName,
			SourceNode:   sourceNode,
			PeerNode:     peerNode,
		},
		// After the new node has caught up to the source node, we advance the
		// replication slots we created earlier.
		&database.LagTrackerCommitTimestampResource{
			DatabaseName: dbName,
			OriginNode:   peerNode,
			ReceiverNode: newNode,
			ExtraDependencies: []resource.Identifier{
				database.WaitForSyncEventResourceIdentifier(
					sourceNode,
					newNode,
					dbName,
				),
			},
		},
		&database.ReplicationSlotAdvanceFromCTSResource{
			DatabaseName:   dbName,
			ProviderNode:   peerNode,
			SubscriberNode: newNode,
		},
		// Origin advance runs on the subscriber's host; must be separate from
		// slot advance which runs on the provider's host.
		&database.ReplicationOriginAdvanceResource{
			DatabaseName:   dbName,
			ProviderNode:   peerNode,
			SubscriberNode: newNode,
		},
	)
}

func addSyncResources(
	state *resource.State,
	dbName string,
	peerWaitForSync []resource.Identifier,
	sourceNode string,
	newNode string,
) error {
	return state.AddResource(
		&database.ReplicationSlotResource{
			DatabaseName:   dbName,
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
		},
		&database.SubscriptionResource{
			DatabaseName:      dbName,
			SubscriberNode:    newNode,
			ProviderNode:      sourceNode,
			SyncStructure:     true,
			SyncData:          true,
			ExtraDependencies: peerWaitForSync,
		},
		&database.SyncEventResource{
			DatabaseName:   dbName,
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
		},
		&database.WaitForSyncEventResource{
			DatabaseName:   dbName,
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
		},
	)
}
