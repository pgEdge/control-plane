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
