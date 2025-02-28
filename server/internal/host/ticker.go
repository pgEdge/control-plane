package host

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const UpdateStatusInterval = 15 * time.Second

type UpdateTicker struct {
	t      *time.Ticker
	logger zerolog.Logger
	done   chan struct{}
}

func NewUpdateTicker(logger zerolog.Logger) *UpdateTicker {
	h := &UpdateTicker{
		logger: logger,
		done:   make(chan struct{}, 1),
	}

	return h
}

func (u *UpdateTicker) Start(ctx context.Context, svc *Service) {
	u.t = time.NewTicker(UpdateStatusInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-u.done:
				return
			case <-u.t.C:
				if err := svc.UpdateHostStatus(ctx); err != nil {
					u.logger.Err(err).Msg("failed to update host")
				}
			}
		}
	}()

	// Run the first update immediately
	if err := svc.UpdateHostStatus(ctx); err != nil {
		u.logger.Err(err).Msg("failed to update host")
	}
}

func (u *UpdateTicker) Stop() {
	u.t.Stop()
	u.done <- struct{}{}
}
