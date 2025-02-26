package etcd

import (
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/zerozap"
	"go.uber.org/zap"
)

// func RegisterLogger(i *do.Injector) {
// 	do.Provide(i, func(i *do.Injector) (*zap.Logger, error) {
// 		mgr, err := do.Invoke[*config.Manager](i)
// 		if err != nil {
// 			return nil, err
// 		}
// 		base, err := do.Invoke[zerolog.Logger](i)
// 		if err != nil {
// 			return nil, err
// 		}
// 		return NewLogger(mgr.Config().EmbeddedEtcd, base)
// 	})
// }

func newZapLogger(base zerolog.Logger, logLevel, component string) (*zap.Logger, error) {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to parse etcd log level: %w", err)
	}
	core := zerozap.New(base.
		Level(level).
		With().
		Str("component", component).
		CallerWithSkipFrameCount(5). // Found via trial and error
		Logger())
	return zap.New(core), nil
}
