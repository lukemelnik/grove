// Package tmux handles tmux session/window creation, pane layouts,
// and environment injection for Grove workspaces.
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/worktree"
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
		if isSensitiveTmuxCommand(args) {
			return "", fmt.Errorf("tmux %s: %w", sanitizedTmuxArgs(args), err)
		}
		return output, fmt.Errorf("tmux %s: %s: %w", strings.Join(args, " "), output, err)
	}
	return output, nil
}

const redactedTmuxArg = "[redacted]"

func isSensitiveTmuxCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return args[0] == "set-environment" || args[0] == "send-keys"
}

func sanitizedTmuxArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	sanitized := append([]string(nil), args...)
	switch args[0] {
	case "set-environment":
		if len(sanitized) > 0 {
			sanitized[len(sanitized)-1] = redactedTmuxArg
		}
	case "send-keys":
		redactSendKeysArgs(sanitized)
	}
	return strings.Join(sanitized, " ")
}

func redactSendKeysArgs(args []string) {
	for i := 1; i < len(args); i++ {
		if args[i] == "-t" && i+1 < len(args) {
			i++ // Preserve only the target; every typed key/payload is sensitive.
			continue
		}
		args[i] = redactedTmuxArg
	}
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

const (
	// RoleCanonical marks Grove's primary reopen target for a worktree.
	RoleCanonical = "canonical"
	// RoleExtra marks additional Grove-created targets for the same worktree.
	RoleExtra = "extra"

	optionBranch      = "@grove.branch"
	optionProjectRoot = "@grove.project_root"
	optionWorktree    = "@grove.worktree_path"
	optionRole        = "@grove.role"
)

// Target identifies a labeled Grove tmux target.
type Target struct {
	Mode    string
	Target  string
	Session string
	Name    string
	Role    string
}

// Options controls tmux workspace creation behavior.
type Options struct {
	// ProjectRoot is the main repository root used to label and find Grove targets.
	ProjectRoot string

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

	// Role labels the created target as canonical or extra. Defaults to canonical.
	Role string

	// ForceNewWindow creates a new tmux window even if another target exists.
	ForceNewWindow bool
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
	if opts.ForceNewWindow {
		mode = "window"
	}

	role := opts.Role
	if role == "" {
		role = RoleCanonical
	}

	// Step 1: Create session or window
	var target Target
	created := false
	switch mode {
	case "session":
		if m.HasSession(name) {
			return fmt.Errorf("tmux session %q already exists without Grove ownership labels; refusing to adopt it", name)
		}
		if err := m.createSession(name, opts.WorktreePath); err != nil {
			return fmt.Errorf("creating tmux session: %w", err)
		}
		created = true
		target = Target{Mode: "session", Target: name, Session: name, Name: name, Role: role}
	case "window":
		session, err := m.currentSession()
		if err != nil {
			return fmt.Errorf("window mode requires running inside tmux; start tmux and rerun, or set tmux.mode=session: %w", err)
		}
		windowTarget, err := m.createWindow(session, name, opts.WorktreePath)
		if err != nil {
			return fmt.Errorf("creating tmux window: %w", err)
		}
		created = true
		target = Target{Mode: "window", Target: windowTarget, Session: session, Name: name, Role: role}
	}

	if err := m.labelTarget(target, opts.ProjectRoot, opts.Branch, opts.WorktreePath, role); err != nil {
		return m.rollbackCreatedTarget(target, created, fmt.Errorf("labeling tmux target: %w", err))
	}

	// Step 2: Inject managed env via set-environment (session mode only).
	// This is a fallback for tools that read env vars directly instead of .env files.
	// In window mode, set-environment would leak between windows, so we skip it —
	// tools should read .env/.env.local files instead.
	if mode == "session" && len(opts.Env) > 0 {
		if err := m.injectEnv(target.Target, opts.Env); err != nil {
			return m.rollbackCreatedTarget(target, created, fmt.Errorf("injecting environment: %w", err))
		}
	}

	// Step 3: Filter panes (handle optional)
	panes := filterPanes(cfg.Panes, opts.IncludeAll, opts.IncludeWith)

	// Step 4: Create panes
	if err := m.createPanes(target.Target, opts.WorktreePath, panes, cfg); err != nil {
		return m.rollbackCreatedTarget(target, created, fmt.Errorf("creating panes: %w", err))
	}

	// Step 5: Attach/switch if requested
	if opts.Attach {
		if err := m.AttachTarget(target); err != nil {
			return m.rollbackCreatedTarget(target, created, fmt.Errorf("attaching to tmux: %w", err))
		}
	}

	return nil
}

// HasSession checks if a tmux session with the given name exists.
func (m *Manager) HasSession(name string) bool {
	_, err := m.runner.Run("has-session", "-t", name)
	return err == nil
}

func (m *Manager) rollbackCreatedTarget(target Target, created bool, originalErr error) error {
	if !created {
		return originalErr
	}

	var cleanupErr error
	switch target.Mode {
	case "session":
		_, cleanupErr = m.runner.Run("kill-session", "-t", target.Target)
	case "window":
		cleanupErr = m.killWindowPreservingSession(target)
	}
	if cleanupErr != nil {
		return fmt.Errorf("%w; rollback failed: %w", originalErr, cleanupErr)
	}
	return originalErr
}

func (m *Manager) labelTarget(target Target, projectRoot, branch, worktreePath, role string) error {
	labels := []struct {
		key string
		val string
	}{
		{optionProjectRoot, projectRoot},
		{optionBranch, branch},
		{optionWorktree, worktreePath},
		{optionRole, role},
	}

	for _, label := range labels {
		var err error
		if target.Mode == "session" {
			_, err = m.runner.Run("set", "-t", target.Target, label.key, label.val)
		} else {
			_, err = m.runner.Run("setw", "-t", target.Target, label.key, label.val)
		}
		if err != nil {
			return fmt.Errorf("setting %s: %w", label.key, err)
		}
	}
	return nil
}

// FindCanonical finds Grove's labeled canonical target for a worktree.
func (m *Manager) FindCanonical(projectRoot, branch, worktreePath, mode string) (Target, bool, error) {
	targets, err := m.FindTargets(projectRoot, branch, worktreePath)
	if err != nil {
		return Target{}, false, err
	}
	for _, target := range targets {
		if mode != "" && target.Mode != mode {
			continue
		}
		if target.Role == RoleCanonical {
			return target, true, nil
		}
	}
	return Target{}, false, nil
}

// FindTargets returns all Grove-labeled tmux targets for a worktree.
func (m *Manager) FindTargets(projectRoot, branch, worktreePath string) ([]Target, error) {
	sessions, err := m.findSessionTargets(projectRoot, branch, worktreePath)
	if err != nil {
		return nil, err
	}
	windows, err := m.findWindowTargets(projectRoot, branch, worktreePath)
	if err != nil {
		return nil, err
	}
	return append(sessions, windows...), nil
}

func (m *Manager) findSessionTargets(projectRoot, branch, worktreePath string) ([]Target, error) {
	format := "#{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"
	out, err := m.runner.Run("list-sessions", "-F", format)
	if err != nil {
		if isNoTmuxServerError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}

	var targets []Target
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}
		if !labelMatch(fields[1], fields[2], fields[3], projectRoot, branch, worktreePath) {
			continue
		}
		targets = append(targets, Target{
			Mode:    "session",
			Target:  fields[0],
			Session: fields[0],
			Name:    fields[0],
			Role:    fields[4],
		})
	}
	return targets, nil
}

func (m *Manager) findWindowTargets(projectRoot, branch, worktreePath string) ([]Target, error) {
	format := "#{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"
	out, err := m.runner.Run("list-windows", "-a", "-F", format)
	if err != nil {
		if isNoTmuxServerError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tmux windows: %w", err)
	}

	var targets []Target
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		if !labelMatch(fields[3], fields[4], fields[5], projectRoot, branch, worktreePath) {
			continue
		}
		targets = append(targets, Target{
			Mode:    "window",
			Target:  fields[0],
			Session: fields[1],
			Name:    fields[2],
			Role:    fields[6],
		})
	}
	return targets, nil
}

func isNoTmuxServerError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no server running") ||
		strings.Contains(message, "failed to connect to server") ||
		strings.Contains(message, "no sessions")
}

func labelMatch(gotProjectRoot, gotBranch, gotWorktree, projectRoot, branch, worktreePath string) bool {
	if gotBranch != branch {
		return false
	}
	if projectRoot != "" && !samePathLabel(gotProjectRoot, projectRoot) {
		return false
	}
	if worktreePath != "" && !samePathLabel(gotWorktree, worktreePath) {
		return false
	}
	return true
}

func samePathLabel(a, b string) bool {
	if a == b {
		return true
	}
	return normalizePathLabel(a) == normalizePathLabel(b)
}

func normalizePathLabel(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// DestroyLabeled kills all Grove-labeled tmux targets for a worktree.
func (m *Manager) DestroyLabeled(projectRoot, branch, worktreePath string) (bool, error) {
	targets, err := m.FindTargets(projectRoot, branch, worktreePath)
	if err != nil {
		return false, err
	}
	return m.DestroyTargets(targets)
}

// DestroyTargets removes exact targets previously returned by FindTargets.
// Callers may discover targets before deleting a worktree, then safely destroy
// those exact IDs after the path no longer exists.
func (m *Manager) DestroyTargets(targets []Target) (bool, error) {
	if len(targets) == 0 {
		return false, nil
	}

	killed := false
	killedSessions := map[string]bool{}
	var firstErr error
	for _, target := range targets {
		if target.Mode != "session" {
			continue
		}
		if killedSessions[target.Session] {
			continue
		}
		if _, err := m.runner.Run("kill-session", "-t", target.Target); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("killing tmux session %q: %w", target.Target, err)
			}
			continue
		}
		killed = true
		killedSessions[target.Session] = true
	}
	for _, target := range targets {
		if target.Mode != "window" || killedSessions[target.Session] {
			continue
		}
		if err := m.killWindowPreservingSession(target); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		killed = true
	}

	return killed, firstErr
}

func (m *Manager) killWindowPreservingSession(target Target) error {
	if err := m.ensureWindowKillPreservesSession(target); err != nil {
		return err
	}
	if _, err := m.runner.Run("kill-window", "-t", target.Target); err != nil {
		return fmt.Errorf("killing tmux window %q: %w", target.Target, err)
	}
	return nil
}

func (m *Manager) ensureWindowKillPreservesSession(target Target) error {
	count, err := m.sessionWindowCount(target.Session)
	if err != nil {
		return fmt.Errorf("querying tmux window count for session %q before killing %q: %w", target.Session, target.Target, err)
	}
	if count != 1 {
		return nil
	}
	if _, err := m.runner.Run("new-window", "-d", "-t", target.Session, "-n", "grove-placeholder"); err != nil {
		return fmt.Errorf("creating placeholder window in tmux session %q before killing %q: %w", target.Session, target.Target, err)
	}
	return nil
}

func (m *Manager) sessionWindowCount(session string) (int, error) {
	out, err := m.runner.Run("list-windows", "-t", session, "-F", "#{window_id}")
	if err != nil {
		return 0, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return 0, nil
	}
	return len(strings.Split(out, "\n")), nil
}

// createSession creates a new detached tmux session.
func (m *Manager) createSession(name, workdir string) error {
	_, err := m.runner.Run("new-session", "-d", "-s", name, "-c", workdir)
	return err
}

// createWindow creates a new window in the given session and returns its exact window ID.
// It never adopts an existing same-name window; tmux may create duplicate window names.
func (m *Manager) createWindow(session, name, workdir string) (string, error) {
	out, err := m.runner.Run("new-window", "-P", "-F", "#{window_id}", "-t", session, "-n", name, "-c", workdir)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("created tmux window %q in session %q but tmux did not return an exact window id", name, session)
	}
	return out, nil
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

// AttachTarget attaches to or switches to a specific labeled tmux target.
func (m *Manager) AttachTarget(target Target) error {
	if target.Mode == "session" {
		return m.doAttach(target.Target, "session")
	}

	if os.Getenv("TMUX") != "" {
		_, err := m.runner.Run("select-window", "-t", target.Target)
		return err
	}

	if target.Session == "" {
		_, err := m.runner.Run("attach", "-t", target.Target)
		return err
	}
	_, _ = m.runner.Run("select-window", "-t", target.Target)
	_, err := m.runner.Run("attach", "-t", target.Session)
	return err
}

// doAttach implements the attach/switch logic.
func (m *Manager) doAttach(name, mode string) error {
	if os.Getenv("TMUX") != "" {
		if mode == "session" {
			// Already inside tmux — switch to the target session.
			_, err := m.runner.Run("switch-client", "-t", name)
			return err
		}

		// Window mode inside tmux — select the actual window target instead of
		// treating the branch name like a session target. This avoids collisions
		// when a separate tmux session already exists with the same name.
		sessionName, err := m.findWindowSession(name)
		if err == nil {
			_, err = m.runner.Run("select-window", "-t", sessionName+":"+name)
			return err
		}

		currentSession, currentErr := m.currentSession()
		if currentErr == nil {
			_, err = m.runner.Run("select-window", "-t", currentSession+":"+name)
			return err
		}

		_, err = m.runner.Run("select-window", "-t", name)
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
