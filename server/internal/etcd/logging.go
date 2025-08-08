package etcd

import (
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/zerozap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"
)

func newZapLogger(base zerolog.Logger, logLevel, component string) (*zap.Logger, error) {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to parse etcd log level: %w", err)
	}
	core := zerozap.New(base.
		Level(level).
		With().
		Str("component", component).
		Logger())
	return zap.New(core), nil
}

func newGrpcLogger(base zerolog.Logger) *zapgrpc.Logger {
	core := zerozap.New(base.
		Level(zerolog.FatalLevel).
		With().
		Str("component", "grpc_logger").
		Logger())
	return zapgrpc.NewLogger(zap.New(core))
}
