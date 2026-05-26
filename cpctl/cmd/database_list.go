package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/pgEdge/control-plane/cpctl/config"
	"github.com/spf13/cobra"
)

func newListCommand(mgr *config.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List database instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := mgr.APIClient(args)
			if err != nil {
				return err
			}

			resp, err := cli.ListDatabases(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list databases: %w", err)
			}

			return writeOutput(os.Stdout, mgr.Config().Output, resp)
		},
	}

	addOutputFlag(cmd.Flags())

	return cmd
}
