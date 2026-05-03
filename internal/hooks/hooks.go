// Package hooks runs lifecycle scripts defined in .grove.yml.
package hooks

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// RunOpts contains the parameters for hook execution.
type RunOpts struct {
	Branch       string
	WorktreePath string
	ProjectRoot  string
	Ports        map[string]int // service name -> assigned port
	Stdout       io.Writer      // typically os.Stdout
	Stderr       io.Writer      // typically os.Stderr
}

// RunPostCreate runs each post_create hook script sequentially.
// It returns a list of warnings (script failures) rather than a hard error,
// so that worktree creation succeeds even if a hook fails.
func RunPostCreate(scripts []string, opts RunOpts) []string {
	var warnings []string
	env := buildEnv(opts)

	for _, script := range scripts {
		if err := runScript("post_create", script, opts, env, true); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings
}

// RunPreDelete runs each pre_delete hook script sequentially.
// It returns the first hook error so callers can abort deletion before the
// worktree is removed.
func RunPreDelete(scripts []string, opts RunOpts) error {
	env := buildEnv(opts)

	for _, script := range scripts {
		if err := runScript("pre_delete", script, opts, env, false); err != nil {
			return err
		}
	}
	return nil
}

func runScript(lifecycle, script string, opts RunOpts, env []string, skipMissing bool) error {
	absScript := filepath.Join(opts.ProjectRoot, script)
	if _, err := os.Stat(absScript); err != nil {
		if os.IsNotExist(err) && skipMissing {
			return fmt.Errorf("hooks.%s: script %q not found, skipping", lifecycle, script)
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("hooks.%s: script %q not found", lifecycle, script)
		}
		return fmt.Errorf("hooks.%s: stat %q: %w", lifecycle, script, err)
	}

	cmd := exec.Command(absScript)
	cmd.Dir = opts.WorktreePath
	cmd.Env = env
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hooks.%s: %q failed: %w", lifecycle, script, err)
	}
	return nil
}

func buildEnv(opts RunOpts) []string {
	// Inherit current environment (PATH, HOME, etc.)
	env := os.Environ()

	env = append(env, "GROVE_BRANCH="+opts.Branch)
	env = append(env, "GROVE_WORKTREE="+opts.WorktreePath)
	env = append(env, "GROVE_PROJECT_ROOT="+opts.ProjectRoot)

	// Add GROVE_PORT_<SERVICE> for each service, uppercased and sorted for determinism.
	names := make([]string, 0, len(opts.Ports))
	for name := range opts.Ports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := "GROVE_PORT_" + strings.ToUpper(name)
		env = append(env, fmt.Sprintf("%s=%d", key, opts.Ports[name]))
	}

	return env
}
