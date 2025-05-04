package logging

import (
	"log/slog"

	"github.com/rs/zerolog"
	slogzerolog "github.com/samber/slog-zerolog/v2"
	"go.mau.fi/zerozap"
	"go.uber.org/zap"
)

func Slog(base zerolog.Logger, level zerolog.Level) *slog.Logger {
	translatedLevel := slog.LevelDebug
	for sl, zl := range slogzerolog.LogLevels {
		if zl == level {
			translatedLevel = sl
			break
		}
	}
	innerLogger := base.With().
		CallerWithSkipFrameCount(3).
		Logger()
	return slog.New(slogzerolog.Option{
		Level:  translatedLevel,
		Logger: &innerLogger,
	}.NewZerologHandler())
}

func Zap(base zerolog.Logger, level zerolog.Level) *zap.Logger {
	core := zerozap.New(base.
		Level(level).
		With().
		CallerWithSkipFrameCount(5). // Found via trial and error
		Logger())
	return zap.New(core)
}
