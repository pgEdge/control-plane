package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

type ScheduledJobRunner struct {
	Job      *StoredScheduledJob
	Executer WorkflowExecutor
	Logger   zerolog.Logger
	Store    *ScheduledJobStore
}

func NewScheduledJobRunner(
	job *StoredScheduledJob,
	executer WorkflowExecutor,
	logger zerolog.Logger,
	store *ScheduledJobStore,
) *ScheduledJobRunner {
	return &ScheduledJobRunner{
		Job:      job,
		Executer: executer,
		Logger:   logger,
		Store:    store,
	}
}

func (r *ScheduledJobRunner) Run(ctx context.Context) {
	start := time.Now()
	r.setStatus(JobStatusRunning, fmt.Sprintf("Job started at %s\n", start.Format(time.RFC3339)))
	r.logInfo("Starting scheduled job", start)

	err := validateScheduledJob(r.Job)
	if err != nil {
		r.failJob(ctx, err, "Job validation failed")
		return
	}

	err = r.Executer.Execute(ctx, r.Job.Workflow, r.Job.ArgsJSON)
	now := time.Now()

	if err != nil {
		r.failJob(ctx, err, "Scheduled job failed")
	} else {
		duration := now.Sub(start)
		r.setStatus(JobStatusCompleted, fmt.Sprintf("Job completed at %s in %s\n", now.Format(time.RFC3339), duration))
		r.Logger.Info().
			Str("job_id", r.Job.ID).
			Str("workflow", r.Job.Workflow).
			Dur("duration", duration).
			Time("completed_at", now).
			Msg("Scheduled job completed successfully")
	}

	if next := r.Store.GetNextRun(r.Job.ID); next != nil {
		r.Job.NextRun = next
	}

	r.saveJob(ctx)
}

func (r *ScheduledJobRunner) failJob(ctx context.Context, err error, message string) {
	r.setStatus(JobStatusFailed, fmt.Sprintf("%s: %v\n", message, err))
	r.Logger.Error().
		Err(err).
		Str("job_id", r.Job.ID).
		Str("workflow", r.Job.Workflow).
		Msg(message)
	r.saveJob(ctx)
}

func (r *ScheduledJobRunner) setStatus(status, logMsg string) {
	r.Job.Status = status
	r.Job.Logs += logMsg
}

func (r *ScheduledJobRunner) logInfo(msg string, start time.Time) {
	r.Logger.Info().
		Str("job_id", r.Job.ID).
		Str("workflow", r.Job.Workflow).
		Time("started_at", start).
		Msg(msg)
}

func (r *ScheduledJobRunner) saveJob(ctx context.Context) {
	err := r.Store.Put(r.Job).Exec(ctx)
	if err != nil {
		r.Logger.Error().
			Err(err).
			Str("job_id", r.Job.ID).
			Msg("Failed to update job in store")
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
