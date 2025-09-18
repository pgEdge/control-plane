package client

import (
	"time"

	goahttp "goa.design/goa/v3/http"

	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	"github.com/pgEdge/control-plane/mqtt"
)

// MQTTServerConfig configures a connection to a Control Plane server via MQTT.
type MQTTServerConfig struct {
	endpoint mqtt.Endpoint
	topic    string
	maxWait  time.Duration
}

func NewMQTTServerConfig(hostID string, endpoint mqtt.Endpoint, topic string, maxWait time.Duration) ServerConfig {
	if maxWait == 0 {
		maxWait = 30 * time.Second // Default max wait time
	}
	cfg := &MQTTServerConfig{
		endpoint: endpoint,
		topic:    topic,
		maxWait:  maxWait,
	}

	return ServerConfig{
		hostID: hostID,
		mqtt:   cfg,
	}
}

func (c *MQTTServerConfig) newClient() (*client.Client, error) {
	mqttDoer := mqtt.NewHTTPDoer(mqtt.HTTPDoerConfig{
		Topic:    c.topic,
		Endpoint: c.endpoint,
		MaxWait:  c.maxWait,
	})
	return client.NewClient(
		"",
		"",
		mqttDoer,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	), nil
}
