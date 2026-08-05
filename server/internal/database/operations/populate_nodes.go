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

// EnablePeerSubscriptions returns a diff that enables the peer subscriptions
// addPeerResources creates disabled. It must be applied as a separate, later
// phase than PopulateNodes' own state.
//
// addPeerResources creates each peer->new-node subscription disabled so the
// peer-catchup chain (SyncEvent -> WaitForSyncEvent -> PeerCatchup ->
// LagTracker -> ReplicationSlotAdvanceFromCTS -> ReplicationOriginAdvance)
// can run without a live subscriber racing that setup. But a single
// resource.State can only express one desired value per identifier, so that
// same state can never also declare "now enable it" — nothing else in the
// codebase ever does, which left these subscriptions permanently disabled.
// This returns a second state that re-declares the same SubscriptionResource
// identifiers with Disabled: false; applied after the populate phase is
// fully persisted, its diff sees disabled->enabled and calls Update, which
// is what actually flips sub_enabled in spock.subscription.
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
			err := enable.AddResource(&database.SubscriptionResource{
				DatabaseName:   dbName,
				SubscriberNode: node.NodeName,
				ProviderNode:   peer,
				Disabled:       false,
				ExtraDependencies: []resource.Identifier{
					database.ReplicationOriginAdvanceResourceIdentifier(peer, node.NodeName, dbName),
				},
			})
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
