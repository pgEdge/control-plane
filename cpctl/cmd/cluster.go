package cmd

import (
	"github.com/pgEdge/control-plane/cpctl/config"
	"github.com/spf13/cobra"
)

func newClusterCommand(mgr *config.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Commands to interact with the Control Plane cluster API",
	}

	cmd.AddCommand(newClusterDiscoverCommand(mgr))
	cmd.AddCommand(newClusterInitCommand(mgr))

	return cmd
}
