package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
)

type Service struct {
	logger    zerolog.Logger
	store     *ScheduledJobStore
	executer  WorkflowExecutor
	scheduler *gocron.Scheduler
	runners   map[string]*gocron.Job
	mu        sync.Mutex
}

func NewService(
	logger zerolog.Logger,
	store *ScheduledJobStore,
	executer WorkflowExecutor,
) *Service {
	s := &Service{
		logger:    logger,
		store:     store,
		executer:  executer,
		scheduler: gocron.NewScheduler(time.UTC),
		runners:   make(map[string]*gocron.Job),
	}
	return s
}

func (s *Service) Start(ctx context.Context) error {
	s.logger.Info().Msg("Starting scheduler service")

	jobs, err := s.store.GetAll().Exec(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to retrieve scheduled jobs from store")
	}
	for _, job := range jobs {
		if err := s.registerJob(ctx, job); err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID).Msg("Failed to register scheduled job")
		}
	}

	s.scheduler.StartAsync()
	return nil
}

func (s *Service) Shutdown() error {
	s.logger.Info().Msg("Shutting down scheduler service")
	s.scheduler.Stop()
	return nil
}

func (s *Service) registerJob(ctx context.Context, job *StoredScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runners[job.ID]; exists {
		s.logger.Warn().Str("job_id", job.ID).Msg("Job already registered, skipping")
		return nil
	}

	runner := NewScheduledJobRunner(job, s.executer, s.logger, s.store)

	gocronJob, err := s.scheduler.Cron(job.CronExpr).Tag(job.ID).Do(func() {
		runner.Run(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to schedule job '%s': %w", job.ID, err)
	}

	s.runners[job.ID] = gocronJob

	s.logger.Info().Str("job_id", job.ID).Msg("Registered scheduled job")
	return nil
}

func (s *Service) UnregisterJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, ok := s.runners[jobID]; ok {
		s.scheduler.RemoveByReference(job)
		delete(s.runners, jobID)
		s.logger.Info().Str("job_id", jobID).Msg("job unregistered")
	}
}

func (s *Service) RegisterJob(ctx context.Context, job *StoredScheduledJob) error {
	job.Status = JobStatusPending
	if err := s.store.Put(job).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store job: %w", err)
	}
	return s.registerJob(ctx, job)
}
func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	_, err := s.store.Delete(jobID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}
	s.UnregisterJob(jobID)
	return nil
}

func (s *Service) ExitsJob(jobID string) bool {
	_, ok := s.runners[jobID]
	return ok
}

func (s *Service) ListScheduledJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := make([]string, 0, len(s.runners))
	for id := range s.runners {
		ids = append(ids, id)
	}
	return ids
}
