package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type FailoverInput struct {
	DatabaseID          string
	NodeName            string
	Instances           []*activities.InstanceHost
	CandidateInstanceID string
	SkipValidation      bool
	TaskID              uuid.UUID
}

type FailoverOutput struct{}

func (w *Workflows) Failover(ctx workflow.Context, in *FailoverInput) (*FailoverOutput, error) {
	logger := workflow.Logger(ctx).With(
		"database_id", in.DatabaseID,
		"task_id", in.TaskID.String(),
		"node_name", in.NodeName,
	)
	logger.Info("starting failover workflow")

	var leaderHostID string
	var leaderInstanceID string

	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow cancelled; running cleanup")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)
			w.cancelTask(cleanupCtx, task.ScopeDatabase, in.DatabaseID, in.TaskID, logger)
		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("failover failed")
		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      in.DatabaseID,
			TaskID:        in.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_, _ = w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx)
		return cause
	}

	startUpdate := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      in.DatabaseID,
		TaskID:        in.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, startUpdate).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to mark task running: %w", err))
	}

	leaderInstanceID, err := w.getNodeLeader(ctx, in)
	if err != nil {
		return nil, handleError(err)
	}

	var candidateID string
	if leaderInstanceID == "" {
		// Patroni promotes a replica on its own when it can, so a node without
		// a leader usually means that Patroni refused to promote the remaining
		// instances, e.g. because they're behind the cluster's timeline.
		// Promoting one of them can lose writes, so we only do it when the
		// caller chose the candidate.
		if in.CandidateInstanceID == "" {
			return nil, handleError(fmt.Errorf("node %s has no leader; a candidate instance is required to fail over", in.NodeName))
		}
		candidateID = in.CandidateInstanceID
		logger.Warn("node has no leader; failing over to the requested candidate", "candidate_instance", candidateID)
	} else {
		leaderHostID = instanceHostID(in.Instances, leaderInstanceID)
		if leaderHostID == "" {
			return nil, handleError(fmt.Errorf("failed to resolve leader host id for instance %s", leaderInstanceID))
		}

		logger.Info("primary resolved", "leader_instance", leaderInstanceID, "leader_host", leaderHostID)

		// If skipValidation is false, check cluster health and refuse if healthy.
		if !in.SkipValidation {
			// Call activity to check cluster health on the leader host queue.
			checkIn := &activities.CheckClusterHealthInput{
				DatabaseID: in.DatabaseID,
				InstanceID: leaderInstanceID,
			}
			checkOut, err := w.Activities.ExecuteCheckClusterHealth(ctx, leaderHostID, checkIn).Get(ctx)
			if err != nil {
				return nil, handleError(fmt.Errorf("failed to check cluster health: %w", err))
			}
			if checkOut != nil && checkOut.Healthy {
				return nil, handleError(fmt.Errorf("cluster is healthy; refuse failover unless skip_validation is true"))
			}
		}

		// Determine candidate
		candidateID, err = w.chooseCandidate(ctx, logger, &chooseCandidateInput{
			DatabaseID:       in.DatabaseID,
			NodeName:         in.NodeName,
			Instances:        in.Instances,
			LeaderInstanceID: leaderInstanceID,
			LeaderHostID:     leaderHostID,
			Requested:        in.CandidateInstanceID,
		})
		if err != nil {
			return nil, handleError(err)
		}
	}

	logger.Info("candidate chosen", "candidate_instance", candidateID)

	if candidateID == leaderInstanceID {
		logger.Info("candidate is already the leader; skipping failover", "candidate", candidateID)
		completeUpdate := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      in.DatabaseID,
			TaskID:        in.TaskID,
			UpdateOptions: task.UpdateComplete(),
		}
		_, _ = w.Activities.ExecuteUpdateTask(ctx, completeUpdate).Get(ctx)
		return &FailoverOutput{}, nil
	}

	// Dispatch perform failover activity on the leader's host queue, or the
	// candidate's if there's no leader.
	performIn := &activities.PerformFailoverInput{
		DatabaseID:          in.DatabaseID,
		LeaderInstanceID:    leaderInstanceID,
		CandidateInstanceID: candidateID,
		TaskID:              in.TaskID,
	}
	performHostID := leaderHostID
	if performHostID == "" {
		performHostID = instanceHostID(in.Instances, candidateID)
		if performHostID == "" {
			return nil, handleError(fmt.Errorf("failed to resolve host id for candidate instance %s", candidateID))
		}
	}

	logger.Info("dispatching perform failover activity", "target_host_queue", utils.HostQueue(performHostID))
	if _, err := w.Activities.ExecutePerformFailover(ctx, performHostID, performIn).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("perform failover activity failed: %w", err))
	}

	completeUpdate := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      in.DatabaseID,
		TaskID:        in.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, completeUpdate).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to mark task complete: %w", err))
	}

	logger.Info("failover workflow completed successfully")
	return &FailoverOutput{}, nil
}

// getNodeLeader returns the node's leader, or an empty string if it has none.
// Any instance can report the leader, so it tries each one in turn, starting
// with the requested candidate since it must be reachable for the failover to
// succeed.
func (w *Workflows) getNodeLeader(ctx workflow.Context, in *FailoverInput) (string, error) {
	var ordered []*activities.InstanceHost
	for _, inst := range in.Instances {
		switch {
		case inst == nil:
			continue
		case inst.InstanceID == in.CandidateInstanceID:
			ordered = append([]*activities.InstanceHost{inst}, ordered...)
		default:
			ordered = append(ordered, inst)
		}
	}
	if len(ordered) == 0 {
		return "", fmt.Errorf("no instance id available to resolve primary for node %s", in.NodeName)
	}

	var errs []error
	for _, inst := range ordered {
		out, err := w.Activities.ExecuteGetNodeLeader(ctx, inst.HostID, &activities.GetNodeLeaderInput{
			DatabaseID: in.DatabaseID,
			InstanceID: inst.InstanceID,
		}).Get(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("instance %s: %w", inst.InstanceID, err))
			continue
		}
		return out.LeaderInstanceID, nil
	}

	return "", fmt.Errorf("failed to get the leader of node %s: %w", in.NodeName, errors.Join(errs...))
}

func instanceHostID(instances []*activities.InstanceHost, instanceID string) string {
	for _, inst := range instances {
		if inst != nil && inst.InstanceID == instanceID {
			return inst.HostID
		}
	}
	return ""
}
