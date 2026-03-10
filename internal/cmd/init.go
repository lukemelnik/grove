package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a new .grove.yml configuration",
		Long:  `Interactively create a .grove.yml in the current directory by asking about services, ports, and tmux preferences.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "grove init: not yet implemented")
			return nil
		},
	}
}
