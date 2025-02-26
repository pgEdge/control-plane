package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/rs/zerolog"
	goahttp "goa.design/goa/v3/http"

	"github.com/pgEdge/control-plane/api"
	controlplane "github.com/pgEdge/control-plane/api/gen/control_plane"
	"github.com/pgEdge/control-plane/api/gen/http/control_plane/server"
	"github.com/pgEdge/control-plane/server/internal/config"
)

type Server struct {
	cfg   config.Config
	svc   controlplane.Service
	http  *httpServer
	mqtt  *mqttServer
	errCh chan error
}

func NewServer(cfg config.Config, logger zerolog.Logger, svc controlplane.Service) *Server {
	mux := goahttp.NewMuxer()
	mux.Handle("GET", "/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Link", `</openapi.json>; rel="service-desc"`)
		w.WriteHeader(204)
	})

	// svc := NewServiceManager(cfg, logger, e)
	endpoints := controlplane.NewEndpoints(svc)
	dec := goahttp.RequestDecoder
	enc := goahttp.ResponseEncoder
	fs := http.FS(api.OpenAPISpecFS)
	svr := server.New(endpoints, mux, dec, enc, nil, nil, fs)
	server.Mount(mux, svr)

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
		svc:   svc,
		http:  httpSvr,
		mqtt:  mqttSvr,
		errCh: make(chan error, 2),
	}
}

func (s *Server) Start(ctx context.Context) {
	var errChs []chan error

	if s.http != nil {
		s.http.start()
		errChs = append(errChs, s.http.errCh)
	}
	if s.mqtt != nil {
		s.mqtt.start(ctx)
		errChs = append(errChs, s.mqtt.errCh)
	}

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
