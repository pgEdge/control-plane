package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type TriggerSyncEventInput struct {
	Spec       *database.Spec `json:"spec"`
	InstanceID string         `json:"instance_id"`
}

type TriggerSyncEventOutput struct {
	LSN string `json:"lsn"`
}

type WaitForSyncEventInput struct {
	Spec            *database.Spec `json:"spec"`
	OriginName      string         `json:"origin_name"`
	LSN             string         `json:"lsn"`
	ZodanInstanceID string         `json:"zodan_instance_id,omitempty"`
}

type WaitForSyncEventOutput struct{}

func (a *Activities) ExecuteTriggerSyncEvent(
	ctx workflow.Context,
	hostID string,
	input *TriggerSyncEventInput,
) workflow.Future[*TriggerSyncEventOutput] {
	options := workflow.ActivityOptions{
		Queue:        core.Queue(hostID),
		RetryOptions: workflow.RetryOptions{MaxAttempts: 1},
	}
	return workflow.ExecuteActivity[*TriggerSyncEventOutput](ctx, options, a.TriggerSyncEvent, input)
}

func (a *Activities) TriggerSyncEvent(ctx context.Context, input *TriggerSyncEventInput) (*TriggerSyncEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("triggering spock.sync_event")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	lsn, err := dbSvc.TriggerSyncEvent(ctx, input.Spec, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger sync event: %w", err)
	}

	logger.With("lsn", lsn).Info("sync_event triggered successfully")
	return &TriggerSyncEventOutput{LSN: lsn}, nil
}

func (a *Activities) ExecuteWaitForSyncEvent(
	ctx workflow.Context,
	hostID string,
	input *WaitForSyncEventInput,
) workflow.Future[*WaitForSyncEventOutput] {
	options := workflow.ActivityOptions{
		Queue:        core.Queue(hostID),
		RetryOptions: workflow.RetryOptions{MaxAttempts: 1},
	}
	return workflow.ExecuteActivity[*WaitForSyncEventOutput](ctx, options, a.WaitForSyncEvent, input)
}

func (a *Activities) WaitForSyncEvent(ctx context.Context, input *WaitForSyncEventInput) (*WaitForSyncEventOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.With("origin", input.OriginName, "lsn", input.LSN).Info("waiting for spock.wait_for_sync_event")

	dbSvc, err := do.Invoke[*database.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = dbSvc.WaitForSyncEvent(ctx, input.Spec, input.ZodanInstanceID, input.OriginName, input.LSN, 12000)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for sync event: %w", err)
	}

	logger.Info("sync_event wait completed successfully")
	return &WaitForSyncEventOutput{}, nil
}
