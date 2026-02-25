package logging

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
)

func Provide(i *do.Injector) {
	provideLogger(i)
	provideFactory(i)
}

func provideLogger(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (zerolog.Logger, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return zerolog.Nop(), fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := NewLogger(cfg)
		if err != nil {
			return zerolog.Nop(), fmt.Errorf("failed to create logger: %w", err)
		}
		return logger, nil
	})
}

func provideFactory(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Factory, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get base logger: %w", err)
		}
		return NewFactory(cfg, logger)
	})
}
