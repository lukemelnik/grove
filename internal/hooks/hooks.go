// Package hooks runs lifecycle scripts defined in .grove.yml.
package hooks

import (
	"fmt"
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
	Stdout       *os.File       // typically os.Stdout
	Stderr       *os.File       // typically os.Stderr
}

// RunPostCreate runs each post_create hook script sequentially.
// It returns a list of warnings (script failures) rather than a hard error,
// so that worktree creation succeeds even if a hook fails.
func RunPostCreate(scripts []string, opts RunOpts) []string {
	var warnings []string
	env := buildEnv(opts)

	for _, script := range scripts {
		absScript := filepath.Join(opts.ProjectRoot, script)
		if _, err := os.Stat(absScript); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("hooks.post_create: script %q not found, skipping", script))
			continue
		}

		cmd := exec.Command(absScript)
		cmd.Dir = opts.WorktreePath
		cmd.Env = env
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr

		if err := cmd.Run(); err != nil {
			warnings = append(warnings, fmt.Sprintf("hooks.post_create: %q failed: %v", script, err))
		}
	}
	return warnings
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
