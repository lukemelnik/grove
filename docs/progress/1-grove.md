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
- [ ] Implement deterministic port hashing (MD5 branch name -> offset)
- [ ] Implement port availability checking with collision avoidance
- [ ] Implement blocked port list (browser-restricted ports)
- [ ] Implement env file reading (.env parsing)
- [ ] Implement env resolution order (env_files -> env block -> services ports -> -e flags)
- [ ] Implement template syntax resolution ({{service.port}})
- [ ] Write tests for port hashing, env resolution, template expansion

### Sprint 3: Worktree Management
- [ ] Implement worktree creation (git worktree add)
- [ ] Implement branch resolution (local, remote tracking, new from ref)
- [ ] Implement worktree path calculation (branch name sanitization)
- [ ] Implement worktree deletion (git worktree remove + branch cleanup)
- [ ] Implement worktree listing
- [ ] Implement current worktree detection (from cwd)
- [ ] Write tests for worktree operations

### Sprint 4: Tmux Integration
- [ ] Implement session/window creation
- [ ] Implement environment injection (tmux set-environment)
- [ ] Implement pane creation with all 4 layout tiers
- [ ] Implement optional pane filtering (--all, --with)
- [ ] Implement attach/switch behavior
- [ ] Implement session/window cleanup on delete
- [ ] Write tests for tmux command generation

### Sprint 5: Command Implementation (wire everything together)
- [ ] Implement `grove create` (full pipeline)
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
