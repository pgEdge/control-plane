package systemd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

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
		assert.NoError(t, serviceServing(t.Context(), serverPort(t, http.StatusOK)))
	})
	t.Run("client error still means serving", func(t *testing.T) {
		assert.NoError(t, serviceServing(t.Context(), serverPort(t, http.StatusUnauthorized)))
	})
	t.Run("server error", func(t *testing.T) {
		assert.EqualError(t, serviceServing(t.Context(), serverPort(t, http.StatusServiceUnavailable)),
			"service responded with status 503")
	})
	t.Run("not listening", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := l.Addr().(*net.TCPAddr).Port
		require.NoError(t, l.Close())

		assert.Error(t, serviceServing(t.Context(), port))
	})
}
