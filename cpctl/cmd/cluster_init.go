package cmd

import (
	"context"
	"time"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/common/prompt"
	"github.com/pgEdge/control-plane/cpctl/config"
	"github.com/spf13/cobra"
)

func newClusterInitCommand(mgr *config.Manager) *cobra.Command {
	var clusterID string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Commands to interact with the Control Plane cluster API",
		RunE: func(cmd *cobra.Command, args []string) error {
			// If args is nonempty, we should record the given server in the config
			// defaultPromptAnswer, err := prompt.DefaultFromYesNoFlags(cmd.Flags())
			// if err != nil {
			// 	return err
			// }
			cli, err := mgr.APIClient(args)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			cli.InitCluster(ctx, &controlplane.InitClusterRequest{})

			return nil
		},
	}

	prompt.AddYesNoFlags(cmd)
	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID for the new cluster. A random ID will be generated if blank.")

	return cmd
}
