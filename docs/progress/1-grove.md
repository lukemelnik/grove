# Grove — Deterministic Worktree Workspaces

## Sprint Progress

### Sprint 1: Project Scaffolding, CLI Framework, Config Parsing & Discovery
- [x] Initialize Go module (`grove`)
- [x] Install dependencies (cobra, yaml.v3)
- [x] Set up Cobra CLI with root command
- [x] Add subcommand stubs: `init`, `create`, `attach`, `delete`, `list`, `status`
- [x] Define all CLI flags per spec (`create`: -e, --from, --no-tmux, --all, --with, --json, --attach; `delete`: --force, --keep-branch; `list`/`status`: --json)
- [x] Implement `.grove.yml` config parsing with full schema (services, env, env_files, tmux with all 4 layout tiers, worktree_dir, optional panes)
- [x] Implement custom YAML unmarshaling for Pane (string and map forms)
- [x] Implement config validation (port range, required env var, tmux mode)
- [x] Implement config discovery (walk up from cwd to find `.grove.yml`)
- [x] Write unit tests for config parsing (19 tests covering all config variants, validation errors, edge cases)
- [x] Write unit tests for config discovery (current dir, parent dir, not found)

**Status: COMPLETE**

### Sprint 2: Port Hashing, Port Availability, Env Resolution
- [x] Implement deterministic port hashing (MD5 branch name -> offset)
- [x] Implement port availability checking with collision avoidance
- [x] Implement blocked port list (browser-restricted ports)
- [x] Implement env file reading (.env parsing)
- [x] Implement env resolution order (env_files -> env block -> services ports -> -e flags)
- [x] Implement template syntax resolution ({{service.port}})
- [x] Write tests for port hashing, env resolution, template expansion

**Status: COMPLETE**

### Sprint 3: Worktree Management & `grove create` Command
- [x] Implement worktree creation (git worktree add)
- [x] Implement branch resolution (local, remote tracking, new from ref)
- [x] Implement worktree path calculation (branch name sanitization, slashes → dashes)
- [x] Implement `--from <ref>` flag to override base branch for new branches
- [x] Implement worktree deletion (git worktree remove + optional branch cleanup)
- [x] Implement worktree listing (porcelain parsing)
- [x] Implement current worktree detection (from cwd, with symlink resolution)
- [x] Implement GitRunner interface for testability (mock + real implementations)
- [x] Wire up `grove create` command: config discovery → port assignment → env resolution → worktree creation
- [x] Implement `--no-tmux` mode: print worktree path, ports, and env to stdout
- [x] Implement `--json` mode: structured JSON output (`{"worktree", "branch", "ports", "env"}`)
- [x] Wire up all flags: `-e`, `--from`, `--no-tmux`, `--json`, `--attach`, `--all`, `--with`
- [x] `--all` and `--with` flags accepted but deferred to Sprint 4 (tmux)
- [x] Write unit tests for worktree functions (SanitizeBranchName, WorktreePath, parseWorktreeList, BranchResolution.String)
- [x] Write mock-based tests for Manager (create existing/local/remote/new, remove, list)
- [x] Write integration tests with real git repos (create local/new/reuse, remove, remove+branch, list, FindByPath)
- [x] Write integration tests for `grove create` command (text output, JSON output, env overrides, --from, reuse, minimal config, missing arg)

**Status: COMPLETE**

### Sprint 4: Tmux Integration
- [x] Implement tmux Runner interface for testability (mock + real implementations via os/exec)
- [x] Implement session creation (`tmux new-session -d -s <name> -c <path>`)
- [x] Implement window creation (`tmux new-window -t <session>:<name> -c <path>`)
- [x] Implement window mode fallback to session mode when not inside tmux
- [x] Implement session/window naming (branch name slashes → dashes)
- [x] Implement environment injection (`tmux set-environment`) before pane creation
- [x] Implement Tier 1: preset layout (`select-layout` with preset name, default `main-vertical`)
- [x] Implement Tier 2: preset with size hint (`set-option main-pane-width/height` before `select-layout`)
- [x] Implement Tier 3: explicit splits (recursive split tree with `-h`/`-v` flags and `-p` percentage)
- [x] Implement Tier 4: raw tmux layout string (applied via `select-layout`)
- [x] Implement pane command execution (`send-keys`)
- [x] Implement optional pane filtering (`--all` includes all, `--with <name>` includes by name/index)
- [x] Implement nested split container optional pane filtering
- [x] Implement attach behavior (`tmux attach` outside tmux, `switch-client` inside tmux)
- [x] Implement session/window cleanup on delete (`kill-session` / `kill-window`)
- [x] Wire tmux integration into `grove create` (after worktree + env resolution, before output)
- [x] Write 29 unit tests for tmux command generation covering all tiers, modes, env injection, optional panes, attach, destroy

**Status: COMPLETE**

### Sprint 5: Remaining Commands — `grove attach`, `grove delete`, `grove list`, `grove status`
- [x] `grove create` tmux integration wired up in Sprint 4
- [x] Implement `grove attach` (worktree check, tmux session/window detection, create-and-attach if needed, error with "did you mean grove create?" if no worktree)
- [x] Implement `grove delete` (PR check via `gh`, --force to skip, --keep-branch, tmux cleanup, worktree removal, branch deletion)
- [x] Implement `grove list` (text and --json output, port computation per branch, sorted output)
- [x] Implement `grove status` (detect worktree from cwd, show branch/path/ports/env, --json output)
- [x] Add `HasSession`, `HasWindow`, `Attach` methods to tmux Manager
- [x] Add `ghCommandRunner` and `ghAvailable` vars for testable `gh` integration
- [x] Implement `grove init` (interactive setup — deferred to Sprint 6)
- [x] Write integration tests for `grove attach` (no worktree error, no tmux config, tmux session creation, missing arg)
- [x] Write integration tests for `grove delete` (basic, keep-branch, open PR aborts, force with open PR, gh not available, missing arg, checkOpenPRs unit tests)
- [x] Write integration tests for `grove list` (empty, with worktrees, JSON output, no services)
- [x] Write integration tests for `grove status` (inside worktree, JSON, not inside worktree, main worktree, minimal config)
- [x] Write unit tests for tmux HasSession, HasWindow, Attach methods

**Status: COMPLETE**

### Sprint 6: `grove init`, Shell Completions, and Final Polish
- [x] Implement `grove init` interactive setup (services, worktree dir, env files, tmux config)
- [x] Use `bufio.Scanner` for stdin input with testable `stdinReader` var
- [x] Handle existing `.grove.yml` with overwrite confirmation
- [x] Validate port input, default env var names, .env file detection
- [x] Add `grove completion <shell>` command (bash, zsh, fish via Cobra built-in)
- [x] Add `--version` flag to root command (set via `Version` var / ldflags at build time)
- [x] Consistent exit codes: 0 success, 1 error (via `SilenceUsage`/`SilenceErrors` + `os.Exit(1)` in main)
- [x] Consistent `--json` output across `create`, `list`, `status` commands
- [x] Write 7 unit tests for `grove init` (full interactive, minimal, overwrite abort/confirm, invalid port, default env var, .env detection)
- [x] Write 6 unit tests for shell completions and version flag (bash, zsh, fish, invalid shell, no args, --version)

**Status: COMPLETE**

## Alerts
- No alerts for Sprint 1.
- Sprint 2: Fixed .env inline comment stripping — docstring claimed support but implementation was missing. Added implementation and test (`TestParseEnvContent_InlineComments`).
- Sprint 3: Pulled `grove create` wiring (non-tmux flow) forward from Sprint 5 into Sprint 3, since the worktree + port + env pipeline naturally completes here. Sprint 5 retains tmux-integrated `grove create` and other commands.
- Sprint 4: Pulled `grove create` tmux wiring forward from Sprint 5 into Sprint 4, since it naturally completes with the tmux manager. Sprint 5 marked that item as done. Added `tmuxRunnerFactory` var to `create.go` for test overridability.
- Sprint 5: Deferred `grove init` (interactive setup) to Sprint 6 — it is independent of the other commands and fits better with polish/distribution. Added public `Attach` method to tmux Manager (refactored private `attach` to `doAttach`) for use by the `grove attach` command. Port computation in `grove list` and `grove status` uses a pass-through port checker (always returns true) since we want deterministic assignment, not availability checking, when displaying info.
- Sprint 5 review: Fixed `HasWindow` false positive — some tmux versions return exit 0 with empty output when no windows match; now checks for non-empty output. Fixed `doAttach` in window mode outside tmux — `select-window` requires an active client, so now finds the parent session via `list-windows`, selects the window, and attaches to the session. Added tests for both fixes.
- Sprint 6: GoReleaser and Homebrew tap setup deferred — these are distribution concerns that require repository infrastructure (GitHub Actions, a separate tap repo). The CLI itself is feature-complete. Marked `grove init` in Sprint 5's checklist as done since it was implemented here. All 6 sprints are now complete.
