//go:build !darwin && !linux && !windows

package hooks

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcessTree(*exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
