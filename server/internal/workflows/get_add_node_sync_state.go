package workflows

import (
	"fmt"
	"log/slog"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type GetAddNodeSyncStateInput struct {
	Spec           *database.Spec
	TargetNodeName *string
	SourceNodeName *string
}

type GetAddNodeSyncStateOutput struct {
	State *resource.State `json:"state"`
}

func (w *Workflows) ExecuteGetAddNodeSyncState(
	ctx workflow.Context,
	input *GetAddNodeSyncStateInput,
) workflow.Future[*GetAddNodeSyncStateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*GetAddNodeSyncStateOutput](ctx, options, w.GetAddNodeSyncState, input)
}

func (w *Workflows) GetAddNodeSyncState(ctx workflow.Context, input *GetAddNodeSyncStateInput) (*GetAddNodeSyncStateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)

	logger.Info("getting add node sync state")

	// Get node instances from the database spec
	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	// State machine to collect resources
	state := resource.NewState()

	var instanceFutures []workflow.Future[*activities.GetInstanceResourcesOutput]

	// Track 3 categories of subscriptions
	// - disabledSubscriptions: subscriptions created but not enabled yet
	// - syncSubscriptions: subscriptions for source->target with sync enabled
	// - normalSubscriptions: all existing subscriptions between peers
	disabledSubscriptions := make([]*database.SubscriptionResource, 0)
	syncSubscriptions := make([]*database.SubscriptionResource, 0)
	normalSubscriptions := make([]*database.SubscriptionResource, 0)

	// Keep track of the target node (new node being added)
	var targetNode *database.NodeInstances

	for i, nodeInstance := range nodeInstances {
		// Collect instance-level resources for monitoring
		var result *GetAddNodeSyncStateOutput
		result, err := w.collectInstanceResources(ctx, nodeInstance, &instanceFutures, state)
		if err != nil {
			return result, err
		}

		// Identify the target node (new node being added)
		nodeIsTarget := *input.TargetNodeName == nodeInstance.NodeName
		if nodeIsTarget {
			targetNode = nodeInstance
		}
		for j, peer := range nodeInstances {
			if i == j {
				continue
			}

			peerIsSource := peer.NodeName == *input.SourceNodeName

			var sub *database.SubscriptionResource

			// Skip adding subscriptions if the target node is the provider
			if peer.NodeName == *input.TargetNodeName {
				continue
			}

			if nodeIsTarget && !peerIsSource {
				// Subscription to peer nodes from the target node are disabled initially
				logger.Info("adding disabled subscription", "subscriber", nodeInstance.NodeName, "provider", peer.NodeName)
				sub = database.NewSubscriptionResource(nodeInstance, peer, false, false, false)
				disabledSubscriptions = append(disabledSubscriptions, sub)

				// Create replication slot for disabled subscription
				logger.Info("creating replication slot", "subscriber", nodeInstance.NodeName, "provider", peer.NodeName)
				replicationSlot := database.NewReplicationSlotCreateResource(
					input.Spec.DatabaseName,
					peer.NodeName,
					nodeInstance.NodeName,
				)
				_ = state.AddResource(replicationSlot)
				// Introduced a dependency to guarantee that when a subscription is disabled, the corresponding replication slot is also properly managed.
				sub.AddDependentResource(replicationSlot.Identifier())
			} else if nodeIsTarget && peerIsSource {
				// Subscription from source to target has sync_data, sync_structure, and enabled true
				logger.Info("adding active subscription with data sync", "subscriber", peer.NodeName, "provider", nodeInstance.NodeName)
				sub = database.NewSubscriptionResource(nodeInstance, peer, true, true, true)
				syncSubscriptions = append(syncSubscriptions, sub)
			} else {
				// Existing peer->peer subscriptions
				logger.Info("adding active subscription for existing nodes", "subscriber", peer.NodeName, "provider", nodeInstance.NodeName)

				sub = database.NewSubscriptionResource(nodeInstance, peer, true, false, false)
				normalSubscriptions = append(normalSubscriptions, sub)
			}
		}
	}

	// Add confirm sync between target node and source node
	syncSubWaitForSyncEvent := buildSyncEvents(input, nodeInstances, state)

	// Make all sync subscriptions dependent on disabled subscriptions
	for _, syncSub := range syncSubscriptions {
		for _, disabledSub := range disabledSubscriptions {
			syncSub.AddDependentResource(disabledSub.Identifier())
		}
	}

	// Lag tracker + slot advance
	replicationSlotAdvance := addLagTrackerAndSlotAdvance(input, syncSubWaitForSyncEvent, state)

	// Add subscription resources (disabled, sync, normal)
	addAllSubscriptions(disabledSubscriptions, state, syncSubscriptions, normalSubscriptions)

	// Back-subscriptions: enable new node (target) as provider for peers
	addBackSubscriptions(nodeInstances, input, logger, targetNode, replicationSlotAdvance, state)

	// Enable previously disabled subscriptions
	enableDisabledSubscriptions(disabledSubscriptions, logger, state)

	// Collect instance resources
	result, err := resolveInstanceFutures(ctx, instanceFutures, state)
	if err != nil {
		return result, err
	}

	logger.Info("successfully got add node sync state")

	return &GetAddNodeSyncStateOutput{
		State: state,
	}, nil
}

func (w *Workflows) collectInstanceResources(ctx workflow.Context,
	nodeInstance *database.NodeInstances,
	instanceFutures *[]workflow.Future[*activities.GetInstanceResourcesOutput],
	state *resource.State) (*GetAddNodeSyncStateOutput, error) {
	var instanceIDs []string
	for _, instance := range nodeInstance.Instances {
		instanceIDs = append(instanceIDs, instance.InstanceID)
		instanceFuture := w.Activities.ExecuteGetInstanceResources(ctx, &activities.GetInstanceResourcesInput{
			Spec: instance,
		})
		*instanceFutures = append(*instanceFutures, instanceFuture)

		// Instance monitor resource to track health
		err := state.AddResource(&monitor.InstanceMonitorResource{
			DatabaseID:   instance.DatabaseID,
			InstanceID:   instance.InstanceID,
			HostID:       instance.HostID,
			DatabaseName: instance.DatabaseName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add instance monitor resource to state: %w", err)
		}
	}
	err := state.AddResource(&database.NodeResource{
		ClusterID:   w.Config.ClusterID,
		Name:        nodeInstance.NodeName,
		InstanceIDs: instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add node resource to state: %w", err)
	}
	return nil, nil
}

func buildSyncEvents(input *GetAddNodeSyncStateInput,
	nodeInstances []*database.NodeInstances,
	state *resource.State) *database.WaitForSyncEventResource {
	syncSubSyncEvent := database.NewSyncEventResource(*input.TargetNodeName, *input.SourceNodeName)
	syncSubWaitForSyncEvent := database.NewWaitForSyncEventResource(*input.TargetNodeName, *input.SourceNodeName)

	// Add confirm sync between source node and all peers (not the source)
	for _, peer := range nodeInstances {
		if peer.NodeName == *input.SourceNodeName || peer.NodeName == *input.TargetNodeName {
			continue
		}

		peerSubSyncEvent := database.NewSyncEventResource(*input.SourceNodeName, peer.NodeName)
		peerSubWaitForSyncEvent := database.NewWaitForSyncEventResource(*input.SourceNodeName, peer.NodeName)

		// Add as a dependency on sync subscription wait_for_sync_event
		syncSubSyncEvent.AddDependentResource(peerSubWaitForSyncEvent.Identifier())
		syncSubWaitForSyncEvent.AddDependentResource(peerSubWaitForSyncEvent.Identifier())

		_ = state.AddResource(peerSubSyncEvent)
		_ = state.AddResource(peerSubWaitForSyncEvent)
	}

	_ = state.AddResource(syncSubSyncEvent)
	_ = state.AddResource(syncSubWaitForSyncEvent)
	return syncSubWaitForSyncEvent
}

func addLagTrackerAndSlotAdvance(input *GetAddNodeSyncStateInput,
	syncSubWaitForSyncEvent *database.WaitForSyncEventResource,
	state *resource.State) *database.ReplicationSlotAdvanceFromCTSResource {
	lagTracker := database.NewLagTrackerCommitTimestampResource(*input.SourceNodeName, *input.TargetNodeName)
	lagTracker.AddDependentResource(syncSubWaitForSyncEvent.Identifier())
	_ = state.AddResource(lagTracker)

	// Create replication slot on provider before advancing
	replicationSlot := database.NewReplicationSlotCreateResource(
		input.Spec.DatabaseName,
		*input.SourceNodeName, // provider
		*input.TargetNodeName, // subscriber
	)
	_ = state.AddResource(replicationSlot)

	// Advance replication slot using commit_ts from lag tracker
	replicationSlotAdvance := database.NewReplicationSlotAdvanceFromCTSResource(
		input.Spec.DatabaseName,
		*input.SourceNodeName,
		*input.TargetNodeName,
	)
	replicationSlotAdvance.AddDependentResource(lagTracker.Identifier())
	replicationSlotAdvance.AddDependentResource(replicationSlot.Identifier())
	_ = state.AddResource(replicationSlotAdvance)
	return replicationSlotAdvance
}

func addAllSubscriptions(
	disabledSubscriptions []*database.SubscriptionResource,
	state *resource.State,
	syncSubscriptions []*database.SubscriptionResource,
	normalSubscriptions []*database.SubscriptionResource) {
	for _, sub := range disabledSubscriptions {
		_ = state.AddResource(sub)
	}
	for _, sub := range syncSubscriptions {
		_ = state.AddResource(sub)
	}
	for _, sub := range normalSubscriptions {
		_ = state.AddResource(sub)
	}
}

func addBackSubscriptions(
	nodeInstances []*database.NodeInstances,
	input *GetAddNodeSyncStateInput,
	logger *slog.Logger,
	targetNode *database.NodeInstances,
	replicationSlotAdvance *database.ReplicationSlotAdvanceFromCTSResource,
	state *resource.State) {
	for _, peer := range nodeInstances {
		// skip target node
		if peer.NodeName == *input.TargetNodeName {
			continue
		}

		logger.Info("adding back subscription", "subscriber", peer.NodeName, "provider", *input.TargetNodeName)

		sub := database.NewSubscriptionResource(peer, targetNode, true, false, false)
		sub.AddDependentResource(replicationSlotAdvance.Identifier())
		_ = state.AddResource(sub)
	}
}

func enableDisabledSubscriptions(disabledSubscriptions []*database.SubscriptionResource, logger *slog.Logger, state *resource.State) {
	for _, disabledSub := range disabledSubscriptions {
		// flip Enabled -> true, keep same provider/subscriber
		disabledSub.Enable(true)

		logger.Info("enabling disabled subscription",
			"subscriber", disabledSub.SubscriberNode,
			"provider", disabledSub.ProviderNode)

		_ = state.AddResource(disabledSub)
	}
}

func resolveInstanceFutures(ctx workflow.Context,
	instanceFutures []workflow.Future[*activities.GetInstanceResourcesOutput],
	state *resource.State,
) (*GetAddNodeSyncStateOutput, error) {
	for _, instanceFuture := range instanceFutures {
		instanceOutput, err := instanceFuture.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get instance resources: %w", err)
		}
		err = state.AddResource(instanceOutput.Resources.Instance)
		if err != nil {
			return nil, fmt.Errorf("failed to add instance resource to state: %w", err)
		}
		for _, resource := range instanceOutput.Resources.Resources {
			state.Add(resource)
		}
	}
	return nil, nil
}
