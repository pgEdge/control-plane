package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
)

type httpServer struct {
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
		logger: logger,
		errCh:  make(chan error, 1),
		server: &http.Server{
			Addr:    cfg.HostPort,
			Handler: handler,
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
	defer close(s.errCh)

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("error while shutting down http server: %w", err)
	}
	return nil
}
