package operations

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// PopulateNode returns a diff that adds resources to sync the given node with
// its source node.
func PopulateNode(node *NodeResources, existingNodeNames []string) (*resource.State, error) {
	dbName := node.InstanceResources[0].DatabaseName()
	populate := resource.NewState()
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
			database.WaitForSyncEventResourceIdentifier(peer, node.SourceNode),
		)
	}

	err := addSyncResources(
		populate,
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
		&database.SubscriptionResource{
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
			ProviderNode:   peerNode,
			SubscriberNode: sourceNode,
		},
		// After the new node has caught up to the source node, we advance the
		// replication slots we created earlier.
		&database.LagTrackerCommitTimestampResource{
			OriginNode:   peerNode,
			ReceiverNode: newNode,
			ExtraDependencies: []resource.Identifier{
				database.WaitForSyncEventResourceIdentifier(
					sourceNode,
					newNode,
				),
			},
		},
		&database.ReplicationSlotAdvanceFromCTSResource{
			ProviderNode:   peerNode,
			SubscriberNode: newNode,
		},
	)
}

func addSyncResources(
	state *resource.State,
	peerWaitForSync []resource.Identifier,
	sourceNode string,
	newNode string,
) error {
	return state.AddResource(
		&database.SubscriptionResource{
			SubscriberNode:    newNode,
			ProviderNode:      sourceNode,
			SyncStructure:     true,
			SyncData:          true,
			ExtraDependencies: peerWaitForSync,
		},
		&database.SyncEventResource{
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
			ExtraDependencies: []resource.Identifier{
				database.SubscriptionResourceIdentifier(
					sourceNode,
					newNode,
				),
			},
		},
		&database.WaitForSyncEventResource{
			ProviderNode:   sourceNode,
			SubscriberNode: newNode,
		},
	)
}
