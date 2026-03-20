package migrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/election"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/version"
)

// migrations should take on the order of seconds at a maximum, but we're going
// to be overly cautious just in case since this can prevent startup.
const migrationTimeout = 5 * time.Minute

// Runner orchestrates migration execution with distributed locking.
type Runner struct {
	hostID      string
	store       *Store
	injector    *do.Injector
	logger      zerolog.Logger
	migrations  []Migration
	candidate   *election.Candidate
	watchOp     storage.WatchOp[*StoredRevision]
	errCh       chan error
	doneCh      chan struct{}
	doneOnce    sync.Once
	versionInfo *version.Info
}

// NewRunner creates a new migration runner.
func NewRunner(
	hostID string,
	store *Store,
	injector *do.Injector,
	loggerFactory *logging.Factory,
	migrations []Migration,
	candidate *election.Candidate,
) *Runner {
	return &Runner{
		hostID:     hostID,
		store:      store,
		injector:   injector,
		logger:     loggerFactory.Logger(logging.ComponentMigrationRunner),
		migrations: migrations,
		candidate:  candidate,
		errCh:      make(chan error, 1),
		doneCh:     make(chan struct{}),
	}
}

// Run executes any pending migrations if this runner wins the election,
// otherwise waits until the current revision reaches its target.
func (r *Runner) Run(ctx context.Context) error {
	hasPendingMigrations, err := r.hasPendingMigrations(ctx)
	if err != nil {
		return err
	}
	if !hasPendingMigrations {
		return nil
	}

	// failure to get version info is non-fatal
	versionInfo, _ := version.GetInfo()
	r.versionInfo = versionInfo

	ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()

	r.watchOp = r.store.Revision.Watch()
	if err := r.watch(ctx); err != nil {
		return err
	}
	defer r.watchOp.Close()

	r.candidate.AddHandlers(func(_ context.Context) {
		if err := r.runMigrations(ctx); err != nil {
			r.errCh <- err
		}
	})
	if err := r.candidate.Start(ctx); err != nil {
		return fmt.Errorf("failed to initialize locker: %w", err)
	}
	defer r.candidate.Stop(ctx)

	// Block until either the migrations complete, we timeout, or we encounter
	// an error.
	select {
	case <-r.doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for migrations: %w", ctx.Err())
	case err := <-r.errCh:
		return err
	}
}

func (r *Runner) watch(ctx context.Context) error {
	r.logger.Debug().Msg("starting watch")

	if len(r.migrations) == 0 {
		return errors.New("watch called with empty migrations list")
	}
	targetRevision := r.migrations[len(r.migrations)-1].Identifier()

	err := r.watchOp.Watch(ctx, func(evt *storage.Event[*StoredRevision]) error {
		switch evt.Type {
		case storage.EventTypePut:
			if evt.Value.Identifier == targetRevision {
				r.doneOnce.Do(func() {
					close(r.doneCh)
				})
			}
		case storage.EventTypeError:
			r.logger.Warn().Err(evt.Err).Msg("encountered error in watch")
		case storage.EventTypeUnknown:
			r.logger.Debug().Msg("encountered unknown watch event type")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	r.watchOp.PropagateErrors(ctx, r.errCh)

	return nil
}

func (r *Runner) runMigrations(ctx context.Context) error {
	currentRevision, err := r.getCurrentRevision(ctx)
	if err != nil {
		return err
	}

	startIndex := r.findStartIndex(currentRevision)
	if startIndex >= len(r.migrations) {
		r.logger.Debug().Msg("control-plane db is up to date, no migrations to run")
		return nil
	}

	for i := startIndex; i < len(r.migrations); i++ {
		migration := r.migrations[i]
		identifier := migration.Identifier()

		if err := r.runMigration(ctx, migration); err != nil {
			r.logger.Err(err).
				Str("migration", identifier).
				Msg("run migrations error, stopping migrations")
			return err
		}

		if err := r.updateRevision(ctx, identifier); err != nil {
			return fmt.Errorf("failed to update revision: %w", err)
		}
	}

	return nil
}

func (r *Runner) getCurrentRevision(ctx context.Context) (string, error) {
	rev, err := r.store.Revision.Get().Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get current revision: %w", err)
	}
	return rev.Identifier, nil
}

func (r *Runner) findStartIndex(currentRevision string) int {
	if currentRevision == "" {
		return 0
	}

	for i := len(r.migrations) - 1; i >= 0; i-- {
		if r.migrations[i].Identifier() == currentRevision {
			return i + 1
		}
	}

	r.logger.Warn().
		Str("revision", currentRevision).
		Msg("current revision not found in migrations list, starting from beginning")
	return 0
}

func (r *Runner) hasPendingMigrations(ctx context.Context) (bool, error) {
	if len(r.migrations) == 0 {
		r.logger.Debug().Msg("no migrations to run")
		return false, nil
	}

	currentRevision, err := r.getCurrentRevision(ctx)
	if err != nil {
		return false, err
	}

	startIndex := r.findStartIndex(currentRevision)
	if startIndex >= len(r.migrations) {
		r.logger.Debug().Msg("control-plane db is up to date, no migrations to run")
		return false, nil
	}

	return true, nil
}

func (r *Runner) runMigration(ctx context.Context, migration Migration) error {
	identifier := migration.Identifier()
	r.logger.Info().Str("migration", identifier).Msg("running migration")

	stored := &StoredResult{
		Identifier: identifier,
		StartedAt:  time.Now(),
	}
	err := migration.Run(ctx, r.injector)
	if err != nil {
		stored.Error = err.Error()
	} else {
		stored.Successful = true
	}
	stored.CompletedAt = time.Now()
	stored.RunByHostID = r.hostID
	stored.RunByVersionInfo = r.versionInfo

	if storeErr := r.store.Result.Put(stored).Exec(ctx); storeErr != nil {
		return fmt.Errorf("failed to store migration result: %w", storeErr)
	}

	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

func (r *Runner) updateRevision(ctx context.Context, identifier string) error {
	rev, err := r.store.Revision.Get().Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return r.store.Revision.Create(&StoredRevision{Identifier: identifier}).Exec(ctx)
	}
	if err != nil {
		return err
	}
	rev.Identifier = identifier
	return r.store.Revision.Update(rev).Exec(ctx)
}
