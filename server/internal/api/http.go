package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"

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
	if s.cfg.CACert == "" {
		s.listenAndServe()
	} else {
		s.listenAndServeTLS()
	}
}

func (s *httpServer) listenAndServe() {
	s.logger.Info().
		Str("host_port", s.server.Addr).
		Msg("starting http server")

	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			s.errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()
}

func (s *httpServer) listenAndServeTLS() {
	s.logger.Info().
		Str("host_port", s.server.Addr).
		Msg("starting https server")

	go func() {
		rootCA, err := os.ReadFile(s.cfg.CACert)
		if err != nil {
			s.errCh <- fmt.Errorf("failed to read CA cert: %w", err)
			return
		}

		certPool := x509.NewCertPool()
		if ok := certPool.AppendCertsFromPEM(rootCA); !ok {
			s.errCh <- errors.New("failed to use CA cert")
			return
		}

		s.server.TLSConfig = &tls.Config{
			RootCAs:    certPool,
			ClientCAs:  certPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
			MinVersion: tls.VersionTLS13,
		}

		if err := s.server.ListenAndServeTLS(s.cfg.ServerCert, s.cfg.ServerKey); err != nil {
			s.errCh <- fmt.Errorf("https server error: %w", err)
		}
	}()
}

func (s *httpServer) stop(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("error while shutting down http server: %w", err)
	}
	return nil
}
