package monitor

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/host"
)

type HostMonitor struct {
	monitor *Monitor
	svc     *host.Service
}

func NewHostMonitor(
	logger zerolog.Logger,
	svc *host.Service,
) *HostMonitor {
	m := &HostMonitor{
		svc: svc,
	}
	m.monitor = NewMonitor(
		logger,
		host.HostMonitorRefreshInterval,
		m.update,
	)
	return m
}

func (m *HostMonitor) Start(ctx context.Context) {
	m.monitor.Start(ctx)
}

func (m *HostMonitor) Stop() {
	m.monitor.Stop()
}

func (m *HostMonitor) update(ctx context.Context) error {
	if err := m.svc.UpdateHost(ctx); err != nil {
		return fmt.Errorf("failed to update host: %w", err)
	}
	if err := m.svc.UpdateHostStatus(ctx); err != nil {
		return fmt.Errorf("failed to update host status: %w", err)
	}

	return nil
}
