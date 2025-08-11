package client

import (
	"net/http"
	"net/url"

	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	goahttp "goa.design/goa/v3/http"
)

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
	return client.NewClient(
		c.url.Scheme,
		c.url.Host,
		http.DefaultClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)
}
