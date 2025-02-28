package ipam

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
)

// TODO: This should be part of the swarm orchestrator and it should be scoped
// to the cohort ID.

const (
	bridgeAllocatorName   = "bridge_subnet_allocator"
	databaseAllocatorName = "database_subnet_allocator"
)

type Service struct {
	mu                sync.Mutex
	cfg               config.Config
	logger            zerolog.Logger
	store             *Store
	bridgeAllocator   *SubnetRange
	databaseAllocator *SubnetRange
}

func NewService(cfg config.Config, logger zerolog.Logger, store *Store) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
		store:  store,
	}
}

func (s *Service) Start(ctx context.Context) error {
	bridgeCIDR, err := netip.ParsePrefix(s.cfg.DockerSwarm.BridgeNetworksCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse bridge networks CIDR: %w", err)
	}
	bridgeAllocator, err := NewSubnetRange(SubnetRangeSpec{
		CIDR:       bridgeCIDR,
		SubnetBits: s.cfg.DockerSwarm.BridgeNetworksSubnetBits,
	})
	if err != nil {
		return fmt.Errorf("failed to create bridge subnet allocator: %w", err)
	}
	s.bridgeAllocator = bridgeAllocator
	if err := s.restoreAllocator(ctx, s.bridgeAllocator, bridgeAllocatorName); err != nil {
		return fmt.Errorf("failed to restore bridge subnet allocator from storage: %w", err)
	}

	databaseCIDR, err := netip.ParsePrefix(s.cfg.DockerSwarm.DatabaseNetworksCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse database networks CIDR: %w", err)
	}
	databaseAllocator, err := NewSubnetRange(SubnetRangeSpec{
		CIDR:       databaseCIDR,
		SubnetBits: s.cfg.DockerSwarm.DatabaseNetworksSubnetBits,
	})
	if err != nil {
		return fmt.Errorf("failed to create database subnet allocator: %w", err)
	}
	s.databaseAllocator = databaseAllocator
	if err := s.restoreAllocator(ctx, s.databaseAllocator, databaseAllocatorName); err != nil {
		return fmt.Errorf("failed to restore database subnet allocator from storage: %w", err)
	}

	return nil
}

func (s *Service) restoreAllocator(ctx context.Context, allocator *SubnetRange, name string) error {
	exists, err := s.store.ExistsByKey(name).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subnet allocator spec exists: %w", err)
	}
	if !exists {
		return nil
	}
	stored, err := s.store.GetByKey(name).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get subnet allocator spec from storage: %w", err)
	}
	if err := allocator.Restore(stored.Spec, stored.Snapshot); err != nil {
		// An error can happen here if the config has changed. In this case, we'll
		// continue without restoring the allocator and overwrite the old allocator
		// on the next allocation.
		s.logger.Warn().
			Err(err).
			Msg("failed to restore subnet allocator")
	}
	return nil
}

func (s *Service) AllocateBridgeSubnet(ctx context.Context) (netip.Prefix, error) {
	return s.allocateSubnet(ctx, s.bridgeAllocator, bridgeAllocatorName)
}

func (s *Service) AllocateDatabaseSubnet(ctx context.Context) (netip.Prefix, error) {
	return s.allocateSubnet(ctx, s.databaseAllocator, databaseAllocatorName)
}

func (s *Service) allocateSubnet(ctx context.Context, allocator *SubnetRange, name string) (netip.Prefix, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnet, err := allocator.AllocateNext()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to allocate subnet: %w", err)
	}

	spec, snapshot, err := allocator.Snapshot()
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to snapshot subnet allocator: %w", err)
	}

	if err := s.store.Put(&StoredSubnetRange{
		Name:     name,
		Spec:     spec,
		Snapshot: snapshot,
	}).Exec(ctx); err != nil {
		return netip.Prefix{}, fmt.Errorf("failed to store subnet allocator: %w", err)
	}

	return subnet, nil
}
