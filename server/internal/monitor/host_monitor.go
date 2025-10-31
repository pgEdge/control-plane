package monitor

import (
	"context"

	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/rs/zerolog"
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
		statusMonitorInterval,
		m.checkStatus,
	)
	return m
}

func (m *HostMonitor) Start(ctx context.Context) {
	m.monitor.Start(ctx)
}

func (m *HostMonitor) Stop() {
	m.monitor.Stop()
}

func (m *HostMonitor) checkStatus(ctx context.Context) error {
	return m.svc.UpdateHostStatus(ctx)

}
