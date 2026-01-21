package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	goahttp "goa.design/goa/v3/http"

	"github.com/pgEdge/control-plane/server/internal/api/apiv1"
	"github.com/pgEdge/control-plane/server/internal/config"
)

var _ do.Shutdownable = (*Server)(nil)

type Server struct {
	logger  zerolog.Logger
	started bool
	cfg     config.Config
	v1Svc   *apiv1.Service
	http    *httpServer
	mqtt    *mqttServer
	errCh   chan error
}

func NewServer(
	cfg config.Config,
	logger zerolog.Logger,
	v1Svc *apiv1.Service,
) *Server {
	mux := goahttp.NewMuxer()
	mux.Handle("GET", "/", func(w http.ResponseWriter, r *http.Request) {
		// Direct clients to the v1 API spec by default
		w.Header().Add("Link", `</v1/openapi.json>; rel="service-desc"`)
		w.WriteHeader(204)
	})

	if cfg.ProfilingEnabled {
		mountPprofHandlers(mux)
	}

	// Mount all the v1 handlers
	v1Svc.Mount(mux)

	logger = logger.With().
		Str("component", "api_server").
		Logger()

	handler := addMiddleware(logger, mux)

	var (
		httpSvr *httpServer
		mqttSvr *mqttServer
	)

	if cfg.HTTP.Enabled {
		httpSvr = newHTTPServer(cfg.HTTP, handler, logger)
	}
	if cfg.MQTT.Enabled {
		mqttSvr = newMQTTServer(cfg.MQTT, handler, logger)
	}

	return &Server{
		logger: logger,
		cfg:    cfg,
		v1Svc:  v1Svc,
		http:   httpSvr,
		mqtt:   mqttSvr,
		errCh:  make(chan error, 2),
	}
}

func (s *Server) ServePreInit(ctx context.Context) error {
	s.logger.Debug().Msg("serving pre-init handlers")

	if err := s.v1Svc.UsePreInitHandlers(); err != nil {
		return fmt.Errorf("failed to set v1 api to use pre-init handlers: %w", err)
	}

	s.serve(ctx)

	return nil
}

func (s *Server) ServePostInit(ctx context.Context) error {
	s.logger.Debug().Msg("serving post-init handlers")

	if err := s.v1Svc.UsePostInitHandlers(); err != nil {
		return fmt.Errorf("failed to set v1 api to use post-init handlers: %w", err)
	}

	s.serve(ctx)

	return nil
}

// HandleInitializationError takes an error that occurred during initialization
// and propagates it to the handlers so that it can be returned to clients that
// are waiting on a join or init cluster response.
func (s *Server) HandleInitializationError(err error) {
	s.v1Svc.HandleInitializationError(err)
}

func (s *Server) serve(ctx context.Context) {
	if s.started {
		return
	}

	var errChs []chan error

	if s.http != nil {
		s.http.start()
		errChs = append(errChs, s.http.errCh)
	}
	if s.mqtt != nil {
		s.mqtt.start(ctx)
		errChs = append(errChs, s.mqtt.errCh)
	}

	s.started = true

	for _, c := range errChs {
		go func(c chan error) {
			select {
			case <-ctx.Done():
				return
			case err := <-c:
				s.errCh <- err
			}
		}(c)
	}
}

func (s *Server) Shutdown() error {
	s.logger.Debug().Msg("shutting down api server")

	ctx := context.Background()

	var errs []error

	if s.http != nil {
		errs = append(errs, s.http.stop(ctx))
	}
	if s.mqtt != nil {
		errs = append(errs, s.mqtt.stop(ctx))
	}

	return errors.Join(errs...)
}

func (s *Server) Error() <-chan error {
	return s.errCh
}
