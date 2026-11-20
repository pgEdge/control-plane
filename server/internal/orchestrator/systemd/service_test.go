package systemd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceServing(t *testing.T) {
	serverPort := func(t *testing.T, status int) int {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)
		_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
		require.NoError(t, err)
		port, err := strconv.Atoi(portStr)
		require.NoError(t, err)
		return port
	}

	t.Run("ok", func(t *testing.T) {
		assert.NoError(t, serviceServing(t.Context(), "postgrest", serverPort(t, http.StatusOK)))
	})
	t.Run("client error still means serving", func(t *testing.T) {
		assert.NoError(t, serviceServing(t.Context(), "postgrest", serverPort(t, http.StatusUnauthorized)))
	})
	t.Run("server error", func(t *testing.T) {
		assert.EqualError(t, serviceServing(t.Context(), "postgrest", serverPort(t, http.StatusServiceUnavailable)),
			"service responded with status 503")
	})
	t.Run("not listening", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := l.Addr().(*net.TCPAddr).Port
		require.NoError(t, l.Close())

		assert.Error(t, serviceServing(t.Context(), "postgrest", port))
	})
	t.Run("mcp requests its health endpoint", func(t *testing.T) {
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
		}))
		t.Cleanup(srv.Close)
		port := srv.Listener.Addr().(*net.TCPAddr).Port

		assert.NoError(t, serviceServing(t.Context(), "mcp", port))
		assert.Equal(t, "/health", path)
	})
	t.Run("mcp client error isn't serving", func(t *testing.T) {
		assert.EqualError(t, serviceServing(t.Context(), "mcp", serverPort(t, http.StatusNotFound)),
			"service responded with status 404")
	})
	t.Run("rag requests its health endpoint", func(t *testing.T) {
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
		}))
		t.Cleanup(srv.Close)
		port := srv.Listener.Addr().(*net.TCPAddr).Port

		assert.NoError(t, serviceServing(t.Context(), "rag", port))
		assert.Equal(t, "/v1/health", path)
	})
	t.Run("rag client error isn't serving", func(t *testing.T) {
		assert.EqualError(t, serviceServing(t.Context(), "rag", serverPort(t, http.StatusNotFound)),
			"service responded with status 404")
	})
	t.Run("unknown service type", func(t *testing.T) {
		assert.EqualError(t, serviceServing(t.Context(), "unknown", serverPort(t, http.StatusOK)),
			`no health check for service type "unknown"`)
	})
}

func TestServiceHealthCheckTimeouts(t *testing.T) {
	// The RAG health endpoint pings every pipeline's providers, which can
	// take about 10 seconds.
	assert.Equal(t, ragHealthCheckTimeout, serviceHealthChecks["rag"].timeout)
	assert.Greater(t, serviceHealthChecks["rag"].timeout, 10*time.Second)
	assert.Equal(t, serviceHealthCheckTimeout, serviceHealthChecks["mcp"].timeout)
	assert.Equal(t, serviceHealthCheckTimeout, serviceHealthChecks["postgrest"].timeout)

	// The deploy-time readiness check wraps the health check, so it must
	// not cut any health check short.
	for serviceType, check := range serviceHealthChecks {
		assert.Greater(t, serviceInstanceStatusTimeout, check.timeout, serviceType)
	}
}
