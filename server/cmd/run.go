package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/samber/do"
	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/server/internal/app"
)

func newRunCommand(i *do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			a, err := app.NewApp(i)
			if err != nil {
				return fmt.Errorf("failed to initialize application: %w", err)
			}

			return a.Run(ctx)
		},
	}
}
