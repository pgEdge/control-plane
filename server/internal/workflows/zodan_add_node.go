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
	TaskID uuid.UUID      `json:"task_id"`
	Spec   *database.Spec `json:"spec"`
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

	var zodanInstance *database.InstanceSpec
	var waitSyncInputs []*activities.WaitForSyncEventInput
	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}
	for _, nodeInstance := range nodeInstances {
		for _, instance := range nodeInstance.Instances {
			if instance.ZodanEnabled {
				zodanInstance = instance
				continue
			}
			syncEventInput := &activities.TriggerSyncEventInput{
				Spec:       input.Spec,
				InstanceID: instance.InstanceID,
			}
			output, err := w.Activities.ExecuteTriggerSyncEvent(ctx, instance.HostID, syncEventInput).Get(ctx)
			if err != nil {
				return nil, handleError(fmt.Errorf("failed to trigger sync event on host %s: %w", instance.HostID, err))
			}
			waitSyncInputs = append(waitSyncInputs, &activities.WaitForSyncEventInput{
				Spec:       input.Spec,
				OriginName: instance.NodeName,
				LSN:        output.LSN,
			})
		}
	}

	if zodanInstance == nil {
		return nil, fmt.Errorf("no zodan-enabled instance found")
	}
	_ = w.Activities.ExecuteUpdateInstance(ctx, &activities.UpdateInstanceInput{
		DatabaseID: input.Spec.DatabaseID,
		InstanceID: zodanInstance.InstanceID,
		State:      string(database.InstanceStateZodanSyncing),
	})

	for _, waitInput := range waitSyncInputs {
		_, err := w.Activities.ExecuteWaitForSyncEvent(ctx, zodanInstance.HostID, waitInput).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("failed to wait for sync from origin %s: %w", waitInput.OriginName, err))
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
