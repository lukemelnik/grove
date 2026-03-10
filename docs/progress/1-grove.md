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
- [ ] Implement session/window creation
- [ ] Implement environment injection (tmux set-environment)
- [ ] Implement pane creation with all 4 layout tiers
- [ ] Implement optional pane filtering (--all, --with)
- [ ] Implement attach/switch behavior
- [ ] Implement session/window cleanup on delete
- [ ] Write tests for tmux command generation

### Sprint 5: Command Implementation (wire everything together)
- [ ] Implement `grove create` tmux integration (full pipeline; non-tmux flow done in Sprint 3)
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
