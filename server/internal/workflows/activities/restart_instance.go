package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/samber/do"
)

type RestartInstanceInput struct {
	DatabaseID  string    `json:"database_id"`
	InstanceID  string    `json:"instance_id"`
	ScheduledAt time.Time `json:"scheduled_at,omitempty"` // Optional, if empty, restart immediately
	TaskID      uuid.UUID `json:"task_id"`
}

type RestartInstanceOutput struct{}

func (a *Activities) ExecuteRestartInstance(
	ctx workflow.Context,
	hostID string,
	input *RestartInstanceInput,
) workflow.Future[*RestartInstanceOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
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

	restartReq := &patroni.Restart{}
	if !input.ScheduledAt.IsZero() {
		restartReq.Schedule = &input.ScheduledAt
		logger = logger.With("scheduled_at", input.ScheduledAt.String())
		logger.Info("scheduled restart")
	}

	err = patroniClient.ScheduleRestart(ctx, restartReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get restart instance : %w", err)
	}

	logger.Info("restart instance completed")
	return &RestartInstanceOutput{}, nil
}
