package mqtt_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/pgEdge/control-plane/mqtt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPRequestResponse(t *testing.T) {
	ctx, broker := setupTestBroker(t)

	t.Run("basic", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				fmt.Fprintf(w, "Hello, %s!", body)
			default:
				fmt.Fprintf(w, "incorrect method %q", r.Method)
				w.WriteHeader(400)
			}
		})

		server, err := mqtt.NewHTTPServer(mqtt.HTTPServerConfig{
			Topic: t.Name(),
			Broker: mqtt.BrokerConfig{
				URL: broker.URL(),
			},
			Handler: mux,
		})
		require.NoError(t, err)
		require.NoError(t, server.Start(ctx))

		defer server.Stop(ctx)

		doer, err := mqtt.NewHTTPDoer(mqtt.HTTPDoerConfig{
			Topic: t.Name(),
			Broker: mqtt.BrokerConfig{
				URL: broker.URL(),
			},
		})
		require.NoError(t, err)
		require.NoError(t, doer.Connect(ctx))

		defer doer.Disconnect(ctx)

		req, err := http.NewRequestWithContext(ctx, "POST", "/hello", bytes.NewBuffer([]byte("test")))
		require.NoError(t, err)

		resp, err := doer.Do(req)
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "Hello, test!", string(body))

		req, err = http.NewRequestWithContext(ctx, "GET", "/not-found", nil)
		require.NoError(t, err)

		resp, err = doer.Do(req)
		require.NoError(t, err)
		assert.Equal(t, 404, resp.StatusCode)
	})

	t.Run("message retain", func(t *testing.T) {
		// validates that the client can be configured in a way that its
		// requests will be retained even if the server is down.
		doer, err := mqtt.NewHTTPDoer(mqtt.HTTPDoerConfig{
			Topic: t.Name(),
			Broker: mqtt.BrokerConfig{
				URL: broker.URL(),
			},
			MaxWait: time.Second * 30,
			QoS:     2,
			Retain:  true,
		})
		require.NoError(t, err)
		require.NoError(t, doer.Connect(ctx))

		defer doer.Disconnect(ctx)

		req, err := http.NewRequestWithContext(ctx, "POST", "/hello", bytes.NewBuffer([]byte("test")))
		require.NoError(t, err)

		respChan := make(chan *http.Response)

		go func() {
			resp, err := doer.Do(req)
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode)

			respChan <- resp
		}()

		mux := http.NewServeMux()
		mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				fmt.Fprintf(w, "Hello, %s!", body)
			default:
				fmt.Fprintf(w, "incorrect method %q", r.Method)
				w.WriteHeader(400)
			}
		})

		server, err := mqtt.NewHTTPServer(mqtt.HTTPServerConfig{
			Topic: t.Name(),
			Broker: mqtt.BrokerConfig{
				URL: broker.URL(),
			},
			Handler: mux,
		})
		require.NoError(t, err)
		require.NoError(t, server.Start(ctx))

		defer server.Stop(ctx)

		resp := <-respChan
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "Hello, test!", string(body))
	})
}
