package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/samber/do"
	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/server/internal/version"
)

func newVersionCommand(i *do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			info, err := version.GetInfo()
			if err != nil {
				return fmt.Errorf("failed to initialize application: %w", err)
			}
			raw, err := json.MarshalIndent(info, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal version info: %w", err)
			}

			fmt.Println(string(raw))

			return nil
		},
	}
}
