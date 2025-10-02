package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type PerformFailoverInput struct {
	DatabaseID          string    `json:"database_id"`
	LeaderInstanceID    string    `json:"leader_instance_id"`
	CandidateInstanceID string    `json:"candidate_instance_id,omitempty"`
	TaskID              uuid.UUID `json:"task_id"`
}

type PerformFailoverOutput struct{}

func (a *Activities) ExecutePerformFailover(ctx workflow.Context, hostID string, input *PerformFailoverInput) workflow.Future[*PerformFailoverOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PerformFailoverOutput](ctx, opts, a.PerformFailover, input)
}

func (a *Activities) PerformFailover(ctx context.Context, input *PerformFailoverInput) (*PerformFailoverOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With("database_id", input.DatabaseID, "task_id", input.TaskID.String(),
		"leader_instance", input.LeaderInstanceID, "candidate_instance", input.CandidateInstanceID)

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		logger.Error("failed to resolve orchestrator from injector", "error", err)
		return nil, fmt.Errorf("failed to get orchestrator: %w", err)
	}

	// Get connection info for the leader instance. We will call that leader's Patroni API.
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
	if err != nil {
		logger.Error("failed to get leader instance connection info", "error", err, "leader", input.LeaderInstanceID)
		return nil, fmt.Errorf("failed to get instance connection info for leader %s: %w", input.LeaderInstanceID, err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	if input.CandidateInstanceID == "" {
		logger.Error("no candidate provided for failover; patroni requires a candidate")
		return nil, errors.New("failover requires a candidate instance id")
	}

	failReq := &patroni.Failover{
		Leader:    &input.LeaderInstanceID,
		Candidate: &input.CandidateInstanceID,
	}

	logger.Info("calling patroni InitiateFailover", "leader", input.LeaderInstanceID, "candidate", input.CandidateInstanceID)
	if err := pClient.InitiateFailover(ctx, failReq); err != nil {
		logger.Error("patroni InitiateFailover failed", "error", err)
		return nil, fmt.Errorf("patroni initiate failover call failed: %w", err)
	}

	logger.Info("patroni initiate failover request sent")
	return &PerformFailoverOutput{}, nil
}
