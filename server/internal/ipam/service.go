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

const (
	maxRetries        = 3
	releaseMaxRetries = 2
)

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

// ReleaseSubnet releases the subnet back to the pool. Best-effort: logs warnings and returns nil so callers are not failed.
func (s *Service) ReleaseSubnet(ctx context.Context, prefix netip.Prefix, bits int, subnet netip.Prefix) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !subnet.IsValid() || !prefix.IsValid() {
		s.logger.Warn().
			Str("subnet", subnet.String()).
			Str("prefix", prefix.String()).
			Msg("release subnet skipped: invalid subnet or prefix")
		return nil
	}

	var lastErr error
	for retries := releaseMaxRetries; retries >= 0; retries-- {
		lastErr = s.releaseSubnet(ctx, prefix, bits, subnet)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, storage.ErrValueVersionMismatch) {
			// Non-retryable: log and succeed so delete is not blocked
			s.logger.Warn().
				Err(lastErr).
				Str("subnet", subnet.String()).
				Str("prefix", prefix.String()).
				Msg("release subnet failed (non-fatal)")
			return nil
		}
	}
	s.logger.Warn().
		Err(lastErr).
		Str("subnet", subnet.String()).
		Str("prefix", prefix.String()).
		Msg("release subnet failed after retries (non-fatal)")
	return nil
}

// releaseSubnet performs one release attempt. Returns ErrValueVersionMismatch on conflict for retry.
func (s *Service) releaseSubnet(ctx context.Context, prefix netip.Prefix, bits int, subnet netip.Prefix) error {
	stored, err := s.store.GetByKey(prefix.String()).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		s.logger.Warn().
			Str("prefix", prefix.String()).
			Str("subnet", subnet.String()).
			Msg("release subnet skipped: no allocator for range")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get subnet allocator from storage: %w", err)
	}

	allocator, err := NewSubnetRange(SubnetRangeSpec{
		CIDR:       prefix,
		SubnetBits: bits,
	})
	if err != nil {
		return fmt.Errorf("failed to create subnet allocator: %w", err)
	}

	if err := allocator.Restore(stored.Spec, stored.Snapshot); err != nil {
		s.logger.Warn().
			Err(err).
			Str("prefix", prefix.String()).
			Str("subnet", subnet.String()).
			Msg("release subnet skipped: failed to restore allocator (e.g. config changed)")
		return nil
	}

	if !allocator.Contains(subnet) {
		s.logger.Warn().
			Str("subnet", subnet.String()).
			Str("prefix", prefix.String()).
			Msg("release subnet skipped: subnet outside or mismatched with configured range")
		return nil
	}

	if err := allocator.Release(subnet); err != nil {
		s.logger.Warn().
			Err(err).
			Str("subnet", subnet.String()).
			Str("prefix", prefix.String()).
			Msg("release subnet skipped: release failed (e.g. subnet not allocated)")
		return nil
	}

	spec, snapshot, err := allocator.Snapshot()
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("subnet", subnet.String()).
			Msg("release subnet skipped: failed to snapshot allocator")
		return nil
	}

	stored.Spec = spec
	stored.Snapshot = snapshot

	if err := s.store.Update(stored).Exec(ctx); err != nil {
		if errors.Is(err, storage.ErrValueVersionMismatch) {
			return err
		}
		s.logger.Warn().
			Err(err).
			Str("subnet", subnet.String()).
			Msg("release subnet skipped: failed to store allocator")
		return nil
	}

	return nil
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
