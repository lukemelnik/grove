package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <shell>",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for bash, zsh, or fish.

To load completions:

Bash:
  $ source <(grove completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ grove completion bash > /etc/bash_completion.d/grove
  # macOS:
  $ grove completion bash > $(brew --prefix)/etc/bash_completion.d/grove

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ grove completion zsh > "${fpath[1]}/_grove"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ grove completion fish | source

  # To load completions for each session, execute once:
  $ grove completion fish > ~/.config/fish/completions/grove.fish
`,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}

	return cmd
}
