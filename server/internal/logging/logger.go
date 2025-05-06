package logging

import (
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/pgEdge/control-plane/server/internal/config"
)

const defaultLevel = zerolog.InfoLevel

// NewLogger initializes and configures a new zerolog.Logger.
func NewLogger(cfg config.Config) (zerolog.Logger, error) {
	level := defaultLevel
	if cfg.Logging.Level != "" {
		l, err := zerolog.ParseLevel(cfg.Logging.Level)
		if err != nil {
			return zerolog.Nop(), fmt.Errorf("failed to parse log level '%s': %w", cfg.Logging.Level, err)
		}

		level = l
	}

	var out io.Writer
	out = os.Stdout
	if cfg.Logging.Pretty {
		out = zerolog.ConsoleWriter{Out: out}
	}

	logger := zerolog.New(out).
		With().
		Timestamp().
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
