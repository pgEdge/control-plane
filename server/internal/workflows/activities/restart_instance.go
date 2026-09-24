package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type RestartInstanceInput struct {
	DatabaseID  string    `json:"database_id"`
	InstanceID  string    `json:"instance_id"`
	ScheduledAt time.Time `json:"scheduled_at,omitempty"` // Optional, if empty, restart immediately
	TaskID      uuid.UUID `json:"task_id"`
}

type RestartInstanceOutput struct {
	// BaselinePostmasterStartTime is pg_postmaster_start_time() as reported by
	// Patroni right before the restart was scheduled. It's used as a baseline
	// to detect when the restart has actually happened.
	//
	// The JSON tag intentionally keeps the pre-rename wire name
	// (postmaster_start_time): this is an activity result that go-workflows
	// persists durably and replays. A workflow already past this activity
	// when a rolling deploy lands would otherwise decode an empty baseline
	// from its recorded history and could report an immediate restart as
	// complete before it actually happens.
	BaselinePostmasterStartTime string `json:"postmaster_start_time,omitempty"`
}

// baselineStatusMaxAttempts and baselineStatusRetryDelay bound how hard we
// try to read the instance's status before scheduling a restart. A transient
// Patroni blip here shouldn't fail the whole operation the way a single
// failed attempt would.
const (
	baselineStatusMaxAttempts = 3
	baselineStatusRetryDelay  = 2 * time.Second
)

func getBaselineStatus(ctx context.Context, patroniClient *patroni.Client) (*patroni.InstanceStatus, error) {
	var lastErr error
	for attempt := 1; attempt <= baselineStatusMaxAttempts; attempt++ {
		status, err := patroniClient.GetInstanceStatus(ctx)
		if err == nil {
			return status, nil
		}
		lastErr = err
		if attempt < baselineStatusMaxAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(baselineStatusRetryDelay):
			}
		}
	}
	return nil, lastErr
}

// parsePatroniTimestamp parses a timestamp as reported by Patroni's REST API
// (e.g. "2026-09-18 05:03:40.251655+00:00").
func parsePatroniTimestamp(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05.999999999Z07:00", s)
}

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

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	status, err := getBaselineStatus(ctx, patroniClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline instance status: %w", err)
	}
	if status.PostmasterStartTime == nil {
		return nil, errors.New("baseline instance status has no postmaster start time")
	}
	baselinePostmasterStartTime := *status.PostmasterStartTime

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

	logger.Info("restart requested")
	return &RestartInstanceOutput{BaselinePostmasterStartTime: baselinePostmasterStartTime}, nil
}

type WaitForRestartCompleteInput struct {
	DatabaseID                  string    `json:"database_id"`
	InstanceID                  string    `json:"instance_id"`
	TaskID                      uuid.UUID `json:"task_id"`
	BaselinePostmasterStartTime string    `json:"baseline_postmaster_start_time,omitempty"`
	// ScheduledAt is the time the restart was scheduled for, if any. When
	// set, a postmaster start time change alone isn't enough to consider the
	// restart complete: it must also be at or after this time, so that an
	// unrelated restart (e.g. a failover) occurring while we wait doesn't get
	// mistaken for the restart we're waiting on.
	ScheduledAt time.Time `json:"scheduled_at,omitempty"`
}

type WaitForRestartCompleteOutput struct{}

func (a *Activities) ExecuteWaitForRestartComplete(
	ctx workflow.Context,
	hostID string,
	input *WaitForRestartCompleteInput,
) workflow.Future[*WaitForRestartCompleteOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*WaitForRestartCompleteOutput](ctx, options, a.WaitForRestartComplete, input)
}

const (
	waitForRestartCompleteTimeout      = 10 * time.Minute
	waitForRestartCompletePollInterval = 5 * time.Second
	waitForRestartCompleteMaxErrors    = 5

	// restartTimeSkewTolerance absorbs ordinary clock skew between the
	// control plane and the Patroni node when comparing an observed restart
	// time against ScheduledAt: without it, a Patroni clock that's even
	// slightly behind would make a genuinely successful scheduled restart
	// look like it happened "too early" on every single poll (the observed
	// timestamp never changes again), hanging the activity until it times
	// out instead of recognizing the restart completed.
	restartTimeSkewTolerance = 30 * time.Second
)

func (a *Activities) WaitForRestartComplete(ctx context.Context, input *WaitForRestartCompleteInput) (*WaitForRestartCompleteOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"instance_id", input.InstanceID,
	)
	logger.Info("waiting for restart to complete")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if input.TaskID != uuid.Nil {
		watcher, err := a.TaskSvc.NewWatcher(ctx, task.ScopeDatabase, input.DatabaseID, input.TaskID)
		if err != nil {
			logger.Warn("failed to start task watcher; activity won't be interrupted on task cancellation", "error", err)
		} else {
			go func() {
				defer watcher.Close()
				select {
				case <-watcher.Done():
					cancel()
				case <-watcher.Error():
					// Watch stream died; stop monitoring without cancelling
					// the activity — we don't know the task's current state.
				case <-ctx.Done():
				}
			}()
		}
	}

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	pollCtx, pollCancel := context.WithTimeout(ctx, waitForRestartCompleteTimeout)
	defer pollCancel()

	ticker := time.NewTicker(waitForRestartCompletePollInterval)
	defer ticker.Stop()

	var errCount int
	for {
		select {
		case <-pollCtx.Done():
			return nil, pollCtx.Err()
		case <-ticker.C:
			status, err := patroniClient.GetInstanceStatus(pollCtx)
			if err != nil {
				errCount++
				if errCount >= waitForRestartCompleteMaxErrors {
					return nil, fmt.Errorf("failed to get instance status: %w", err)
				}
				continue
			}
			errCount = 0
			if status.InErrorState() {
				return nil, fmt.Errorf("instance entered error state %q while waiting for restart", *status.State)
			}
			if !status.InRunningState() || status.PostmasterStartTime == nil {
				continue
			}
			if *status.PostmasterStartTime == input.BaselinePostmasterStartTime {
				continue
			}
			if !input.ScheduledAt.IsZero() {
				restartedAt, parseErr := parsePatroniTimestamp(*status.PostmasterStartTime)
				if parseErr != nil {
					logger.Warn("failed to parse postmaster start time; accepting restart as complete",
						"error", parseErr, "value", *status.PostmasterStartTime)
				} else if restartedAt.Before(input.ScheduledAt.Add(-restartTimeSkewTolerance)) {
					// The instance restarted before our scheduled time (e.g. an
					// unrelated failover) -- keep waiting for the actual
					// scheduled restart.
					continue
				}
			}
			logger.Info("restart completed")
			return &WaitForRestartCompleteOutput{}, nil
		}
	}
}

type CancelRestartInput struct {
	DatabaseID string    `json:"database_id"`
	InstanceID string    `json:"instance_id"`
	TaskID     uuid.UUID `json:"task_id"`
}

type CancelRestartOutput struct{}

func (a *Activities) ExecuteCancelRestart(
	ctx workflow.Context,
	hostID string,
	input *CancelRestartInput,
) workflow.Future[*CancelRestartOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CancelRestartOutput](ctx, options, a.CancelRestart, input)
}

func (a *Activities) CancelRestart(ctx context.Context, input *CancelRestartInput) (*CancelRestartOutput, error) {
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}

	patroniClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	if err := patroniClient.CancelRestart(ctx); err != nil {
		return nil, fmt.Errorf("patroni cancel restart call failed: %w", err)
	}

	logger.Info("patroni cancel restart request sent")
	return &CancelRestartOutput{}, nil
}
