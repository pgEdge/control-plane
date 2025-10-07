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

type CheckClusterHealthInput struct {
	DatabaseID string    `json:"database_id"`
	InstanceID string    `json:"instance_id"`
	TaskID     uuid.UUID `json:"task_id,omitempty"`
}

type CheckClusterHealthOutput struct {
	Healthy          bool   `json:"healthy"`
	LeaderInstanceID string `json:"leader_instance_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

func (a *Activities) ExecuteCheckClusterHealth(ctx workflow.Context, hostID string, input *CheckClusterHealthInput) workflow.Future[*CheckClusterHealthOutput] {
	opts := workflow.ActivityOptions{
		Queue:        utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{MaxAttempts: 1},
	}
	return workflow.ExecuteActivity[*CheckClusterHealthOutput](ctx, opts, a.CheckClusterHealth, input)
}

func (a *Activities) CheckClusterHealth(ctx context.Context, input *CheckClusterHealthInput) (*CheckClusterHealthOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)

	logger.Info("starting cluster health check")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		logger.Error("failed to resolve orchestrator", "error", err)
		return nil, fmt.Errorf("failed to resolve orchestrator: %w", err)
	}

	// Resolve connection information for the instance we will query.
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		logger.Error("failed to get instance connection info", "error", err, "instance", input.InstanceID)
		return nil, fmt.Errorf("failed to get instance connection info for %s: %w", input.InstanceID, err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	// Query Patroni cluster status.
	cluster, err := pClient.GetClusterStatus(ctx)
	if err != nil {
		logger.Error("failed to query patroni cluster status", "error", err)
		return nil, fmt.Errorf("failed to query patroni cluster status: %w", err)
	}

	out := &CheckClusterHealthOutput{}

	if cluster == nil || len(cluster.Members) == 0 {
		out.Healthy = false
		out.Reason = "empty cluster response"
		logger.Info("cluster health check finished", "healthy", out.Healthy, "reason", out.Reason)
		return out, nil
	}

	leaderMember, ok := cluster.Leader()
	if !ok {
		out.Healthy = false
		out.Reason = "no leader reported"
		logger.Info("cluster health check finished", "healthy", out.Healthy, "reason", out.Reason)
		return out, nil
	}

	out.LeaderInstanceID = utils.FromPointer(leaderMember.Name)

	if !leaderMember.IsRunning() {
		out.Healthy = false
		out.Reason = fmt.Sprintf("leader %s not running (state=%v)", out.LeaderInstanceID, utils.FromPointer(leaderMember.State))
		logger.Info("cluster health check finished", "healthy", out.Healthy, "reason", out.Reason)
		return out, nil
	}

	// Now ensure no non-leader member is in an error state.
	var unhealthy []string
	for _, m := range cluster.Members {
		// skip leader itself
		if m.IsLeader() {
			continue
		}
		// If member is in an error state, consider cluster not healthy.
		if m.InErrorState() {
			ident := utils.FromPointer(m.Name)
			if ident == "" {
				ident = utils.FromPointer(m.Host)
			}
			unhealthy = append(unhealthy, ident)
		}
	}

	if len(unhealthy) > 0 {
		out.Healthy = false
		out.Reason = fmt.Sprintf("unhealthy replicas: %v", unhealthy)
	} else {
		out.Healthy = true
		out.Reason = "leader present and no replicas in error state"
	}

	logger.Info("cluster health check finished", "healthy", out.Healthy, "reason", out.Reason)
	return out, nil
}
