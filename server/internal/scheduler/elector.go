package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

var ErrNonLeader = errors.New("the elector is not leader")

var _ gocron.Elector = (*Elector)(nil)
var _ do.Shutdownable = (*Elector)(nil)

type Elector struct {
	mu       sync.Mutex
	isLeader bool
	hostID   string
	store    *LeaderStore
	logger   zerolog.Logger
	ttl      time.Duration
	watchOp  storage.WatchOp[*StoredLeader]
	ticker   *time.Ticker
	done     chan struct{}
	errCh    chan error
}

func NewElector(
	hostID string,
	store *LeaderStore,
	logger zerolog.Logger,
	ttl time.Duration,
) *Elector {
	return &Elector{
		hostID: hostID,
		store:  store,
		logger: logger.With().Str("component", "scheduler_elector").Logger(),
		ttl:    ttl,
		done:   make(chan struct{}),
		errCh:  make(chan error, 1),
	}
}

func (e *Elector) Start(ctx context.Context) error {
	if err := e.checkClaim(ctx); err != nil {
		return err
	}

	e.ticker = time.NewTicker(e.ttl / 3)
	go func() {
		defer e.ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.done:
				return
			case <-e.ticker.C:
				if err := e.checkClaim(ctx); err != nil {
					e.errCh <- err
				}
			}
		}
	}()

	go e.watch(ctx)

	if err := e.IsLeader(ctx); err == nil {
		e.logger.Debug().Msg("this host is the scheduler leader")
	}

	return nil
}

func (e *Elector) IsLeader(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isLeader {
		return ErrNonLeader
	}

	return nil
}

func (e *Elector) Shutdown() error {
	if e.watchOp != nil {
		e.watchOp.Close()
	}

	close(e.done)

	return nil
}

func (e *Elector) Error() <-chan error {
	return e.errCh
}

func (e *Elector) checkClaim(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	leader, err := e.store.GetByKey().Exec(ctx)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return e.attemptClaim(ctx)
	case err != nil:
		return fmt.Errorf("failed to check for existing leader: %w", err)
	}

	e.isLeader = leader.HostID == e.hostID
	if !e.isLeader {
		return nil
	}

	err = e.store.Update(leader).
		WithTTL(e.ttl).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh claim")
	}

	return nil
}

func (e *Elector) attemptClaim(ctx context.Context) error {
	e.logger.Debug().Msg("attempting to claim scheduler leadership")

	err := e.store.
		Create(&StoredLeader{
			HostID:    e.hostID,
			CreatedAt: time.Now(),
		}).
		WithTTL(e.ttl).
		Exec(ctx)
	switch {
	case err == nil:
		e.isLeader = true
		e.logger.Debug().Msg("successfully claimed scheduler leadership")
	case !errors.Is(err, storage.ErrAlreadyExists):
		return fmt.Errorf("failed to claim scheduler leadership: %w", err)
	default:
		e.isLeader = false
	}

	return nil
}

func (e *Elector) watch(ctx context.Context) {
	e.watchOp = e.store.Watch()
	err := e.watchOp.Watch(ctx, func(evt *storage.Event[*StoredLeader]) {
		switch evt.Type {
		case storage.EventTypeDelete:
			// The delete event will fire simultaneously with the ticker in some
			// types of outages, so the claim might have already been created
			// when this handler runs, even though its for a 'delete' event.
			if err := e.checkClaim(ctx); err != nil {
				e.errCh <- err
			}
		case storage.EventTypeError:
			e.logger.Debug().Err(evt.Err).Msg("encountered a watch error")
			if errors.Is(evt.Err, storage.ErrWatchClosed) {
				defer e.watch(ctx)
			}
		}
	})
	if err != nil {
		e.errCh <- fmt.Errorf("failed to start watch: %w", err)
	}
}
