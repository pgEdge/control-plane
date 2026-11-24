package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type PerformFailoverInput struct {
	DatabaseID string `json:"database_id"`
	// LeaderInstanceID is empty if the node has no leader.
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

	if input.CandidateInstanceID == "" {
		logger.Error("no candidate provided for failover; patroni requires a candidate")
		return nil, errors.New("failover requires a candidate instance id")
	}

	// Any member's Patroni API accepts a failover, so we call the leader's if
	// there is one and the candidate's otherwise.
	apiInstanceID := input.LeaderInstanceID
	if apiInstanceID == "" {
		apiInstanceID = input.CandidateInstanceID
	}
	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, apiInstanceID)
	if err != nil {
		logger.Error("failed to get instance connection info", "error", err, "instance", apiInstanceID)
		return nil, fmt.Errorf("failed to get instance connection info for %s: %w", apiInstanceID, err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	failReq := &patroni.Failover{
		Candidate: &input.CandidateInstanceID,
	}
	// Patroni rejects a failover that names a leader when the node has none.
	if input.LeaderInstanceID != "" {
		failReq.Leader = &input.LeaderInstanceID
	}

	logger.Info("calling patroni InitiateFailover", "leader", input.LeaderInstanceID, "candidate", input.CandidateInstanceID)
	if err := pClient.InitiateFailover(ctx, failReq); err != nil {
		logger.Error("patroni InitiateFailover failed", "error", err)
		return nil, fmt.Errorf("patroni initiate failover call failed: %w", err)
	}

	logger.Info("patroni initiate failover request sent")
	return &PerformFailoverOutput{}, nil
}
