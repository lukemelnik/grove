package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/lukemelnik/grove/internal/certs"
	"github.com/spf13/cobra"
)

var trustAddCA = func(certPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determining home directory: %w", err)
	}
	keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	cmd := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, certPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var trustRemoveCA = func(certPath string) error {
	cmd := exec.Command("security", "remove-trusted-cert", "-d", certPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var trustCheckCA = func() bool {
	cmd := exec.Command("security", "find-certificate", "-c", certs.CACommonName)
	return cmd.Run() == nil
}

func newTrustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage the Grove CA in the macOS login keychain",
		Long: `Add or remove the Grove local CA certificate from your macOS login
keychain. This lets browsers trust HTTPS certificates generated
by the grove proxy without showing security warnings.

  grove trust           Add CA to keychain (triggers system auth dialog)
  grove trust --check   Check if CA is currently trusted (exit code 0/1)
  grove trust --remove  Remove CA from keychain`,
		Args: cobra.NoArgs,
		RunE: runTrust,
	}

	cmd.Flags().Bool("remove", false, "remove the CA from the keychain")
	cmd.Flags().Bool("check", false, "check if the CA is trusted (exit code 0/1)")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	cmd.MarkFlagsMutuallyExclusive("remove", "check")

	return cmd
}

type trustOutput struct {
	Trusted *bool  `json:"trusted,omitempty"`
	Action  string `json:"action,omitempty"`
	Message string `json:"message"`
}

func runTrust(cmd *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return outputError(cmd, fmt.Errorf("grove trust is only supported on macOS"))
	}

	remove, _ := cmd.Flags().GetBool("remove")
	check, _ := cmd.Flags().GetBool("check")

	stateDir, err := certs.DefaultStateDir()
	if err != nil {
		return outputError(cmd, err)
	}

	caCertPath := filepath.Join(stateDir, certs.CACertFile)

	if _, err := os.Stat(caCertPath); err != nil {
		if os.IsNotExist(err) {
			msg := "CA certificate does not exist yet — run grove proxy start first to generate certificates"
			if shouldOutputJSON(cmd) {
				out := trustOutput{Message: msg}
				data, _ := json.Marshal(out)
				fmt.Fprintln(cmd.ErrOrStderr(), string(data))
				return &reportedError{cause: fmt.Errorf("%s", msg)}
			}
			return fmt.Errorf("%s", msg)
		}
		return outputError(cmd, fmt.Errorf("checking CA certificate: %w", err))
	}

	if check {
		return runTrustCheck(cmd)
	}

	if remove {
		return runTrustRemove(cmd, caCertPath)
	}

	return runTrustAdd(cmd, caCertPath)
}

func runTrustCheck(cmd *cobra.Command) error {
	trusted := trustCheckCA()

	if shouldOutputJSON(cmd) {
		out := trustOutput{Trusted: &trusted}
		if trusted {
			out.Message = "Grove CA is trusted in the macOS keychain"
		} else {
			out.Message = "Grove CA is not trusted"
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		if !trusted {
			return &reportedError{cause: fmt.Errorf("CA is not trusted")}
		}
		return nil
	}

	if trusted {
		fmt.Fprintln(cmd.OutOrStdout(), "Grove CA is trusted in the macOS keychain")
		return nil
	}

	return fmt.Errorf("Grove CA is not trusted — run 'grove trust' to add it to your keychain")
}

func runTrustRemove(cmd *cobra.Command, caCertPath string) error {
	if err := trustRemoveCA(caCertPath); err != nil {
		return outputError(cmd, fmt.Errorf("removing CA from keychain: %w", err))
	}

	if shouldOutputJSON(cmd) {
		out := trustOutput{Action: "removed", Message: "Grove CA removed from macOS keychain"}
		data, _ := json.Marshal(out)
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Grove CA removed from macOS keychain")
	return nil
}

func runTrustAdd(cmd *cobra.Command, caCertPath string) error {
	if err := trustAddCA(caCertPath); err != nil {
		return outputError(cmd, fmt.Errorf("adding CA to keychain: %w", err))
	}

	if shouldOutputJSON(cmd) {
		out := trustOutput{Action: "added", Message: "Grove CA added to macOS keychain"}
		data, _ := json.Marshal(out)
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Grove CA added to macOS keychain")
	return nil
}
