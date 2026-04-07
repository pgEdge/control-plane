package election

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/storage"
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
	ttl          time.Duration
	watchOp      storage.WatchOp[*StoredElection]
	ticker       *time.Ticker
	done         chan struct{}
	errCh        chan error
	onClaim      []ClaimHandler
	curr         *StoredElection
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

	if c.isLeader() {
		c.logger.Debug().Msg("i am the current leader")
	}

	return nil
}

// IsLeader returns true if this candidate currently holds leadership.
func (c *Candidate) IsLeader() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.isLeader()
}

func (c *Candidate) isLeader() bool {
	return c.curr != nil && c.curr.LeaderID == c.candidateID
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

func (c *Candidate) attemptClaim(ctx context.Context) error {
	c.logger.Debug().Msg("attempting to claim leadership")

	curr := &StoredElection{
		Name:      c.electionName,
		LeaderID:  c.candidateID,
		CreatedAt: time.Now(),
	}
	err := c.store.
		Create(curr).
		WithUpdatedVersion().
		WithTTL(c.ttl).
		Exec(ctx)
	switch {
	case err == nil:
		c.curr = curr
		c.logger.Debug().Msg("successfully claimed leadership")
		for _, handler := range c.onClaim {
			go handler(ctx)
		}
	case !errors.Is(err, storage.ErrAlreadyExists):
		return fmt.Errorf("failed to claim leadership: %w", err)
	}

	return nil
}

func (c *Candidate) refreshClaim(ctx context.Context) error {
	if !c.isLeader() {
		return errors.New("cannot refresh claim when not the leader")
	}

	err := c.store.Update(c.curr).
		WithTTL(c.ttl).
		WithUpdatedVersion().
		Exec(ctx)
	// We tolerate ErrValueVersionMismatch because it usually means another
	// candidate has successfully claimed leadership. We want to return without
	// error, release the lock, and let our watch catch up.
	if err != nil && !errors.Is(err, storage.ErrValueVersionMismatch) {
		return fmt.Errorf("failed to refresh claim: %w", err)
	}

	return nil
}

func (c *Candidate) checkClaim(ctx context.Context) error {
	switch {
	case c.curr == nil:
		return c.attemptClaim(ctx)
	case c.curr.LeaderID == c.candidateID:
		return c.refreshClaim(ctx)
	}

	return nil
}

func (c *Candidate) watch(ctx context.Context, done chan struct{}) error {
	c.logger.Debug().Msg("starting watch")

	err := c.watchOp.Watch(ctx, func(evt *storage.Event[*StoredElection]) error {
		switch evt.Type {
		case storage.EventTypeError:
			c.logger.Warn().Err(evt.Err).Msg("encountered a watch error")
		case storage.EventTypeUnknown:
			c.logger.Debug().Msg("encountered unknown watch event type")
		}

		switch evt.Type {
		case storage.EventTypePut:

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
	if !c.isLeader() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.ttl)
	defer cancel()

	err := c.store.Delete(c.curr).Exec(ctx)
	// ErrValueVersionMismatch happens when the claim has expired after the
	// above check and someone else has claimed it.
	if err != nil && !errors.Is(err, storage.ErrValueVersionMismatch) {
		return fmt.Errorf("failed to release leadership claim: %w", err)
	}
	c.curr = nil

	return nil
}
