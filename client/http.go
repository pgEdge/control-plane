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
	cli *http.Client
}

type HTTPServerOption func(cfg *HTTPServerConfig)

// NewHTTPServerConfig creates a new HTTPServerConfig with the given URL.
func NewHTTPServerConfig(hostID string, url *url.URL, opts ...HTTPServerOption) ServerConfig {
	cfg := &HTTPServerConfig{
		url: url,
		cli: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return ServerConfig{
		hostID: hostID,
		http:   cfg,
	}
}

func (c *HTTPServerConfig) newClient() *client.Client {
	return client.NewClient(
		c.url.Scheme,
		c.url.Host,
		c.cli,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)
}

func WithHTTPClient(cli *http.Client) HTTPServerOption {
	return func(cfg *HTTPServerConfig) {
		cfg.cli = cli
	}
}
