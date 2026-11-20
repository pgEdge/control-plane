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

// maxScheduledSwitchoverDelay is how long after its scheduled time a scheduled
// switchover may still start.
const maxScheduledSwitchoverDelay = 5 * time.Minute

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

	completeSkipped := func(reason string, args ...any) (*SwitchoverOutput, error) {
		logger.Info(reason, args...)
		completeUpdate := &activities.UpdateTaskInput{
			Scope:         task.ScopeDatabase,
			EntityID:      in.DatabaseID,
			TaskID:        in.TaskID,
			UpdateOptions: task.UpdateComplete(),
		}
		if _, err := w.Activities.ExecuteUpdateTask(ctx, completeUpdate).Get(ctx); err != nil {
			return nil, handleError(fmt.Errorf("failed to mark task complete: %w", err))
		}
		return &SwitchoverOutput{}, nil
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
	getPrimary := func() (string, error) {
		getPrimaryIn := &activities.GetPrimaryInstanceInput{
			DatabaseID: in.DatabaseID,
			InstanceID: instanceToQuery,
		}
		out, err := w.Activities.ExecuteGetPrimaryInstance(ctx, getPrimaryQueue, getPrimaryIn).Get(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get primary instance: %w", err)
		}
		return out.PrimaryInstanceID, nil
	}

	// Wait for a scheduled switchover here rather than in Patroni, so that the
	// task stays open, and can be canceled, until the switchover has happened.
	var scheduledFrom string
	if !in.ScheduledAt.IsZero() {
		// Note the primary we're switching away from, so that we don't switch
		// again if it has already changed by the scheduled time.
		var err error
		if scheduledFrom, err = getPrimary(); err != nil {
			return nil, handleError(err)
		}
		if d := in.ScheduledAt.Sub(workflow.Now(ctx)); d > 0 {
			logger.Info("sleeping until scheduled switchover time", "duration", d.String())
			if err := workflow.Sleep(ctx, d); err != nil {
				if errors.Is(err, workflow.Canceled) {
					return nil, err
				}
				return nil, handleError(err)
			}
		}
		// The timer fires late if the Control Plane is down at the scheduled
		// time. Like Patroni, don't run a scheduled switchover long after it
		// was due.
		if late := workflow.Now(ctx).Sub(in.ScheduledAt); late > maxScheduledSwitchoverDelay {
			return nil, handleError(fmt.Errorf("switchover not performed: it was due at %s but couldn't start until %s later",
				in.ScheduledAt.Format(time.RFC3339), late.Round(time.Second)))
		}
	}

	leader, err := getPrimary()
	if err != nil {
		return nil, handleError(err)
	}
	leaderInstanceID = leader

	if scheduledFrom != "" && in.CandidateInstanceID == "" && leaderInstanceID != scheduledFrom {
		return completeSkipped("primary changed before the scheduled time; skipping switchover",
			"scheduled_from", scheduledFrom, "leader_instance", leaderInstanceID)
	}

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

	candidateID, err := w.chooseCandidate(ctx, logger, &chooseCandidateInput{
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

	logger.Info("candidate chosen", "candidate_instance", candidateID)

	if candidateID == leaderInstanceID {
		return completeSkipped("candidate is already the leader; skipping switchover", "candidate", candidateID)
	}

	performIn := &activities.PerformSwitchoverInput{
		DatabaseID:          in.DatabaseID,
		LeaderInstanceID:    leaderInstanceID,
		CandidateInstanceID: candidateID,
		TaskID:              in.TaskID,
	}
	if !in.ScheduledAt.IsZero() {
		// Primary resolution, the candidate's slot check and the activity's
		// queue can all delay the switchover after the check above.
		performIn.Deadline = in.ScheduledAt.Add(maxScheduledSwitchoverDelay)
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
