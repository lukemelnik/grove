//go:build windows

package hooks

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

func configureProcessTree(*exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// CommandContext invokes Cancel while the direct process still exists, so
	// taskkill can terminate its descendants before the parent disappears.
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
