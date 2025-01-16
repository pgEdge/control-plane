package cmd

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
)

var (
	configPath string
	configMgr  *config.Manager
	logger     zerolog.Logger
)

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "control-plane",
		Short: "pgEdge control plane server",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceErrors = true

			sources := []*config.Source{
				config.NewEnvVarSource(),
				config.NewPFlagSource(cmd.Flags()),
			}
			if configPath != "" {
				sources = append(sources, config.NewJsonFileSource(configPath))
			}
			mgr, err := config.NewManager(sources...)
			if err != nil {
				return fmt.Errorf("failed to initialize config manager: %w", err)
			}
			configMgr = mgr

			cfg := configMgr.Config()

			lgr, err := logging.NewLogger(cfg.Logging)
			if err != nil {
				return fmt.Errorf("failed to initialize logger: %w", err)
			}
			logger = lgr.With().
				Str("tenant_id", cfg.TenantID).
				Str("cluster_id", cfg.ClusterID).
				Str("host_id", cfg.HostID).
				Logger()

			zerolog.DefaultContextLogger = &logger

			return nil
		},
	}
}

func Execute() {
	rootCmd := newRootCmd()
	rootCmd.PersistentFlags().StringVarP(&configPath, "config-path", "c", "", "Path to the config.json file for this service.")
	rootCmd.PersistentFlags().StringP("logging.level", "l", "", "The logging level, e.g. 'debug', 'info', 'error', etc.")
	rootCmd.PersistentFlags().BoolP("logging.pretty", "p", false, "Use pretty logging instead of JSON logging.")

	rootCmd.AddCommand(newRunCommand())

	if err := rootCmd.Execute(); err != nil {
		if logger.GetLevel() == zerolog.NoLevel {
			// The zero value for zerolog.Logger has level set to NoLevel. We
			// want to use our fallback logger in this case.
			logging.Fatal(err, "command failed")
		} else {
			logger.Fatal().
				Err(err).
				Msg("command failed")
		}
	}
}
