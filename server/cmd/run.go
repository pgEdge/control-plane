package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pgEdge/control-plane/server/internal/api"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg := configMgr.Config()
			server := api.NewServer(configMgr.Config(), logger)
			server.Start(ctx)

			shutdown := func() {
				logger.Info().Msg("attempting to shut gracefully. press ctrl+c again to force shutdown.")

				gracePeriod := time.Second * time.Duration(cfg.StopGracePeriodSeconds)
				shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
				defer cancel()

				if err := server.Stop(shutdownCtx); err != nil {
					logger.Err(err).Msg("error while stopping the server")
				}
			}

			select {
			case <-ctx.Done():
				logger.Info().Msg("got shutdown signal")

				shutdown()
			case err := <-server.Error():
				logger.Err(err).Msg("server error")

				shutdown()

				return err
			}

			return nil
		},
	}
}
