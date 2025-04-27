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
	return slog.New(slogzerolog.Option{
		Level:  translatedLevel,
		Logger: &base,
	}.NewZerologHandler())
}

func Zap(base zerolog.Logger, level zerolog.Level) *zap.Logger {
	core := zerozap.New(base.
		Level(level).
		With().
		Logger())
	return zap.New(core)
}
