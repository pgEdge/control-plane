package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/election"
)

type DatabasesMonitor struct {
	monitor   *Monitor
	svc       *database.Service
	candidate *election.Candidate
}

func NewDatabasesMonitor(
	logger zerolog.Logger,
	svc *database.Service,
	candidate *election.Candidate,
	cfg config.Config,
) *DatabasesMonitor {
	m := &DatabasesMonitor{
		svc:       svc,
		candidate: candidate,
	}
	interval := time.Duration(cfg.DatabasesMonitorIntervalSeconds) * time.Second
	m.monitor = NewMonitor(logger, interval, m.update)
	return m
}

func (m *DatabasesMonitor) Start(ctx context.Context) error {
	if err := m.candidate.Start(ctx); err != nil {
		return fmt.Errorf("failed to start candidate: %w", err)
	}
	m.monitor.Start(ctx)

	return nil
}

func (m *DatabasesMonitor) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), electionTTL/3)
	defer cancel()

	m.monitor.Stop()
	if err := m.candidate.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop candidate: %w", err)
	}

	return nil
}

func (m *DatabasesMonitor) update(ctx context.Context) error {
	if !m.candidate.IsLeader() {
		return nil
	}
	if err := m.svc.ReconcileAllDatabaseVersions(ctx); err != nil {
		return fmt.Errorf("failed to reconcile database versions: %w", err)
	}

	return nil
}
