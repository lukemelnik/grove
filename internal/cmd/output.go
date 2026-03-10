package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// isTerminal reports whether the given file descriptor is a terminal.
// It is a variable so tests can override it.
var isTerminal = func(fd int) bool {
	return term.IsTerminal(fd)
}

// shouldOutputJSON returns true if the command should produce JSON output.
// This is the case when the --json flag is set explicitly, or when stdout
// is not a terminal (e.g., piped to another program or captured by an agent).
func shouldOutputJSON(cmd *cobra.Command) bool {
	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return true
	}
	return !isTerminal(int(os.Stdout.Fd()))
}

// outputError writes a structured JSON error to stderr when in JSON mode,
// or returns the error for normal Cobra error handling otherwise.
func outputError(cmd *cobra.Command, err error) error {
	if shouldOutputJSON(cmd) {
		msg := struct {
			Error string `json:"error"`
		}{Error: err.Error()}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(cmd.ErrOrStderr(), string(data))
		// Return the original error so the CLI exits with a non-zero code.
		return err
	}
	return err
}
