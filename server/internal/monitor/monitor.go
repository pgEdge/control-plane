package monitor

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type Check func(ctx context.Context) error

type Monitor struct {
	interval time.Duration
	ticker   *time.Ticker
	logger   zerolog.Logger
	checks   []Check
	done     chan struct{}
}

func NewMonitor(
	logger zerolog.Logger,
	interval time.Duration,
	checks ...Check,
) *Monitor {
	return &Monitor{
		interval: interval,
		logger:   logger,
		checks:   checks,
		done:     make(chan struct{}, 1),
	}
}

func (m *Monitor) Check(ctx context.Context) {
	for _, check := range m.checks {
		if err := check(ctx); err != nil {
			m.logger.Err(err).Msg("monitor failed to run check")
		}
	}
}

func (m *Monitor) Start(ctx context.Context) {
	m.ticker = time.NewTicker(m.interval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				m.ticker.Stop()
				return
			case <-m.done:
				return
			case <-m.ticker.C:
				m.Check(ctx)
			}
		}
	}()

	m.Check(ctx)
}

func (m *Monitor) Stop() {
	m.ticker.Stop()
	m.done <- struct{}{}
}
