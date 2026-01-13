package workflows

import (
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type SwitchoverInput struct {
	DatabaseID          string
	NodeName            string
	Instances           []*activities.InstanceHost
	CandidateInstanceID string
	ScheduledAt         time.Time
	TaskID              uuid.UUID
}

type SwitchoverOutput struct{}

func (w *Workflows) Switchover(ctx workflow.Context, in *SwitchoverInput) (*SwitchoverOutput, error) {

	logger := workflow.Logger(ctx).With(
		"database_id", in.DatabaseID,
		"task_id", in.TaskID.String(),
		"node_name", in.NodeName,
	)
	logger.Info("starting switchover workflow")
	var leaderHostID string
	var leaderInstanceID string

	// cleanup on cancellation
	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) {
			logger.Warn("workflow cancelled; running cleanup")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)
			if in != nil && in.TaskID != uuid.Nil {
				if leaderHostID != "" && leaderInstanceID != "" {
					cancelIn := &activities.CancelSwitchoverInput{
						DatabaseID:       in.DatabaseID,
						LeaderInstanceID: leaderInstanceID,
						TaskID:           in.TaskID,
					}
					if _, err := w.Activities.ExecuteCancelSwitchover(cleanupCtx, leaderHostID, cancelIn).Get(cleanupCtx); err != nil {
						logger.Warn("cancel switchover activity failed", "err", err)
					} else {
						logger.Info("cancel switchover activity dispatched")
					}
				}
			}
			w.cancelTask(cleanupCtx, task.ScopeDatabase, in.DatabaseID, in.TaskID, logger)
		}
	}()

	handleError := func(cause error) error {
		logger.With("error", cause).Error("switchover failed")
		updateTaskInput := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      in.DatabaseID,
			TaskID:        in.TaskID,
			UpdateOptions: task.UpdateFail(cause),
		}
		_, _ = w.Activities.ExecuteUpdateTask(ctx, updateTaskInput).Get(ctx)
		return cause
	}

	// mark task as running
	startUpdate := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      in.DatabaseID,
		TaskID:        in.TaskID,
		UpdateOptions: task.UpdateStart(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(ctx, startUpdate).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("failed to mark task running: %w", err))
	}

	// determine an instance id to query for primary resolution (use first provided instance)
	var instanceToQuery string
	var getPrimaryQueue string
	if len(in.Instances) > 0 && in.Instances[0] != nil {
		instanceToQuery = in.Instances[0].InstanceID
		getPrimaryQueue = in.Instances[0].HostID
	}
	if instanceToQuery == "" {
		return nil, handleError(fmt.Errorf("no instance id available to resolve primary for node %s", in.NodeName))
	}

	// Resolve primary instance (leader) for the node via activity, using the instance id.
	getPrimaryIn := &activities.GetPrimaryInstanceInput{
		DatabaseID: in.DatabaseID,
		InstanceID: instanceToQuery,
	}

	getPrimaryOut, err := w.Activities.ExecuteGetPrimaryInstance(ctx, getPrimaryQueue, getPrimaryIn).Get(ctx)
	if err != nil {
		return nil, handleError(fmt.Errorf("failed to get primary instance: %w", err))
	}
	leaderInstanceID = getPrimaryOut.PrimaryInstanceID

	// Resolve leaderHostID by scanning provided Instances
	for _, inst := range in.Instances {
		if inst != nil && inst.InstanceID == leaderInstanceID {
			leaderHostID = inst.HostID
			break
		}
	}
	if leaderHostID == "" {
		return nil, handleError(fmt.Errorf("failed to resolve leader host id for instance %s", leaderInstanceID))
	}

	logger.Info("primary resolved", "leader_instance", leaderInstanceID, "leader_host", leaderHostID)

	candidateID := in.CandidateInstanceID
	if candidateID == "" {
		selIn := &activities.SelectCandidateInput{
			DatabaseID:      in.DatabaseID,
			NodeName:        in.NodeName,
			ExcludeInstance: leaderInstanceID,
			Instances:       in.Instances,
		}
		selOut, err := w.Activities.ExecuteSelectCandidate(ctx, selIn).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("candidate selection failed: %w", err))
		}
		if selOut == nil || selOut.CandidateInstanceID == "" {
			return nil, handleError(fmt.Errorf("no eligible candidate found"))
		}
		candidateID = selOut.CandidateInstanceID
	}

	logger.Info("candidate chosen", "candidate_instance", candidateID)

	if candidateID == leaderInstanceID {
		logger.Info("candidate is already the leader; skipping switchover", "candidate", candidateID)
		completeUpdate := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      in.DatabaseID,
			TaskID:        in.TaskID,
			UpdateOptions: task.UpdateComplete(),
		}
		_, _ = w.Activities.ExecuteUpdateTask(ctx, completeUpdate).Get(ctx)
		return &SwitchoverOutput{}, nil
	}

	performIn := &activities.PerformSwitchoverInput{
		DatabaseID:          in.DatabaseID,
		LeaderInstanceID:    leaderInstanceID,
		CandidateInstanceID: candidateID,
		ScheduledAt:         in.ScheduledAt,
		TaskID:              in.TaskID,
	}

	logger.Info("dispatching perform switchover activity", "target_host_queue", utils.HostQueue(leaderHostID))

	if _, err := w.Activities.ExecutePerformSwitchover(ctx, leaderHostID, performIn).Get(ctx); err != nil {
		return nil, handleError(fmt.Errorf("perform switchover activity failed: %w", err))
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

	logger.Info("switchover workflow completed successfully")
	return &SwitchoverOutput{}, nil
}
