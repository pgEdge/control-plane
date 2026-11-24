package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetNodeLeaderInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
}

type GetNodeLeaderOutput struct {
	// LeaderInstanceID is empty if the node has no leader.
	LeaderInstanceID string `json:"leader_instance_id,omitempty"`
}

func (a *Activities) ExecuteGetNodeLeader(
	ctx workflow.Context,
	hostID string,
	input *GetNodeLeaderInput,
) workflow.Future[*GetNodeLeaderOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetNodeLeaderOutput](ctx, options, a.GetNodeLeader, input)
}

// GetNodeLeader returns the leader of the instance's node, if it has one.
// Unlike GetPrimaryInstance, it doesn't wait for a leader to be elected. Any
// member's Patroni API reports the whole cluster's state, so the given
// instance can be a replica.
func (a *Activities) GetNodeLeader(ctx context.Context, input *GetNodeLeaderInput) (*GetNodeLeaderOutput, error) {
	logger := activity.Logger(ctx).With("instance_id", input.InstanceID)
	logger.Info("determining node leader")

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}
	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)
	cluster, err := patroniClient.GetClusterStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	out := &GetNodeLeaderOutput{}
	if leader, ok := cluster.Leader(); ok && leader.Name != nil {
		out.LeaderInstanceID = *leader.Name
	}

	return out, nil
}
