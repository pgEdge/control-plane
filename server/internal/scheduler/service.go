package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/go-co-op/gocron"
	elector "github.com/go-co-op/gocron-etcd-elector"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/rs/zerolog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type Service struct {
	logger     zerolog.Logger
	store      *ScheduledJobStore
	executor   WorkflowExecutor
	etcdClient *clientv3.Client
	scheduler  *gocron.Scheduler
	runners    map[string]*gocron.Job
	watchOp    storage.WatchOp[*StoredScheduledJob]
	errCh      chan error
	mu         sync.RWMutex
}

// NewService initializes a new scheduled job service with a scheduler and job store.
func NewService(
	logger zerolog.Logger,
	store *ScheduledJobStore,
	executor WorkflowExecutor,
	etcdClient *clientv3.Client,
) *Service {
	return &Service{
		logger:     logger,
		store:      store,
		executor:   executor,
		etcdClient: etcdClient,
		runners:    make(map[string]*gocron.Job),
		errCh:      make(chan error, 1),
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.logger.Info().Msg("starting scheduler service")
	el, err := elector.NewElectorWithClient(ctx, s.etcdClient, concurrency.WithTTL(30))
	if err != nil {
		return fmt.Errorf("failed to create elector: %w", err)
	}
	// Will show election logs when log level is debug
	el.SetLogger(s.logger.Print)
	go func() {
		if err := el.Start(s.store.ElectorPrefix()); err != nil {
			s.errCh <- fmt.Errorf("failed to start etcd elector: %w", err)
		}
	}()
	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.WithDistributedElector(el)
	s.scheduler = scheduler

	jobs, err := s.store.GetAll().Exec(ctx)
	if err != nil {
		s.logger.Debug().Err(err).Msg("failed to retrieve scheduled jobs from store")
	}
	for _, job := range jobs {
		if err := s.registerJob(ctx, job); err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to register scheduled job")
		}
	}

	go s.watchJobChanges(ctx)
	s.scheduler.StartAsync()

	return nil
}

func (s *Service) Shutdown() error {
	s.logger.Info().Msg("shutting down scheduler service")
	s.scheduler.Stop()

	if s.watchOp != nil {
		s.logger.Info().Msg("closing scheduled job watch")
		s.watchOp.Close()
	}

	return nil
}

func (s *Service) registerJob(ctx context.Context, job *StoredScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runners[job.ID]; exists {
		return nil
	}

	runner, err := NewScheduledJobRunner(job, s.executor, s.logger, s.store)
	if err != nil {
		s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to create job runner")
		return fmt.Errorf("invalid job '%s': %w", job.ID, err)
	}

	gocronJob, err := s.scheduler.Cron(job.CronExpr).Tag(job.ID).Do(func() {
		runner.Run(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to schedule job '%s': %w", job.ID, err)
	}

	s.runners[job.ID] = gocronJob
	s.logger.Info().Str("job_id", job.ID).Msg("registered scheduled job")
	return nil
}

func (s *Service) UnregisterJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, ok := s.runners[jobID]; ok {
		s.scheduler.RemoveByReference(job)
		delete(s.runners, jobID)
		s.logger.Info().Str("job_id", jobID).Msg("unregistered scheduled job")
	}
}

func (s *Service) RegisterJob(ctx context.Context, job *StoredScheduledJob) error {
	if err := s.store.Put(job).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store job: %w", err)
	}
	return s.registerJob(ctx, job)
}
func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	if _, err := s.store.Delete(jobID).Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}
	s.UnregisterJob(jobID)
	return nil
}

func (s *Service) JobExists(jobID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.runners[jobID]
	return exists
}

func (s *Service) ListScheduledJobs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.runners))
	for id := range s.runners {
		ids = append(ids, id)
	}
	return ids
}
func (s *Service) watchJobChanges(ctx context.Context) {
	s.logger.Debug().Msg("watching for scheduled job changes")

	s.watchOp = s.store.WatchJobs()
	err := s.watchOp.Watch(ctx, func(e *storage.Event[*StoredScheduledJob]) {
		switch e.Type {
		case storage.EventTypePut:
			s.logger.Debug().Str("job_id", e.Value.ID).Msg("detected job creation or update")
			if err := s.registerJob(ctx, e.Value); err != nil {
				s.logger.Error().Err(err).Str("job_id", e.Value.ID).Msg("failed to register job from watch")
			}
		case storage.EventTypeDelete:
			jobID := path.Base(e.Key)
			s.logger.Debug().Str("job_id", jobID).Msg("detected job deletion")
			s.UnregisterJob(jobID)
		case storage.EventTypeError:
			s.logger.Debug().Err(e.Err).Msg("encountered a watch error")
			if errors.Is(e.Err, storage.ErrWatchClosed) {
				defer s.watchJobChanges(ctx)
			}
		default:
			s.logger.Warn().
				Err(e.Err).
				Str("event_type", string(e.Type)).
				Msg("unhandled event type in scheduled job watch")
		}
	})
	if err != nil {
		s.logger.Debug().Err(err).Msg("job watch exited with error")
	}
}

func (s *Service) Error() <-chan error {
	return s.errCh
}
