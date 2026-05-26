package logging

import (
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// NewLogger initializes and configures a new zerolog.Logger.
func NewLogger(level zerolog.Level, pretty bool) zerolog.Logger {
	var out io.Writer
	out = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: out}
	}

	logger := zerolog.New(out).
		With().
		Timestamp().
		Logger().
		Level(level)

	return logger
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
