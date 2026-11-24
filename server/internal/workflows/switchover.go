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
	// Patroni can't stop a switchover once it's been requested, so after that
	// point a cancel mustn't mark the task canceled, and task updates use a
	// context that the cancel doesn't affect.
	var switchoverRequested bool
	taskCtx := ctx

	// cleanup on cancellation. Nothing is scheduled in Patroni, so there's no
	// switchover to cancel there, only the task.
	defer func() {
		if errors.Is(ctx.Err(), workflow.Canceled) && !switchoverRequested {
			logger.Warn("workflow cancelled; running cleanup")
			cleanupCtx := workflow.NewDisconnectedContext(ctx)
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
		_, _ = w.Activities.ExecuteUpdateTask(taskCtx, updateTaskInput).Get(taskCtx)
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

	// completeSkipped completes the task without a switchover. The reason goes
	// to the task log, so that it's visible from the API.
	completeSkipped := func(reason string) (*SwitchoverOutput, error) {
		logger.Info(reason)
		if err := w.logTaskEvent(ctx, task.ScopeDatabase, in.DatabaseID, in.TaskID, task.LogEntry{Message: reason}); err != nil {
			logger.With("error", err).Warn("failed to log task event")
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
		return &SwitchoverOutput{}, nil
	}

	if len(in.Instances) == 0 || in.Instances[0] == nil {
		return nil, handleError(fmt.Errorf("no instance id available to resolve primary for node %s", in.NodeName))
	}

	// getPrimary resolves the node's primary instance (leader) from the first
	// of the given instances that answers.
	getPrimary := func(instances []*activities.InstanceHost) (string, error) {
		if len(instances) == 0 {
			return "", fmt.Errorf("no reachable instance of node %s to get the primary from", in.NodeName)
		}
		var err error
		for _, inst := range instances {
			getPrimaryIn := &activities.GetPrimaryInstanceInput{
				DatabaseID: in.DatabaseID,
				InstanceID: inst.InstanceID,
			}
			var out *activities.GetPrimaryInstanceOutput
			out, err = w.Activities.ExecuteGetPrimaryInstance(ctx, inst.HostID, getPrimaryIn).Get(ctx)
			if err == nil {
				return out.PrimaryInstanceID, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return "", fmt.Errorf("failed to get primary instance: %w", err)
	}

	instances := in.Instances
	lookup := in.Instances[:1]
	unreachable := map[string]bool{}

	// Wait for a scheduled switchover here rather than in Patroni, so that the
	// task stays open, and can be canceled, until the switchover has happened.
	var scheduledFrom string
	if !in.ScheduledAt.IsZero() {
		// Note the primary we're switching away from, so that we don't switch
		// again if it has already changed by the scheduled time.
		var err error
		if scheduledFrom, err = getPrimary(lookup); err != nil {
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

		// The node's hosts can change, or go down, during the wait. Use its
		// current instances, and don't send activities to unreachable hosts,
		// where they would never run.
		current, err := w.Activities.ExecuteGetNodeInstances(ctx, &activities.GetNodeInstancesInput{
			DatabaseID: in.DatabaseID,
			NodeName:   in.NodeName,
		}).Get(ctx)
		if err != nil {
			return nil, handleError(fmt.Errorf("switchover not performed: %w", err))
		}
		for _, hostID := range current.UnreachableHosts {
			unreachable[hostID] = true
		}
		instances = current.Instances
		lookup = nil
		var candidate *activities.InstanceHost
		for _, inst := range instances {
			if !unreachable[inst.HostID] {
				lookup = append(lookup, inst)
			}
			if inst.InstanceID == in.CandidateInstanceID {
				candidate = inst
			}
		}
		if in.CandidateInstanceID != "" {
			if candidate == nil {
				return nil, handleError(fmt.Errorf("switchover not performed: candidate instance %s is no longer an instance of node %s",
					in.CandidateInstanceID, in.NodeName))
			}
			if unreachable[candidate.HostID] {
				return nil, handleError(fmt.Errorf("switchover not performed: candidate instance %s is on unreachable host %s",
					in.CandidateInstanceID, candidate.HostID))
			}
		}
	}

	leader, err := getPrimary(lookup)
	if err != nil {
		return nil, handleError(err)
	}
	leaderInstanceID = leader

	if scheduledFrom != "" && leaderInstanceID != scheduledFrom {
		return completeSkipped(fmt.Sprintf("skipping switchover: the primary changed from %s to %s before the scheduled time",
			scheduledFrom, leaderInstanceID))
	}

	// Resolve leaderHostID by scanning the node's instances
	for _, inst := range instances {
		if inst != nil && inst.InstanceID == leaderInstanceID {
			leaderHostID = inst.HostID
			break
		}
	}
	if leaderHostID == "" {
		return nil, handleError(fmt.Errorf("failed to resolve leader host id for instance %s", leaderInstanceID))
	}
	if unreachable[leaderHostID] {
		return nil, handleError(fmt.Errorf("switchover not performed: the primary %s is on unreachable host %s",
			leaderInstanceID, leaderHostID))
	}

	logger.Info("primary resolved", "leader_instance", leaderInstanceID, "leader_host", leaderHostID)

	candidates := instances
	if !in.ScheduledAt.IsZero() {
		candidates = lookup
	}
	candidateID, err := w.chooseCandidate(ctx, logger, &chooseCandidateInput{
		DatabaseID:       in.DatabaseID,
		NodeName:         in.NodeName,
		Instances:        candidates,
		LeaderInstanceID: leaderInstanceID,
		LeaderHostID:     leaderHostID,
		Requested:        in.CandidateInstanceID,
	})
	if err != nil {
		return nil, handleError(err)
	}

	logger.Info("candidate chosen", "candidate_instance", candidateID)

	if candidateID == leaderInstanceID {
		return completeSkipped(fmt.Sprintf("skipping switchover: candidate %s is already the primary", candidateID))
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switchoverRequested = true
	taskCtx = workflow.NewDisconnectedContext(ctx)

	// go-workflows has no timeout for an activity that isn't picked up, and a
	// workflow can't complete while one is pending. So if the leader's host
	// goes down after the check above, this waits until it's back. The
	// activity's Deadline stops a scheduled switchover from then running late.
	logger.Info("dispatching perform switchover activity", "target_host_queue", utils.HostQueue(leaderHostID))

	if _, err := w.Activities.ExecutePerformSwitchover(taskCtx, leaderHostID, performIn).Get(taskCtx); err != nil {
		return nil, handleError(fmt.Errorf("perform switchover activity failed: %w", err))
	}

	completeUpdate := &activities.UpdateTaskInput{
		Scope:         task.ScopeDatabase,
		EntityID:      in.DatabaseID,
		TaskID:        in.TaskID,
		UpdateOptions: task.UpdateComplete(),
	}
	if _, err := w.Activities.ExecuteUpdateTask(taskCtx, completeUpdate).Get(taskCtx); err != nil {
		return nil, handleError(fmt.Errorf("failed to mark task complete: %w", err))
	}

	logger.Info("switchover workflow completed successfully")
	return &SwitchoverOutput{}, nil
}
