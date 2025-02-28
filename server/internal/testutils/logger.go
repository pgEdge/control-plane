package testutils

import (
	"testing"

	"github.com/rs/zerolog"
)

func Logger(t testing.TB) zerolog.Logger {
	t.Helper()

	if testing.Verbose() {
		return zerolog.New(zerolog.NewTestWriter(t))
	}

	return zerolog.Nop()
}
