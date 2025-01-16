package logging

import (
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/pgEdge/control-plane/server/internal/config"
)

const defaultLevel = zerolog.DebugLevel

// NewLogger initializes and configures a new zerolog.Logger.
func NewLogger(cfg config.Logging) (zerolog.Logger, error) {
	level := defaultLevel
	if cfg.Level != "" {
		l, err := zerolog.ParseLevel(cfg.Level)
		if err != nil {
			return zerolog.Nop(), fmt.Errorf("failed to parse log level '%s': %w", cfg.Level, err)
		}

		level = l
	}

	var out io.Writer
	out = os.Stdout
	if cfg.Pretty {
		out = zerolog.ConsoleWriter{Out: out}
	}

	logger := zerolog.New(out).
		With().
		Timestamp().
		Caller().
		Logger().
		Level(level)

	return logger, nil
}

// Fatal calls Fatal on the default zerolog logger. It's intended to be used in
// panic recovery or other places where a logger instance isn't available.
func Fatal(err any, msg string) {
	logger := log.With().
		Timestamp().
		Caller().
		Logger()

	switch v := err.(type) {
	case error:
		logger.Fatal().
			CallerSkipFrame(2).
			Err(v).
			Msg(msg)
	default:
		logger.Fatal().
			CallerSkipFrame(2).
			Interface("error", err).
			Msg(msg)
	}
}
