package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	goahttp "goa.design/goa/v3/http"

	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/mqtt"
)

// MQTTServerConfig configures a connection to a Control Plane server via MQTT.
type MQTTServerConfig struct {
	brokerURL *url.URL
	topic     string
	clientID  string
	username  string
	password  string
	maxWait   time.Duration
}

func NewMQTTServerConfig(hostID string, brokerURL *url.URL, topic string, opts ...MQTTServerOption) ServerConfig {
	cfg := &MQTTServerConfig{
		brokerURL: brokerURL,
		topic:     topic,
		maxWait:   30 * time.Second, // Default max wait time
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return ServerConfig{
		mqtt: cfg,
	}
}

type MQTTServerOption func(cfg *MQTTServerConfig)

// WithClientID sets the client ID for the MQTT connection.
func WithClientID(clientID string) MQTTServerOption {
	return func(cfg *MQTTServerConfig) {
		cfg.clientID = clientID
	}
}

// WithUsername sets the username for the MQTT connection.
func WithUsername(username string) MQTTServerOption {
	return func(cfg *MQTTServerConfig) {
		cfg.username = username
	}
}

// WithPassword sets the password for the MQTT connection.
func WithPassword(password string) MQTTServerOption {
	return func(cfg *MQTTServerConfig) {
		cfg.password = password
	}
}

// WithMaxWait sets the maximum wait time for MQTT operations.
func WithMaxWait(maxWait time.Duration) MQTTServerOption {
	return func(cfg *MQTTServerConfig) {
		cfg.maxWait = maxWait
	}
}

func (c *MQTTServerConfig) newClient(ctx context.Context) (*client.Client, error) {
	mqttDoer := mqtt.NewHTTPDoer(mqtt.HTTPDoerConfig{
		Topic: c.topic,
		Broker: mqtt.BrokerConfig{
			URL:      c.brokerURL.String(),
			ClientID: c.clientID,
			Username: c.username,
			Password: c.password,
		},
		MaxWait: c.maxWait,
	})
	if err := mqttDoer.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect mqtt client: %w", err)
	}
	return client.NewClient(
		"",
		"",
		mqttDoer,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	), nil
}
