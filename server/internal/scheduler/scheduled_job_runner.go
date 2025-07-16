package scheduler

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

type ScheduledJobRunner struct {
	Job      *StoredScheduledJob
	Executor WorkflowExecutor
	Logger   zerolog.Logger
	Store    *ScheduledJobStore
}

func NewScheduledJobRunner(
	job *StoredScheduledJob,
	executor WorkflowExecutor,
	logger zerolog.Logger,
	store *ScheduledJobStore,
) (*ScheduledJobRunner, error) {
	err := validateScheduledJob(job)
	if err != nil {
		return nil, fmt.Errorf("invalid scheduled job: %w", err)
	}
	return &ScheduledJobRunner{
		Job:      job,
		Executor: executor,
		Logger:   logger,
		Store:    store,
	}, nil
}

func (r *ScheduledJobRunner) Run(ctx context.Context) {
	err := r.Executor.Execute(ctx, r.Job.Workflow, r.Job.ArgsJSON)
	if err != nil {
		r.Logger.Error().Err(err).Str("job_id", r.Job.ID).Msg("failed to start scheduled job")
	} else {
		r.Logger.Info().Str("job_id", r.Job.ID).Msg("scheduled job started")
	}
}

func validateScheduledJob(job *StoredScheduledJob) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	if job.ID == "" {
		return fmt.Errorf("job ID is empty")
	}
	if job.CronExpr == "" {
		return fmt.Errorf("missing cron expression")
	}
	if job.Workflow == "" {
		return fmt.Errorf("workflow name is missing")
	}
	if job.ArgsJSON == nil {
		return fmt.Errorf("workflow arguments are missing")
	}
	return nil
}
