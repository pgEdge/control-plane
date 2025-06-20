package api

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/api/apiv1"
	"github.com/pgEdge/control-plane/server/internal/config"
)

func Provide(i *do.Injector) {
	apiv1.Provide(i)
	provideServer(i)
}

func provideServer(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Server, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		v1Svc, err := do.Invoke[*apiv1.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get v1 api service: %w", err)
		}
		return NewServer(cfg, logger, v1Svc), nil
	})
}
