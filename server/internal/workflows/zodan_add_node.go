package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ZodanAddNodeInput struct {
	TaskID     uuid.UUID      `json:"task_id"`
	Spec       *database.Spec `json:"spec"`
	SourceNode string         `json:"source_node"`
}

type ZodanAddNodeOutput struct {
	Updated *resource.State `json:"updated"`
}

func (w *Workflows) ZodanAddNode(ctx workflow.Context, input *ZodanAddNodeInput) (*ZodanAddNodeOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("adding Zodan node")

	handleError := func(err error) error {
		logger.With("error", err).Error("failed to add Zodan node")
		updateStateInput := &activities.UpdateDbStateInput{
			DatabaseID: input.Spec.DatabaseID,
			State:      database.DatabaseStateFailed,
		}
		_, _ = w.Activities.ExecuteUpdateDbState(ctx, updateStateInput).Get(ctx)
		return err
	}

	updateTaskInput := &activities.UpdateTaskInput{
		DatabaseID:    input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	refreshCurrentInput := &RefreshCurrentStateInput{
		DatabaseID: input.Spec.DatabaseID,
		TaskID:     input.TaskID,
	}
	refreshCurrentOutput, err := w.ExecuteRefreshCurrentState(ctx, refreshCurrentInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get current state: %w", err))
	}
	current := refreshCurrentOutput.State

	getDesiredInput := &GetDesiredStateInput{
		Spec: input.Spec,
	}
	desiredOutput, err := w.ExecuteGetDesiredState(ctx, getDesiredInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get desired state: %w", err))
	}
	desired := desiredOutput.State

	var (
		zodanInstance  *database.InstanceSpec
		sourceInstance *database.InstanceSpec
		waitSyncInputs []*activities.WaitForSyncEventInput
		zodanNodeInfo  = input.Spec.HasZodanTargetNode()
	)

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	zodanInstance, sourceInstance, peerInstances := segregateZodanAndPeers(nodeInstances, zodanNodeInfo)
	if zodanInstance == nil {
		return nil, fmt.Errorf("no zodan-enabled instance found")
	}
	for _, instance := range peerInstances {
		// 1. Create disabled subscription on Zodan node for this peer

		subInput := &activities.CreateDisabledSubscriptionInput{
			TaskID:               input.TaskID,
			Spec:                 input.Spec,
			SubscriberInstanceID: zodanInstance.InstanceID,
			ProviderInstanceID:   instance.InstanceID,
		}
		_, err := w.Activities.ExecuteCreateDisabledSubscription(ctx, zodanInstance.HostID, subInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to create disabled subscription to %s: %w", instance.NodeName, err))
		}

		// 2. Create replication slot on peer
		slotInput := &activities.CreateReplicationSlotInput{
			Spec:                 input.Spec,
			ProviderInstanceID:   instance.InstanceID,
			SubscriberInstanceID: zodanInstance.InstanceID,
		}
		if _, err := w.Activities.ExecuteCreateReplicationSlot(ctx, instance.HostID, slotInput).Get(ctx); err != nil {
			return nil, handleError(fmt.Errorf("failed to create replication slot on %s: %w", instance.NodeName, err))
		}

		// 3. Trigger sync event from peer
		triggerInput := &activities.TriggerSyncEventInput{
			Spec:       input.Spec,
			InstanceID: instance.InstanceID,
		}
		triggerOutput, err := w.Activities.ExecuteTriggerSyncEvent(ctx, instance.HostID, triggerInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to trigger sync event on host %s: %w", instance.HostID, err))
		}

		// 4. Append for wait step on Zodan
		waitSyncInputs = append(waitSyncInputs, &activities.WaitForSyncEventInput{
			Spec:            input.Spec,
			OriginName:      instance.NodeName,
			LSN:             triggerOutput.LSN,
			ZodanInstanceID: zodanInstance.InstanceID,
		})
	}

	_ = w.Activities.ExecuteUpdateInstance(ctx, &activities.UpdateInstanceInput{
		DatabaseID: input.Spec.DatabaseID,
		InstanceID: zodanInstance.InstanceID,
		State:      string(database.InstanceStateZodanSyncing),
	})

	// Now wait for each sync from the zodan instance
	for _, waitInput := range waitSyncInputs {
		_, err := w.Activities.ExecuteWaitForSyncEvent(ctx, zodanInstance.HostID, waitInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to wait for sync from origin %s: %w", waitInput.OriginName, err))
		}
	}

	// Add active subscription from source to Zodan
	activeSubInput := &activities.CreateActiveSubscriptionInput{
		TaskID:               input.TaskID,
		Spec:                 input.Spec,
		SubscriberInstanceID: zodanInstance.InstanceID,
		ProviderInstanceID:   sourceInstance.InstanceID,
	}
	if _, err := w.Activities.ExecuteCreateActiveSubscription(ctx, zodanInstance.HostID, activeSubInput).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to create active subscription from source to zodan: %w", err))
	}

	// Trigger sync event from source to Zodan
	triggerSourceInput := &activities.TriggerSyncEventInput{
		Spec:       input.Spec,
		InstanceID: sourceInstance.InstanceID,
	}
	triggerSourceOutput, err := w.Activities.ExecuteTriggerSyncEvent(ctx, sourceInstance.HostID, triggerSourceInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to trigger sync event from source: %w", err))
	}
	// Wait for sync event from source to Zodan
	waitSourceInput := &activities.WaitForSyncEventInput{
		Spec:            input.Spec,
		OriginName:      sourceInstance.NodeName,
		LSN:             triggerSourceOutput.LSN,
		ZodanInstanceID: zodanInstance.InstanceID,
	}
	_, err = w.Activities.ExecuteWaitForSyncEvent(ctx, zodanInstance.HostID, waitSourceInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to wait for sync from source: %w", err))
	}

	// Advance replication slot on source to the LSN received from Zodan
	advanceSlotInput := &activities.AdvanceReplicationSlotInput{
		TaskID:               input.TaskID,
		Spec:                 input.Spec,
		ProviderInstanceID:   sourceInstance.InstanceID,
		SubscriberInstanceID: zodanInstance.InstanceID,
		LSN:                  triggerSourceOutput.LSN,
	}
	if _, err := w.Activities.ExecuteAdvanceReplicationSlot(ctx, sourceInstance.HostID, advanceSlotInput).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to advance replication slot on source: %w", err))
	}

	// Create reverse subscriptions from Zodan to peers
	reverseTargets := append(peerInstances, sourceInstance)
	for _, target := range reverseTargets {
		input := &activities.CreateReverseSubscriptionInput{
			TaskID:               input.TaskID,
			Spec:                 input.Spec,
			SubscriberInstanceID: target.InstanceID,        // n1/n2/n3
			ProviderInstanceID:   zodanInstance.InstanceID, // n4
		}
		if _, err := w.Activities.ExecuteCreateReverseSubscription(ctx, target.HostID, input).Get(ctx); err != nil {
			return nil, handleError(fmt.Errorf("failed to create reverse subscription to %s: %w", target.NodeName, err))
		}
	}

	reconcileInput := &ReconcileStateInput{
		DatabaseID: input.Spec.DatabaseID,
		TaskID:     input.TaskID,
		Current:    current,
		Desired:    desired,
	}
	reconcileOutput, err := w.ExecuteReconcileState(ctx, reconcileInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to reconcile state during zodan: %w", err))
	}

	_ = w.Activities.ExecuteUpdateInstance(ctx, &activities.UpdateInstanceInput{
		DatabaseID: input.Spec.DatabaseID,
		InstanceID: zodanInstance.InstanceID,
		State:      string(database.InstanceStateAvailable),
	})

	updateStateInput := &activities.UpdateDbStateInput{
		DatabaseID: input.Spec.DatabaseID,
		State:      database.DatabaseStateAvailable,
	}
	_, err = w.Activities.ExecuteUpdateDbState(ctx, updateStateInput).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to update database state to available: %w", err))
	}

	updateTaskInput = &activities.UpdateTaskInput{
		DatabaseID:    input.Spec.DatabaseID,
		TaskID:        input.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if err := w.updateTask(ctx, logger, updateTaskInput); err != nil {
		return nil, handleError(err)
	}

	logger.Info("zodan node addition completed successfully")
	return &ZodanAddNodeOutput{
		Updated: reconcileOutput.Updated,
	}, nil
}

func segregateZodanAndPeers(
	nodeInstances []*database.NodeInstances,
	zodanNodeInfo *database.Node,
) (zodan *database.InstanceSpec, sourceInstance *database.InstanceSpec, peers []*database.InstanceSpec) {
	for _, nodeInstance := range nodeInstances {
		for _, inst := range nodeInstance.Instances {
			switch inst.NodeName {
			case zodanNodeInfo.Name:
				zodan = inst
			case zodanNodeInfo.ZodanSource:
				sourceInstance = inst
			default:
				peers = append(peers, inst)
			}
		}
	}
	return
}
