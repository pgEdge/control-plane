package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type InstanceHost struct {
	InstanceID string `json:"instance_id"`
	HostID     string `json:"host_id"`
}

type PerformSwitchoverInput struct {
	DatabaseID          string    `json:"database_id"`
	LeaderInstanceID    string    `json:"leader_instance_id"`
	CandidateInstanceID string    `json:"candidate_instance_id,omitempty"`
	ScheduledAt         time.Time `json:"scheduled_at,omitempty"`
	TaskID              uuid.UUID `json:"task_id"`
}

type PerformSwitchoverOutput struct{}

func (a *Activities) ExecutePerformSwitchover(ctx workflow.Context, hostID string, input *PerformSwitchoverInput) workflow.Future[*PerformSwitchoverOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PerformSwitchoverOutput](ctx, opts, a.PerformSwitchover, input)
}

func (a *Activities) PerformSwitchover(ctx context.Context, input *PerformSwitchoverInput) (*PerformSwitchoverOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get orchestrator: %w", err)
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info for leader: %w", err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	leaderName := &input.LeaderInstanceID
	swReq := &patroni.Switchover{
		Leader: leaderName,
	}
	if input.CandidateInstanceID != "" {
		swReq.Candidate = &input.CandidateInstanceID
	}
	if !input.ScheduledAt.IsZero() {
		swReq.ScheduledAt = &input.ScheduledAt
	}

	if err := pClient.ScheduleSwitchover(ctx, swReq, false); err != nil {
		return nil, fmt.Errorf("patroni scheduled switchover call failed: %w", err)
	}

	logger.Info("patroni scheduled switchover request sent")
	return &PerformSwitchoverOutput{}, nil
}

type CancelSwitchoverInput struct {
	DatabaseID       string    `json:"database_id"`
	LeaderInstanceID string    `json:"leader_instance_id"`
	TaskID           uuid.UUID `json:"task_id"`
}

type CancelSwitchoverOutput struct{}

func (a *Activities) ExecuteCancelSwitchover(ctx workflow.Context, hostID string, input *CancelSwitchoverInput) workflow.Future[*CancelSwitchoverOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CancelSwitchoverOutput](ctx, opts, a.CancelSwitchover, input)
}

func (a *Activities) CancelSwitchover(ctx context.Context, input *CancelSwitchoverInput) (*CancelSwitchoverOutput, error) {
	logger := activity.Logger(ctx)
	logger = logger.With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get orchestrator: %w", err)
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info for leader: %w", err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	if err := pClient.CancelSwitchover(ctx); err != nil {
		return nil, fmt.Errorf("patroni cancel switchover call failed: %w", err)
	}

	logger.Info("patroni cancel switchover request sent")
	return &CancelSwitchoverOutput{}, nil
}
