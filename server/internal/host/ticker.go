package host

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
)

var _ do.Shutdownable = (*UpdateTicker)(nil)

const UpdateStatusInterval = 15 * time.Second

type UpdateTicker struct {
	t      *time.Ticker
	logger zerolog.Logger
	svc    *Service
	done   chan struct{}
}

func NewUpdateTicker(logger zerolog.Logger, svc *Service) *UpdateTicker {
	h := &UpdateTicker{
		logger: logger,
		svc:    svc,
		done:   make(chan struct{}, 1),
	}

	return h
}

func (u *UpdateTicker) Start(ctx context.Context) {
	u.t = time.NewTicker(UpdateStatusInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-u.done:
				return
			case <-u.t.C:
				if err := u.svc.UpdateHostStatus(ctx); err != nil {
					u.logger.Err(err).Msg("failed to update host")
				}
			}
		}
	}()

	// Run the first update immediately
	if err := u.svc.UpdateHostStatus(ctx); err != nil {
		u.logger.Err(err).Msg("failed to update host")
	}
}

func (u *UpdateTicker) Shutdown() error {
	u.t.Stop()
	u.done <- struct{}{}

	return nil
}
