package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/samber/do"
)

type RestartInstanceInput struct {
	DatabaseID  string    `json:"database_id"`
	InstanceID  string    `json:"instance_id"`
	ScheduledAt string    `json:"scheduled_at,omitempty"` // Optional, if empty, restart immediately
	TaskID      uuid.UUID `json:"task_id"`
}
type RestartInstanceOutput struct{}

func (a *Activities) ExecuteRestartInstance(
	ctx workflow.Context,
	input *RestartInstanceInput,
) workflow.Future[*RestartInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*RestartInstanceOutput](ctx, options, a.RestartInstance, input)
}

func (a *Activities) RestartInstance(ctx context.Context, input *RestartInstanceInput) (*RestartInstanceOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
	)
	logger.Info("starting restart instance activity")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	restartPending := true
	restartReq := &patroni.Restart{}
	if input.ScheduledAt != "" {
		scheduleTime, err := time.Parse(time.RFC3339, input.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("invalid time received : %w", err)
		}

		restartReq.Schedule = &scheduleTime
	}
	restartReq.RestartPending = &restartPending

	err = patroniClient.ScheduleRestart(ctx, restartReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get restart instance : %w", err)
	}

	logger.Info("restart instance completed")
	return &RestartInstanceOutput{}, nil
}
