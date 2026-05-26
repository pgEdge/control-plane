package cmd

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pgEdge/control-plane/common/logging"
	"github.com/pgEdge/control-plane/common/paths"
	"github.com/pgEdge/control-plane/cpctl/config"
)

var defaultConfigPath = filepath.Join(paths.ConfigHome, "cpctl", "config.yaml")
var defaultPretty = term.IsTerminal(int(os.Stdout.Fd()))

type LoggerProvider func() zerolog.Logger

func newRootCmd() *cobra.Command {
	var configPath string
	mgr := &config.Manager{}
	cmd := &cobra.Command{
		Use:   "cpctl",
		Short: "pgEdge Control Plane command line client",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			err := mgr.Load(configPath, cmd.Flags())
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&configPath, "config-path", "c", defaultConfigPath, "Path to the configuration file. Defaults to $XDG_CONFIG_HOME/cpctl/config.yaml.")
	cmd.PersistentFlags().StringP("profile", "p", "", "The name of the profile to use from the configuration file.")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "Enables debug logging.")
	cmd.PersistentFlags().BoolP("silent", "s", false, "Disables logging.")
	cmd.PersistentFlags().Bool("pretty", defaultPretty, "Enables pretty logging. Defaults to true when stdout is as a TTY device.")

	AddCommands(cmd, mgr)

	return cmd
}

func AddCommands(cmd *cobra.Command, mgr *config.Manager) {
	cmd.AddCommand(newClusterCommand(mgr))
	cmd.AddCommand(newListCommand(mgr))
}

func Execute() {
	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		logging.Fatal(err, "encountered a fatal error")
	}
}
