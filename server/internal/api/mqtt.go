package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pgEdge/control-plane/mqtt"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
)

type mqttServer struct {
	cfg    config.MQTT
	server *mqtt.HTTPServer
	logger zerolog.Logger
	errCh  chan error
}

func newMQTTServer(
	cfg config.MQTT,
	handler http.Handler,
	logger zerolog.Logger,
) *mqttServer {
	server := mqtt.NewHTTPServer(mqtt.HTTPServerConfig{
		Topic:   cfg.Topic,
		Logger:  &logger,
		Handler: handler,
		Broker: mqtt.BrokerConfig{
			URL:      cfg.BrokerURL,
			ClientID: cfg.ClientID,
			Username: cfg.Username,
			Password: cfg.Password,
		},
	})
	return &mqttServer{
		cfg:    cfg,
		server: server,
		logger: logger,
		errCh:  make(chan error, 1),
	}
}

func (s *mqttServer) start(ctx context.Context) {
	s.logger.Info().
		Str("broker_url", s.cfg.BrokerURL).
		Msg("starting mqtt server")

	if err := s.server.Start(ctx); err != nil {
		s.errCh <- fmt.Errorf("error while starting mqtt server: %w", err)
	}
}

func (s *mqttServer) stop(ctx context.Context) error {
	defer close(s.errCh)

	if err := s.server.Stop(ctx); err != nil {
		return fmt.Errorf("error while stopping mqtt server: %w", err)
	}
	return nil
}
