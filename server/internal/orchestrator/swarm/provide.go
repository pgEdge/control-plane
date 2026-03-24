package swarm

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/zfs"
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

		if cfg.ZFS.Enabled && cfg.ZFS.CommandImage != "" {
			logger.Info().
				Str("image", cfg.ZFS.CommandImage).
				Msg("using docker-based host command runner")
			hostRunner := newDockerHostRunner(dockerClient, cfg.ZFS.CommandImage, logger)
			zfs.DefaultHostRunner = hostRunner
			zfs.DefaultCommandRunner = func(args ...string) (string, error) {
				return hostRunner("zfs", args...)
			}
		}

		return NewOrchestrator(context.Background(), cfg, dockerClient, logger)
	})
}
