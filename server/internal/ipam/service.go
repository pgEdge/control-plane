package ipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

const maxRetries = 3

// TODO: This should be part of the swarm orchestrator and it should be scoped
// to the cohort ID.

type Service struct {
	mu     sync.Mutex
	logger zerolog.Logger
	store  *Store
}

func NewService(logger zerolog.Logger, store *Store) *Service {
	return &Service{
		logger: logger,
		store:  store,
	}
}

func (s *Service) AllocateSubnet(ctx context.Context, prefix netip.Prefix, bits int) (netip.Prefix, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.allocateSubnet(ctx, prefix, bits, maxRetries)
}

func (s *Service) allocateSubnet(ctx context.Context, prefix netip.Prefix, bits int, retriesRemaining int) (netip.Prefix, error) {
	if retriesRemaining < 1 {
		// This can happen if there's too much contention for this subnet range
		// across multiple hosts.
		return netip.Prefix{}, errors.New("failed to allocate subnet: exhausted retries")
	}

	allocator, err := NewSubnetRange(SubnetRangeSpec{
		CIDR:       prefix,
		SubnetBits: bits,
	})
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to create subnet allocator: %w", err)
	}

	stored, err := s.restoreAllocator(ctx, allocator, prefix.String())
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to restore subnet allocator from storage: %w", err)
	}

	subnet, err := allocator.AllocateNext()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to allocate subnet: %w", err)
	}

	spec, snapshot, err := allocator.Snapshot()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to snapshot subnet allocator: %w", err)
	}

	stored.Spec = spec
	stored.Snapshot = snapshot

	err = s.store.Update(stored).Exec(ctx)
	if errors.Is(err, storage.ErrValueVersionMismatch) {
		return s.allocateSubnet(ctx, prefix, bits, retriesRemaining-1)
	} else if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to store subnet allocator: %w", err)
	}

	return subnet, nil
}

func (s *Service) restoreAllocator(ctx context.Context, allocator *SubnetRange, name string) (*StoredSubnetRange, error) {
	stored, err := s.store.GetByKey(name).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return &StoredSubnetRange{
			Name: name,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get subnet allocator spec from storage: %w", err)
	}
	if err := allocator.Restore(stored.Spec, stored.Snapshot); err != nil {
		// An error can happen here if the config has changed. In this case, we'll
		// continue without restoring the allocator and overwrite the old allocator
		// on the next allocation.
		s.logger.Warn().
			Err(err).
			Msg("failed to restore subnet allocator")
	}
	return stored, nil
}
