package ports

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// Using a higher number of retries here because we expect high contention in
// some topologies.
const maxRetries = 6
const sleepBase = 100 * time.Millisecond
const sleepJitterMS = 100

// PortChecker reports whether a port is available for binding.
type PortChecker func(port int) bool

// DefaultPortChecker returns true if the given TCP port is not currently bound
// by any process on the local host.
func DefaultPortChecker(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

type Service struct {
	mu          sync.Mutex
	cfg         config.Config
	logger      zerolog.Logger
	store       *Store
	portChecker PortChecker
	hostSvc     *host.Service
}

func NewService(
	cfg config.Config,
	loggerFactory *logging.Factory,
	store *Store,
	portChecker PortChecker,
	hostSvc *host.Service,
) *Service {
	return &Service{
		cfg:         cfg,
		logger:      loggerFactory.Logger(logging.ComponentPortsService),
		store:       store,
		portChecker: portChecker,
		hostSvc:     hostSvc,
	}
}

// AllocatePort allocates the next available port in [min, max] that is not
// already recorded in the persistent range and is not currently bound on the
// local host.
func (s *Service) AllocatePort(ctx context.Context, hostID string) (int, error) {
	if hostID != s.cfg.HostID {
		return 0, fmt.Errorf("cannot allocate a new port for another host, this host='%s', requested host='%s'", s.cfg.HostID, hostID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.allocatePort(ctx, maxRetries)
}

func (s *Service) allocatePort(ctx context.Context, retriesRemaining int) (int, error) {
	if retriesRemaining < 1 {
		// This can happen if there's too much contention for this port range
		// across multiple hosts.
		return 0, errors.New("failed to allocate port: exhausted retries")
	}

	logger := s.logger.With().
		Int("retries_remaining", retriesRemaining).
		Logger()

	logger.Debug().
		Int("range_min", s.cfg.RandomPorts.Min).
		Int("range_max", s.cfg.RandomPorts.Max).
		Msg("attempting to allocate a random port")

	name := s.cfg.ClientAddress()
	min := s.cfg.RandomPorts.Min
	max := s.cfg.RandomPorts.Max

	r, err := NewPortRange(min, max)
	if err != nil {
		return 0, fmt.Errorf("failed to create port allocator: %w", err)
	}

	stored, err := s.restoreAllocator(ctx, r, name)
	if err != nil {
		return 0, fmt.Errorf("failed to restore port allocator from storage: %w", err)
	}

	port, err := s.allocateAvailablePort(r)
	if err != nil {
		return 0, err
	}

	stored.Snapshot = r.Snapshot()

	err = s.store.Update(stored).Exec(ctx)
	if errors.Is(err, storage.ErrValueVersionMismatch) {
		sleepDuration := addJitter(sleepBase, sleepJitterMS)

		logger.Debug().
			Int64("sleep_milliseconds", sleepDuration.Milliseconds()).
			Msg("encountered conflict. sleeping before reattempting.")

		time.Sleep(sleepDuration)

		return s.allocatePort(ctx, retriesRemaining-1)
	} else if err != nil {
		return 0, fmt.Errorf("failed to store port allocator: %w", err)
	}

	logger.Debug().
		Int("port", port).
		Msg("successfully allocated random port")

	return port, nil
}

func (s *Service) ReleasePortIfDefined(ctx context.Context, hostID string, ports ...*int) error {
	defined := make([]int, 0, len(ports))
	for _, port := range ports {
		if p := utils.FromPointer(port); p != 0 {
			defined = append(defined, p)
		}
	}

	return s.ReleasePort(ctx, hostID, defined...)
}

// ReleasePort releases the given port back to the pool, persisting the updated
// state to storage.
func (s *Service) ReleasePort(ctx context.Context, hostID string, ports ...int) error {
	host, err := s.hostSvc.GetHost(ctx, hostID)
	if err != nil {
		return fmt.Errorf("failed to get host '%s': %w", hostID, err)
	}
	if len(host.ClientAddresses) == 0 {
		return fmt.Errorf("host '%s' has no client addresses", hostID)
	}

	name := host.ClientAddresses[0]

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.releasePort(ctx, name, ports, maxRetries)
}

func (s *Service) releasePort(ctx context.Context, name string, ports []int, retriesRemaining int) error {
	if retriesRemaining < 1 {
		return errors.New("failed to release port: exhausted retries")
	}

	logger := s.logger.With().
		Int("retries_remaining", retriesRemaining).
		Ints("ports", ports).
		Logger()

	logger.Debug().Msg("attempting to release port")

	r, err := NewPortRange(s.cfg.RandomPorts.Min, s.cfg.RandomPorts.Max)
	if err != nil {
		return fmt.Errorf("failed to create port allocator: %w", err)
	}

	stored, err := s.restoreAllocator(ctx, r, name)
	if err != nil {
		return fmt.Errorf("failed to restore port allocator from storage: %w", err)
	}

	for _, port := range ports {
		if err := r.Release(port); err != nil {
			return fmt.Errorf("failed to release port: %w", err)
		}
	}

	stored.Snapshot = r.Snapshot()

	err = s.store.Update(stored).Exec(ctx)
	if errors.Is(err, storage.ErrValueVersionMismatch) {
		sleepDuration := addJitter(sleepBase, sleepJitterMS)

		logger.Debug().
			Int64("sleep_milliseconds", sleepDuration.Milliseconds()).
			Msg("encountered conflict. sleeping before reattempting.")

		time.Sleep(sleepDuration)

		return s.releasePort(ctx, name, ports, retriesRemaining-1)
	} else if err != nil {
		return fmt.Errorf("failed to store port allocator: %w", err)
	}

	logger.Debug().Msg("successfully released port")

	return nil
}

// allocateAvailablePort calls AllocateNext until it finds a port that passes
// the OS availability check. Ports that are occupied by external processes are
// kept as allocated in the range so they are not retried on future calls.
func (s *Service) allocateAvailablePort(r *PortRange) (int, error) {
	for {
		port, err := r.AllocateNext()
		if err != nil {
			return 0, fmt.Errorf("failed to allocate port: %w", err)
		}
		if s.portChecker(port) {
			return port, nil
		}
		s.logger.Debug().
			Int("port", port).
			Msg("port is in use by another process, skipping")
	}
}

func (s *Service) restoreAllocator(ctx context.Context, r *PortRange, name string) (*StoredPortRange, error) {
	stored, err := s.store.GetByKey(name).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return &StoredPortRange{
			Name: name,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get port allocator spec from storage: %w", err)
	}
	if err := r.Restore(stored.Snapshot); err != nil {
		// An error can happen here if the config has changed. In this case,
		// continue without restoring and overwrite the old allocator on the
		// next allocation.
		s.logger.Warn().
			Err(err).
			Msg("failed to restore port allocator")
	}
	return stored, nil
}

func addJitter(base time.Duration, jitterMS uint) time.Duration {
	jitter := time.Duration(rand.N(jitterMS)) * time.Millisecond
	return base + jitter
}
