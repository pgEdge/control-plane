package client

import (
	"net/http"
	"net/url"
	"time"

	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	goahttp "goa.design/goa/v3/http"
)

// defaultHTTPTimeout bounds every request through an HTTPServerConfig's
// client, matching NewMQTTServerConfig's own default maxWait -- without one,
// an unresponsive server blocks the caller indefinitely.
const defaultHTTPTimeout = 30 * time.Second

// HTTPServerConfig configures a connection to a Control Plane server via HTTP.
type HTTPServerConfig struct {
	url *url.URL
}

// NewHTTPServerConfig creates a new HTTPServerConfig with the given URL.
func NewHTTPServerConfig(hostID string, url *url.URL) ServerConfig {
	return ServerConfig{
		hostID: hostID,
		http: &HTTPServerConfig{
			url: url,
		},
	}
}

func (c *HTTPServerConfig) newClient() *client.Client {
	httpClient := &http.Client{Timeout: defaultHTTPTimeout}
	return client.NewClient(
		c.url.Scheme,
		c.url.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)
}
