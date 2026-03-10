// Package tmux handles tmux session/window creation, pane layouts,
// and environment injection for Grove workspaces.
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"grove/internal/config"
	"grove/internal/worktree"
)

// Runner executes tmux commands. This interface exists for testability.
type Runner interface {
	// Run executes a tmux command and returns its combined output.
	Run(args ...string) (string, error)
}

// realRunner executes tmux commands via os/exec.
type realRunner struct{}

// NewRunner creates a Runner that executes real tmux commands.
func NewRunner() Runner {
	return &realRunner{}
}

func (r *realRunner) Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("tmux %s: %s: %w", strings.Join(args, " "), output, err)
	}
	return output, nil
}

// presetLayouts lists all valid tmux preset layout names.
var presetLayouts = map[string]bool{
	"even-horizontal": true,
	"even-vertical":   true,
	"main-horizontal": true,
	"main-vertical":   true,
	"tiled":           true,
}

// IsPresetLayout returns true if the layout string is a tmux preset name.
func IsPresetLayout(layout string) bool {
	return presetLayouts[layout]
}

// layoutTier determines which layout tier the config uses.
type layoutTier int

const (
	tierPreset         layoutTier = iota + 1 // Tier 1: preset name
	tierPresetWithSize                       // Tier 2: preset + main_size
	tierExplicitSplits                       // Tier 3: explicit split objects
	tierRawLayout                            // Tier 4: raw tmux layout string
)

// detectTier determines the layout tier from tmux config.
func detectTier(cfg *config.TmuxConfig) layoutTier {
	// Tier 3: any pane has a Split field set
	if hasExplicitSplits(cfg.Panes) {
		return tierExplicitSplits
	}

	// Tier 4: layout is set but is not a known preset
	if cfg.Layout != "" && !IsPresetLayout(cfg.Layout) {
		return tierRawLayout
	}

	// Tier 2: preset with size hint
	if cfg.MainSize != "" {
		return tierPresetWithSize
	}

	// Tier 1: preset name (or default main-vertical)
	return tierPreset
}

// hasExplicitSplits checks if any pane in the slice has a Split field.
func hasExplicitSplits(panes []config.Pane) bool {
	for _, p := range panes {
		if p.Split != "" {
			return true
		}
	}
	return false
}

// SessionName converts a branch name into a tmux-safe session/window name.
func SessionName(branch string) string {
	return worktree.SanitizeBranchName(branch)
}

func legacySessionName(branch string) string {
	sanitized := strings.ReplaceAll(branch, "/", "-")
	if sanitized == ".." {
		sanitized = "dotdot"
	}
	if sanitized == "." {
		sanitized = "dot"
	}
	return sanitized
}

func sessionNameCandidates(branch string) []string {
	canonical := SessionName(branch)
	legacy := legacySessionName(branch)
	if legacy == canonical {
		return []string{canonical}
	}
	return []string{canonical, legacy}
}

// Options controls tmux workspace creation behavior.
type Options struct {
	// IncludeAll includes all optional panes.
	IncludeAll bool

	// IncludeWith lists specific optional pane names to include.
	IncludeWith []string

	// Attach controls whether to attach/switch after creation.
	Attach bool

	// WorktreePath is the directory for the worktree (used as -c for panes).
	WorktreePath string

	// Branch is the branch name (used for naming).
	Branch string

	// Env has grove-managed env vars (ports, env block).
	// Injected via set-environment as a fallback for non-dotenv tools.
	// Primary mechanism is .env.local files written into the worktree.
	Env map[string]string

	// TmuxConfig is the tmux configuration from .grove.yml.
	TmuxConfig *config.TmuxConfig
}

// Manager orchestrates tmux workspace creation.
type Manager struct {
	runner Runner
}

// NewManager creates a new tmux Manager.
func NewManager(runner Runner) *Manager {
	return &Manager{runner: runner}
}

// Create sets up the full tmux workspace: session/window, env, panes, and attach.
func (m *Manager) Create(opts Options) error {
	name := SessionName(opts.Branch)
	cfg := opts.TmuxConfig

	mode := cfg.Mode
	if mode == "" {
		mode = "window"
	}

	// Step 1: Create session or window
	switch mode {
	case "session":
		if err := m.createSession(name, opts.WorktreePath); err != nil {
			return fmt.Errorf("creating tmux session: %w", err)
		}
	case "window":
		session, err := m.currentSession()
		if err != nil {
			// Fallback to session mode if not inside tmux
			if err := m.createSession(name, opts.WorktreePath); err != nil {
				return fmt.Errorf("creating tmux session (fallback): %w", err)
			}
			mode = "session"
			break
		}
		if err := m.createWindow(session, name, opts.WorktreePath); err != nil {
			return fmt.Errorf("creating tmux window: %w", err)
		}
	}

	// Step 2: Inject managed env via set-environment (session mode only).
	// This is a fallback for tools that read env vars directly instead of .env files.
	// In window mode, set-environment would leak between windows, so we skip it —
	// tools should read .env/.env.local files instead.
	if mode == "session" && len(opts.Env) > 0 {
		if err := m.injectEnv(name, opts.Env); err != nil {
			return fmt.Errorf("injecting environment: %w", err)
		}
	}

	// Step 3: Filter panes (handle optional)
	panes := filterPanes(cfg.Panes, opts.IncludeAll, opts.IncludeWith)

	// Step 4: Create panes
	target := name // session or window name
	if err := m.createPanes(target, opts.WorktreePath, panes, cfg); err != nil {
		return fmt.Errorf("creating panes: %w", err)
	}

	// Step 5: Attach/switch if requested
	if opts.Attach {
		if err := m.doAttach(name, mode); err != nil {
			return fmt.Errorf("attaching to tmux: %w", err)
		}
	}

	return nil
}

// HasSession checks if a tmux session with the given name exists.
func (m *Manager) HasSession(name string) bool {
	_, err := m.runner.Run("has-session", "-t", name)
	return err == nil
}

// HasWindow checks if a tmux window with the given name exists.
// It uses list-windows to search across all sessions.
func (m *Manager) HasWindow(name string) bool {
	out, err := m.runner.Run("list-windows", "-a", "-F", "#{window_name}", "-f", "#{==:#{window_name},"+escapeTmuxFilter(name)+"}")
	if err != nil {
		return false
	}
	// Some tmux versions return exit 0 with empty output when no windows match.
	return strings.TrimSpace(out) != ""
}

// Destroy kills the tmux session or window for a branch.
func (m *Manager) Destroy(branch string, cfg *config.TmuxConfig) error {
	mode := cfg.Mode
	if mode == "" {
		mode = "window"
	}
	name := m.ResolveName(branch, mode)

	switch mode {
	case "session":
		_, err := m.runner.Run("kill-session", "-t", name)
		if err != nil {
			return fmt.Errorf("killing tmux session %q: %w", name, err)
		}
	case "window":
		_, err := m.runner.Run("kill-window", "-t", name)
		if err != nil {
			return fmt.Errorf("killing tmux window %q: %w", name, err)
		}
	}
	return nil
}

// ResolveName returns the active tmux target name for a branch, falling back to
// the legacy slash-to-dash mapping for existing sessions/windows.
func (m *Manager) ResolveName(branch, mode string) string {
	for _, candidate := range sessionNameCandidates(branch) {
		switch mode {
		case "session":
			if m.HasSession(candidate) {
				return candidate
			}
		default:
			if m.HasWindow(candidate) {
				return candidate
			}
		}
	}
	return SessionName(branch)
}

// createSession creates a new detached tmux session.
func (m *Manager) createSession(name, workdir string) error {
	_, err := m.runner.Run("new-session", "-d", "-s", name, "-c", workdir)
	return err
}

// createWindow creates a new window in the given session.
func (m *Manager) createWindow(session, name, workdir string) error {
	_, err := m.runner.Run("new-window", "-t", session, "-n", name, "-c", workdir)
	return err
}

// currentSession returns the name of the current tmux session, or error if not inside tmux.
func (m *Manager) currentSession() (string, error) {
	// Check TMUX env var first
	if os.Getenv("TMUX") == "" {
		return "", fmt.Errorf("not inside tmux")
	}
	out, err := m.runner.Run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// injectEnv sets environment variables on the tmux session.
func (m *Manager) injectEnv(session string, env map[string]string) error {
	// Sort keys for deterministic ordering
	keys := sortedKeys(env)
	for _, k := range keys {
		_, err := m.runner.Run("set-environment", "-t", session, k, env[k])
		if err != nil {
			return fmt.Errorf("setting env var %s: %w", k, err)
		}
	}
	return nil
}

// createPanes creates panes according to the layout tier.
func (m *Manager) createPanes(target, workdir string, panes []config.Pane, cfg *config.TmuxConfig) error {
	if len(panes) == 0 {
		return nil
	}

	tier := detectTier(cfg)

	switch tier {
	case tierPreset:
		return m.createPanesPreset(target, workdir, panes, effectiveLayout(cfg), "")
	case tierPresetWithSize:
		return m.createPanesPreset(target, workdir, panes, effectiveLayout(cfg), cfg.MainSize)
	case tierExplicitSplits:
		return m.createPanesExplicit(target, workdir, panes)
	case tierRawLayout:
		return m.createPanesRaw(target, workdir, panes, cfg.Layout)
	}
	return nil
}

// effectiveLayout returns the layout name, defaulting to "main-vertical".
func effectiveLayout(cfg *config.TmuxConfig) string {
	if cfg.Layout == "" {
		return "main-vertical"
	}
	return cfg.Layout
}

// createPanesPreset handles Tier 1 and Tier 2 layouts.
func (m *Manager) createPanesPreset(target, workdir string, panes []config.Pane, layout, mainSize string) error {
	// First pane already exists in the session/window. Send command to it.
	if panes[0].Cmd != "" || panes[0].Setup != "" {
		if err := m.sendPaneCommands(target, panes[0]); err != nil {
			return fmt.Errorf("sending command to first pane: %w", err)
		}
	}

	// Create additional panes via split-window
	for i := 1; i < len(panes); i++ {
		_, err := m.runner.Run("split-window", "-h", "-t", target, "-c", workdir)
		if err != nil {
			return fmt.Errorf("splitting pane %d: %w", i, err)
		}
		if panes[i].Cmd != "" || panes[i].Setup != "" {
			if err := m.sendPaneCommands(target, panes[i]); err != nil {
				return fmt.Errorf("sending command to pane %d: %w", i, err)
			}
		}
	}

	// Tier 2: set main pane size before applying layout.
	// tmux accepts percentage strings (e.g. "70%") directly for these options.
	if mainSize != "" {
		if layout == "main-vertical" || layout == "main-horizontal" {
			var optionName string
			if layout == "main-vertical" {
				optionName = "main-pane-width"
			} else {
				optionName = "main-pane-height"
			}
			_, err := m.runner.Run("set-option", "-t", target, optionName, mainSize)
			if err != nil {
				return fmt.Errorf("setting %s: %w", optionName, err)
			}
		}
	}

	// Apply the preset layout
	_, err := m.runner.Run("select-layout", "-t", target, layout)
	if err != nil {
		return fmt.Errorf("applying layout %q: %w", layout, err)
	}

	// Select the first pane
	_, _ = m.runner.Run("select-pane", "-t", target+".0")

	return nil
}

// createPanesExplicit handles Tier 3: explicit split objects.
//
// The key insight is that tmux's split-window always splits the *currently selected*
// pane. To build nested layouts correctly, we must create all sibling splits first
// (establishing the structural layout), then recurse into each region. Otherwise,
// depth-first processing leaves the active pane inside a container, causing the next
// sibling to split the wrong pane.
func (m *Manager) createPanesExplicit(target, workdir string, panes []config.Pane) error {
	// Get the initial pane ID (the one created with the session/window)
	initialID, err := m.runner.Run("display-message", "-p", "-t", target, "#{pane_id}")
	if err != nil {
		return fmt.Errorf("getting initial pane id: %w", err)
	}
	return m.walkPanes(strings.TrimSpace(initialID), workdir, panes, "h")
}

// walkPanes recursively creates panes from a split tree using pane ID targeting.
// parentPaneID is the tmux pane ID to split from (e.g. "%0").
// splitDir is "h" for horizontal (left-right) or "v" for vertical (top-bottom).
//
// Phase 1: Create all sibling splits to establish structure, collecting pane IDs.
// Phase 2: Fill each pane with its command or recurse into containers.
func (m *Manager) walkPanes(parentPaneID, workdir string, panes []config.Pane, splitDir string) error {
	if len(panes) == 0 {
		return nil
	}

	// Phase 1: Create structural splits for all siblings.
	// First pane reuses parentPaneID; remaining panes split from it.
	paneIDs := make([]string, len(panes))
	paneIDs[0] = parentPaneID

	for i := 1; i < len(panes); i++ {
		splitArgs := []string{"split-window", "-" + splitDir, "-t", parentPaneID, "-P", "-F", "#{pane_id}", "-c", workdir}
		if panes[i].Size != "" {
			pct, err := parseSizePercent(panes[i].Size)
			if err == nil {
				splitArgs = append(splitArgs, "-p", strconv.Itoa(pct))
			}
		}
		id, err := m.runner.Run(splitArgs...)
		if err != nil {
			return fmt.Errorf("splitting pane %d: %w", i, err)
		}
		paneIDs[i] = strings.TrimSpace(id)
	}

	// Phase 2: Fill each pane with content or recurse into containers.
	for i, p := range panes {
		if p.Split != "" {
			nestedDir := "v"
			if p.Split == "horizontal" {
				nestedDir = "h"
			}
			if err := m.walkPanes(paneIDs[i], workdir, p.Panes, nestedDir); err != nil {
				return err
			}
			continue
		}

		// Leaf pane — send command
		if p.Cmd != "" || p.Setup != "" {
			if err := m.sendPaneCommands(paneIDs[i], p); err != nil {
				return fmt.Errorf("sending command to pane %d: %w", i, err)
			}
		}
	}

	// Select the first pane in this group
	_, _ = m.runner.Run("select-pane", "-t", paneIDs[0])

	return nil
}

// createPanesRaw handles Tier 4: raw tmux layout string.
func (m *Manager) createPanesRaw(target, workdir string, panes []config.Pane, rawLayout string) error {
	// First pane already exists. Send command.
	if len(panes) > 0 && (panes[0].Cmd != "" || panes[0].Setup != "") {
		if err := m.sendPaneCommands(target, panes[0]); err != nil {
			return fmt.Errorf("sending command to first pane: %w", err)
		}
	}

	// Create additional panes
	for i := 1; i < len(panes); i++ {
		_, err := m.runner.Run("split-window", "-h", "-t", target, "-c", workdir)
		if err != nil {
			return fmt.Errorf("splitting pane %d: %w", i, err)
		}
		if panes[i].Cmd != "" || panes[i].Setup != "" {
			if err := m.sendPaneCommands(target, panes[i]); err != nil {
				return fmt.Errorf("sending command to pane %d: %w", i, err)
			}
		}
	}

	// Apply the raw layout string
	_, err := m.runner.Run("select-layout", "-t", target, rawLayout)
	if err != nil {
		return fmt.Errorf("applying raw layout: %w", err)
	}

	// Select first pane
	_, _ = m.runner.Run("select-pane", "-t", target+".0")

	return nil
}

// Attach attaches to or switches to the tmux session/window.
// This is the public version of the attach method.
func (m *Manager) Attach(name, mode string) error {
	return m.doAttach(name, mode)
}

// doAttach implements the attach/switch logic.
func (m *Manager) doAttach(name, mode string) error {
	if os.Getenv("TMUX") != "" {
		// Already inside tmux — switch client
		_, err := m.runner.Run("switch-client", "-t", name)
		return err
	}

	if mode == "session" {
		_, err := m.runner.Run("attach", "-t", name)
		return err
	}

	// Window mode outside tmux — find the session containing the window,
	// select the window, then attach to the session.
	// list-windows -a -F gives us "session_name:window_name" style info.
	sessionName, err := m.findWindowSession(name)
	if err != nil {
		// Fallback: try attaching with the window name directly via target syntax
		_, attachErr := m.runner.Run("attach", "-t", name)
		return attachErr
	}
	// Select the window first, then attach to the parent session.
	_, _ = m.runner.Run("select-window", "-t", sessionName+":"+name)
	_, err = m.runner.Run("attach", "-t", sessionName)
	return err
}

// findWindowSession returns the session name that contains the given window.
func (m *Manager) findWindowSession(windowName string) (string, error) {
	out, err := m.runner.Run("list-windows", "-a", "-F", "#{session_name}", "-f", "#{==:#{window_name},"+escapeTmuxFilter(windowName)+"}")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("window %q not found", windowName)
	}
	// Take the first line in case multiple sessions have a window with the same name.
	lines := strings.SplitN(out, "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

// filterPanes returns only the panes that should be created, based on optional flags.
func filterPanes(panes []config.Pane, includeAll bool, includeWith []string) []config.Pane {
	if includeAll {
		return panes
	}

	// Build a set of names/indices to include
	includeSet := make(map[string]bool, len(includeWith))
	for _, w := range includeWith {
		includeSet[w] = true
	}

	var result []config.Pane
	for i, p := range panes {
		if p.Split != "" {
			// Split containers: recurse to filter nested panes
			filtered := filterPanes(p.Panes, includeAll, includeWith)
			if len(filtered) > 0 {
				cp := p
				cp.Panes = filtered
				result = append(result, cp)
			}
			continue
		}

		if !p.Optional {
			result = append(result, p)
			continue
		}

		// Optional pane: include if name or index matches --with
		if p.Name != "" && includeSet[p.Name] {
			result = append(result, p)
			continue
		}
		if includeSet[strconv.Itoa(i)] {
			result = append(result, p)
		}
	}
	return result
}

// parseSizePercent parses a size string like "70%" into an integer percentage.
func parseSizePercent(s string) (int, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		s = s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n <= 0 || n > 100 {
		return 0, fmt.Errorf("size percentage must be between 1 and 100, got %d", n)
	}
	return n, nil
}

// escapeTmuxFilter escapes characters that have special meaning in tmux
// format/filter strings (#{...}). This prevents branch names containing
// #, {, or } from breaking tmux filter expressions.
func escapeTmuxFilter(s string) string {
	s = strings.ReplaceAll(s, "#", "##")
	s = strings.ReplaceAll(s, "{", "\\{")
	s = strings.ReplaceAll(s, "}", "\\}")
	return s
}

// sendPaneCommands sends setup and/or cmd to a tmux pane according to autorun rules.
//
// Behavior matrix:
//
//	setup="" , cmd="X", autorun=true  → send "X" + Enter
//	setup="" , cmd="X", autorun=false → send "X" (no Enter)
//	setup="S", cmd="" , (any)         → send "S" + Enter
//	setup="S", cmd="X", autorun=true  → send "S && X" + Enter
//	setup="S", cmd="X", autorun=false → send "S" + Enter, then send "X" (no Enter)
func (m *Manager) sendPaneCommands(target string, p config.Pane) error {
	setup := p.Setup
	cmd := p.Cmd
	autorun := p.ShouldAutorun()

	switch {
	case setup != "" && cmd != "" && autorun:
		_, err := m.runner.Run("send-keys", "-t", target, setup+" && "+cmd, "Enter")
		return err

	case setup != "" && cmd != "" && !autorun:
		if _, err := m.runner.Run("send-keys", "-t", target, setup, "Enter"); err != nil {
			return err
		}
		_, err := m.runner.Run("send-keys", "-t", target, cmd)
		return err

	case setup != "":
		_, err := m.runner.Run("send-keys", "-t", target, setup, "Enter")
		return err

	case cmd != "" && autorun:
		_, err := m.runner.Run("send-keys", "-t", target, cmd, "Enter")
		return err

	case cmd != "":
		_, err := m.runner.Run("send-keys", "-t", target, cmd)
		return err
	}

	return nil
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
