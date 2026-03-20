package testutils

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/rs/zerolog"
)

func Logger(t testing.TB) zerolog.Logger {
	t.Helper()

	if testing.Verbose() {
		return zerolog.New(zerolog.NewTestWriter(t)).With().
			Str("test_name", t.Name()).
			Logger()
	}

	return zerolog.Nop()
}

func LoggerFactory(t testing.TB) *logging.Factory {
	t.Helper()

	factory, err := logging.NewFactory(config.Config{}, Logger(t))
	if err != nil {
		t.Fatal(err)
	}

	return factory
}
