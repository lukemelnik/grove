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

### Sprint 5: Command Implementation (wire everything together)
- [x] `grove create` tmux integration wired up in Sprint 4
- [ ] Implement `grove attach`
- [ ] Implement `grove delete` (with PR check via gh)
- [ ] Implement `grove list` (with --json)
- [ ] Implement `grove status` (with --json)
- [ ] Implement `grove init` (interactive setup)
- [ ] Write integration tests

### Sprint 6: Polish & Distribution
- [ ] Add shell completions (bash, zsh, fish)
- [ ] Set up GoReleaser
- [ ] Set up Homebrew tap
- [ ] Add error messages and user-facing output polish
- [ ] Final testing and documentation

## Alerts
- No alerts for Sprint 1.
- Sprint 2: Fixed .env inline comment stripping — docstring claimed support but implementation was missing. Added implementation and test (`TestParseEnvContent_InlineComments`).
- Sprint 3: Pulled `grove create` wiring (non-tmux flow) forward from Sprint 5 into Sprint 3, since the worktree + port + env pipeline naturally completes here. Sprint 5 retains tmux-integrated `grove create` and other commands.
- Sprint 4: Pulled `grove create` tmux wiring forward from Sprint 5 into Sprint 4, since it naturally completes with the tmux manager. Sprint 5 marked that item as done. Added `tmuxRunnerFactory` var to `create.go` for test overridability.
