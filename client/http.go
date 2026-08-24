package client

import (
	"net/http"
	"net/url"
	"time"

	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/client"
	goahttp "goa.design/goa/v3/http"
)

// defaultHTTPTimeout bounds every request made through an HTTPServerConfig's
// client, matching NewMQTTServerConfig's own default maxWait. Every call this
// package exposes (RemoveHost, CreateDatabase, etc.) only ever kicks off
// async work and returns -- actual long-running operations are tracked
// separately via task polling, which takes its own explicit timeout -- so no
// legitimate call should ever need more than this to complete. Without a
// bound here, an unresponsive server (or a stalled connection to one) leaves
// the caller blocked indefinitely, since neither http.DefaultClient nor a
// bare context.Context from something like testing.T.Context() impose one
// on their own.
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
