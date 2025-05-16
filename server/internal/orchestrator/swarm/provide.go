package swarm

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/docker"
)

func Provide(i *do.Injector) {
	provideOrchestrator(i)
}

func provideOrchestrator(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Orchestrator, error) {
		dockerClient, err := do.Invoke[*docker.Docker](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get docker client: %w", err)
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		return NewOrchestrator(context.Background(), cfg, dockerClient, logger)
	})
}
