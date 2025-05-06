package cmd

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/server/internal/api"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/ipam"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/orchestrator"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

var (
	configPath string
	logger     zerolog.Logger
)

func newRootCmd(i *do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "control-plane",
		Short: "pgEdge control plane server",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			// Source order determines precedence. The last source loaded will
			// override any previous values.
			var sources []*config.Source
			if configPath != "" {
				sources = append(sources, config.NewJsonFileSource(configPath))
			}
			sources = append(sources,
				config.NewEnvVarSource(),
				config.NewPFlagSource(cmd.Flags()),
			)

			cfg, err := config.LoadSources(sources...)
			if err != nil {
				return fmt.Errorf("failed to load configs: %w", err)
			}

			config.Provide(i, cfg)
			api.Provide(i)
			certificates.Provide(i)
			database.Provide(i)
			docker.Provide(i)
			etcd.Provide(i)
			filesystem.Provide(i)
			host.Provide(i)
			ipam.Provide(i)
			logging.Provide(i)
			resource.Provide(i)
			workflows.Provide(i)
			activities.Provide(i)
			task.Provide(i)

			registry, err := do.Invoke[*resource.Registry](i)
			if err != nil {
				return fmt.Errorf("failed to get resource registry: %w", err)
			}

			// All resource types get registered regardless of which
			// orchestrator is configured.
			database.RegisterResourceTypes(registry)
			filesystem.RegisterResourceTypes(registry)
			swarm.RegisterResourceTypes(registry)

			if err := orchestrator.Provide(i); err != nil {
				return fmt.Errorf("failed to register orchestrator provider: %w", err)
			}

			logger, err = do.Invoke[zerolog.Logger](i)
			if err != nil {
				return fmt.Errorf("failed to initialize logger: %w", err)
			}

			return nil
		},
	}
}

func Execute() {
	i := do.New()
	rootCmd := newRootCmd(i)
	rootCmd.PersistentFlags().StringVarP(&configPath, "config-path", "c", "", "Path to the config.json file for this service.")
	rootCmd.PersistentFlags().StringP("logging.level", "l", "", "The logging level, e.g. 'debug', 'info', 'error', etc.")
	rootCmd.PersistentFlags().BoolP("logging.pretty", "p", false, "Use pretty logging instead of JSON logging.")

	rootCmd.AddCommand(newRunCommand(i))

	if err := rootCmd.Execute(); err != nil {
		if logger.GetLevel() == zerolog.NoLevel {
			// NoLevel indicates that the logger is uninitialized. In this case
			// we'll use our fallback logger.
			logging.Fatal(err, "command failed")
		} else {
			logger.Fatal().
				Err(err).
				Msg("command failed")
		}
	}
}
