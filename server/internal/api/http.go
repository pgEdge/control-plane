package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
)

type httpServer struct {
	cfg    config.HTTP
	logger zerolog.Logger
	server *http.Server
	errCh  chan error
}

func newHTTPServer(
	cfg config.HTTP,
	handler http.Handler,
	logger zerolog.Logger,
) *httpServer {
	return &httpServer{
		cfg:    cfg,
		logger: logger,
		errCh:  make(chan error, 1),
		server: &http.Server{
			Handler: handler,
			Addr:    fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port),
		},
	}
}

func (s *httpServer) start() {
	go func() {
		s.logger.Info().
			Str("host_port", s.server.Addr).
			Msg("starting http server")

		if err := s.server.ListenAndServe(); err != nil {
			s.errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()
}

func (s *httpServer) stop(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("error while shutting down http server: %w", err)
	}
	return nil
}
