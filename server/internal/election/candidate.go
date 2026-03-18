package election

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/rs/zerolog"
)

// ClaimHandler is a callback function invoked when a candidate successfully
// claims leadership. Handlers are executed in separate goroutines.
type ClaimHandler func(ctx context.Context)

// Candidate participates in a single named election. Once elected, candidate
// maintains leadership until its Stop method is called or until it fails to
// renew its leadership claim.
type Candidate struct {
	mu           sync.Mutex
	running      bool
	store        *ElectionStore
	logger       zerolog.Logger
	electionName Name
	candidateID  string
	isLeader     atomic.Bool
	ttl          time.Duration
	watchOp      storage.WatchOp[*StoredElection]
	ticker       *time.Ticker
	done         chan struct{}
	errCh        chan error
	onClaim      []ClaimHandler
}

// NewCandidate creates a new Candidate instance to participate in the specified
// election. The candidateID uniquely identifies this participant. The ttl
// determines how long a leadership claim remains valid without renewal. The
// onClaim handlers are invoked when this candidate successfully claims
// leadership.
func NewCandidate(
	store *ElectionStore,
	loggerFactory *logging.Factory,
	electionName Name,
	candidateID string,
	ttl time.Duration,
	onClaim []ClaimHandler,
) *Candidate {
	return &Candidate{
		store: store,
		logger: loggerFactory.Logger(logging.ComponentElectionCandidate).With().
			Stringer("election_name", electionName).
			Str("candidate_id", candidateID).
			Logger(),
		electionName: electionName,
		candidateID:  candidateID,
		ttl:          ttl,
		errCh:        make(chan error, 1),
		onClaim:      onClaim,
		watchOp:      store.Watch(electionName),
	}
}

// Start begins participating in the election. It synchronously attempts to
// claim leadership and starts an asynchronous process to periodically refresh
// its claim or re-attempt to claim leadership. Start is idempotent.
func (c *Candidate) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	if err := c.checkClaim(ctx); err != nil {
		return err
	}

	// we're intentionally not assigning e.done and e.ticker directly to capture
	// these variables in this closure and avoid a data race if start is called
	// again.
	done := make(chan struct{}, 1)
	ticker := time.NewTicker(c.ttl / 3)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := c.lockAndCheckClaim(ctx); err != nil {
					c.errCh <- err
				}
			}
		}
	}()

	c.running = true
	c.done = done
	c.ticker = ticker

	if err := c.watch(ctx, done); err != nil {
		return err
	}

	if c.IsLeader() {
		c.logger.Debug().Msg("i am the current leader")
	}

	return nil
}

// IsLeader returns true if this candidate currently holds leadership.
func (c *Candidate) IsLeader() bool {
	return c.isLeader.Load()
}

// Stop ceases participation in the election, releases leadership if held, and
// stops the asynchronous renewal process. Stop is idempotent.
func (c *Candidate) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.watchOp.Close()
	close(c.done)
	c.running = false

	return c.release(ctx)
}

// Error returns a channel that receives errors encountered during election
// operations such as claim attempts, renewals, or watch failures.
func (c *Candidate) Error() <-chan error {
	return c.errCh
}

func (c *Candidate) AddHandlers(handlers ...ClaimHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.onClaim = append(c.onClaim, handlers...)
}

func (c *Candidate) lockAndCheckClaim(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.checkClaim(ctx)
}

func (c *Candidate) checkClaim(ctx context.Context) error {
	const maxRetries = 3
	for range maxRetries {
		curr, err := c.store.GetByKey(c.electionName).Exec(ctx)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			return c.attemptClaim(ctx)
		case err != nil:
			return fmt.Errorf("failed to check for existing leader: %w", err)
		}

		c.isLeader.Store(curr.LeaderID == c.candidateID)
		if curr.LeaderID != c.candidateID {
			return nil
		}

		err = c.store.Update(curr).
			WithTTL(c.ttl).
			Exec(ctx)
		switch {
		case errors.Is(err, storage.ErrValueVersionMismatch):
			// Can happen if we caught the claim right before it expired. The
			// continue will retry the operation. When we re-fetch the claim, it
			// will either not exist or it will belong to someone else.
			continue
		case err != nil:
			return fmt.Errorf("failed to refresh claim: %w", err)
		}

		return nil
	}
	return fmt.Errorf("failed to refresh claim after %d retries", maxRetries)
}

func (c *Candidate) attemptClaim(ctx context.Context) error {
	c.logger.Debug().Msg("attempting to claim leadership")

	err := c.store.
		Create(&StoredElection{
			Name:      c.electionName,
			LeaderID:  c.candidateID,
			CreatedAt: time.Now(),
		}).
		WithTTL(c.ttl).
		Exec(ctx)
	switch {
	case err == nil:
		c.isLeader.Store(true)
		c.logger.Debug().Msg("successfully claimed leadership")
		for _, handler := range c.onClaim {
			go handler(ctx)
		}
	case !errors.Is(err, storage.ErrAlreadyExists):
		return fmt.Errorf("failed to claim leadership: %w", err)
	default:
		c.isLeader.Store(false)
	}

	return nil
}

func (c *Candidate) watch(ctx context.Context, done chan struct{}) error {
	c.logger.Debug().Msg("starting watch")

	err := c.watchOp.Watch(ctx, func(evt *storage.Event[*StoredElection]) error {
		switch evt.Type {
		case storage.EventTypeDelete:
			// The delete event will fire simultaneously with the ticker in some
			// types of outages, so the claim might have already been created
			// when this handler runs, even though its for a 'delete' event.
			if err := c.lockAndCheckClaim(ctx); err != nil {
				return err
			}
		case storage.EventTypeError:
			c.logger.Warn().Err(evt.Err).Msg("encountered a watch error")
		case storage.EventTypeUnknown:
			c.logger.Debug().Msg("encountered unknown watch event type")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case err := <-c.watchOp.Error():
				c.errCh <- err
			}
		}
	}()

	return nil
}

func (c *Candidate) release(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.ttl)
	defer cancel()

	if !c.isLeader.Load() {
		return nil
	}

	curr, err := c.store.GetByKey(c.electionName).Exec(ctx)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		// Happens when the claim has expired since the last time we checked it
		// and no one else has claimed it.
		c.isLeader.Store(false)
		return nil
	case err != nil:
		return fmt.Errorf("failed to fetch current leader: %w", err)
	case curr.LeaderID != c.candidateID:
		// Happens when the claim has expired since the last time we checked it
		// and someone else has claimed it.
		c.isLeader.Store(false)
		return nil
	}

	err = c.store.Delete(curr).Exec(ctx)
	switch {
	case errors.Is(err, storage.ErrValueVersionMismatch):
		// Happens when the claim has expired after the above check and someone
		// else has claimed it.
		c.isLeader.Store(false)
		return nil
	case err != nil:
		return fmt.Errorf("failed to release leadership claim: %w", err)
	}

	c.isLeader.Store(false)
	return nil
}
