# Grove — make work trees as easy as a walk in the woods

![Grove Banner](assets/grove-banner.png)

[![License: MIT](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![tmux](https://img.shields.io/badge/tmux-3.2+-1BB91F?style=flat-square)](https://github.com/tmux/tmux)

For tmux users who want the worktree convenience of GUI tools like T3 Code and Codex — automatic port assignments for parallel dev servers, isolated environments, and full tmux workspaces, all from one command.

```bash
grove create feat/auth
# worktree + ports + env + tmux — done
```

- **Deterministic ports** — default branch uses base ports, others get a stable hash offset with no collisions
- **Layered env** — `.env` files symlinked from the main repo, `.env.local` generated per-branch with ports, `{{service.port}}` and `{{branch}}` templates
- **Tmux workspaces** — from a flat pane list to explicit splits to raw layout strings, four tiers of control
- **Agent-ready** — auto-JSON when piped, structured errors on stderr, `--dry-run` on destructive commands
- **Works from anywhere** — run grove commands from any worktree, not just the main repo

## Install

```bash
# One-line install for Go users
go install github.com/lukemelnik/grove/cmd/grove@latest

# Or build locally
git clone https://github.com/lukemelnik/grove.git
cd grove
make install
```

Prebuilt archives are also published on each GitHub release:
<https://github.com/lukemelnik/grove/releases/latest>

`go install` writes `grove` to `$GOBIN` or `$GOPATH/bin`, so make sure that directory is on your `PATH`.

### Shell Completions

```bash
# Bash
grove completion bash > $(brew --prefix)/etc/bash_completion.d/grove  # macOS
grove completion bash > /etc/bash_completion.d/grove                  # Linux

# Zsh
grove completion zsh > "${fpath[1]}/_grove"

# Fish
grove completion fish > ~/.config/fish/completions/grove.fish
```

## Quick Start

```bash
cd your-project
grove init          # Interactive setup — creates .grove.yml
grove create feat/auth   # Create worktree + workspace
grove create feat/billing
grove list               # See all active worktrees and ports
grove attach feat/auth   # Jump back to an existing workspace
grove delete feat/billing # Clean up when done
grove clean              # Remove worktrees for merged/deleted branches
```

## Configuration

Grove is configured via `.grove.yml` in your project root.

`worktree_dir` is optional. If you omit it, Grove defaults to `../.grove-worktrees/<repo-name>`. Only set it when you want a different location.

### Minimal

```yaml
services:
  app:
    port:
      base: 3000
      env: PORT
```

### Full Example

```yaml
env_files:
  - .env

services:
  api:
    env_file: apps/api/.env
    port:
      base: 4000
      env: PORT
    env:
      CORS_ORIGIN: "http://localhost:{{web.port}}"
  web:
    env_file: apps/web/.env
    port:
      base: 3000
      env: WEB_PORT
    env:
      VITE_API_URL: "http://localhost:{{api.port}}"
      VITE_APP_URL: "http://localhost:{{web.port}}"
      VITE_WORKTREE_NAME: "{{branch}}"

tmux:
  mode: window
  layout: main-vertical
  main_size: "70%"
  panes:
    - nvim
    - claude --model sonnet
    - cmd: pnpm dev
      setup: pnpm install
      name: dev
      optional: true
```

### Config Reference

| Field | Default | Description |
|-------|---------|-------------|
| `worktree_dir` | `../.grove-worktrees/<repo-name>` | Optional override for where worktrees are created (relative to project root) |
| `env_files` | — | Env files to symlink (for files not tied to a service) |
| `services` | — | Services with ports, env files, and env vars |
| `services.<name>.port.base` | — | Base port number (1-65535) |
| `services.<name>.port.env` | — | Env var name for the assigned port |
| `services.<name>.env_file` | — | The `.env` file for this service (auto-symlinked). Required if you use `services.<name>.env`. |
| `services.<name>.env` | — | Additional env vars scoped to this service's `.env.local` |
| `env` | — | Global env vars (written to all `.env.local` files) |
| `tmux` | — | Tmux workspace configuration |

## Commands

### `grove schema`

Print the full annotated `.grove.yml` reference — every field, its type, default, and description.

```bash
grove schema           # print full config reference
grove schema > .grove.yml  # use as a starting template
```

### `grove init`

Create `.grove.yml` interactively or via flags. Non-interactive mode is useful for agents and scripts.

```bash
grove init                                          # interactive
grove init --service api:4000:PORT --pane nvim      # non-interactive
grove init --service api:4000:PORT --service web:3000:WEB_PORT \
  --env-file .env --pane nvim --pane "pnpm dev:dev:optional"
```

Service format: `name:port:ENV_VAR`. Pane format: `command[:name[:optional]]`.
If you omit `worktree_dir`, Grove defaults to `../.grove-worktrees/<repo-name>`. Use `--worktree-dir` only to override that location.

### `grove create <branch>`

Creates a git worktree, assigns deterministic ports, resolves environment variables, and optionally sets up a tmux workspace.

```bash
grove create feat/auth                      # Create from default base branch
grove create feat/auth --from develop       # Create from specific base
grove create feat/auth --no-tmux            # Skip tmux, just create worktree
grove create feat/auth --all                # Include optional panes
grove create feat/auth --with dev --with test  # Include specific optional panes
```

**Branch resolution:**
- If the branch exists locally, use it
- If it exists on the remote, create a local tracking branch
- If it doesn't exist anywhere, create a new branch from the base ref

**Flags:**

| Flag | Description |
|------|-------------|
| `--from <ref>` | Base branch for new branches (default: auto-detected, usually `origin/main`) |
| `--no-tmux` | Skip tmux workspace creation |
| `--all` | Include all optional panes |
| `--with <name>` | Include specific optional pane by name (repeatable) |
| `--json` | Output as JSON |
| `--attach` | Auto-attach to tmux (default: true) |

### `grove attach <branch>`

Jump back to an existing worktree. If a tmux session/window is running, switches to it. If not, creates one and attaches.

```bash
grove attach feat/auth
```

### `grove delete <branch>`

Removes the worktree, kills the tmux session/window, and deletes the local branch.

```bash
grove delete feat/auth              # Safety checks first, then delete
grove delete feat/auth --force      # Skip all safety checks
grove delete feat/auth --keep-branch  # Remove worktree but keep the git branch
```

**Safety checks** (all skipped with `--force`):
- **Open PRs** — checks via `gh` CLI (if available)
- **Unpushed commits** — blocks if the branch has local commits not on the remote
- **Never-pushed branches** — blocks if the branch has no remote tracking branch
- **Uncommitted changes** — git refuses to remove dirty worktrees

### `grove list`

List all active worktrees with their branches, paths, and port assignments.

```bash
grove list
```

```
Branch:   feat/auth
Worktree: /path/to/.grove-worktrees/my-project/feat-auth
Ports:
  api: 4045
  web: 3045

Branch:   feat/billing
Worktree: /path/to/.grove-worktrees/my-project/feat-billing
Ports:
  api: 4092
  web: 3092
```

### `grove clean`

Batch cleanup of stale worktrees. Each branch is labeled with a reason:

| Reason | Meaning | Detection |
|--------|---------|-----------|
| **gone** | Remote tracking branch was deleted (e.g. after merging a PR) | `git branch -vv` — works with any merge strategy |
| **merged** | Branch is fully merged into the default branch | `git branch --merged` — regular merges only, not squash/rebase |
| **unchanged** | Branch has no unique commits beyond the default branch | `git rev-list --count` — created but never worked on |

**Tip:** If you use squash or rebase merges, enable **"Automatically delete head branches"** in your GitHub repo settings (Settings → General). This ensures remote branches are deleted after merging, so `grove clean` reliably detects them as "gone".

```bash
grove clean              # Interactive — shows what would be cleaned, asks for confirmation
grove clean --dry-run    # Just show what would be cleaned
grove clean --force      # Skip confirmation
```

For each stale worktree, grove kills the tmux session/window, removes the worktree, deletes the local branch, and prunes git worktree metadata.

### `grove status`

Show info for the current worktree (detected from your working directory).

```bash
grove status
```

```
Branch:   feat/auth
Worktree: /path/to/.grove-worktrees/my-project/feat-auth
Ports:
  api: 4045
  web: 3045
```

## How It Works

### Config Discovery

Grove walks up from your current directory looking for `.grove.yml`. If it doesn't find one (e.g. you're in a worktree), it falls back to the main repo root via `git rev-parse --git-common-dir`. This means `.grove.yml` can live only in the main repo — untracked, even — and all commands still work from any worktree.

### Port Assignment

Ports are assigned **deterministically** based on the branch name. The same branch always gets the same ports, no matter when or how many times you run `grove create`.

The **default branch** (main/master) uses the base ports directly — `api: 4000` and `web: 3000`. All other branches get a hash-based offset:

```
offset = md5(branch_name) mod 3000
assigned_port = base_port + offset
```

For example, `feat/auth` might get offset 45, giving you `api: 4045` and `web: 3045`.

All services share the same offset, so port relationships are preserved. Browser-restricted ports (from the WHATWG Fetch spec) are automatically avoided — and rejected at config time if used as base ports.

### Environment Variables

In worktrees, `.env` files are **symlinked** from the main repo so secrets stay in one place. Grove writes `.env.local` files next to each symlink with branch-specific port assignments and template-resolved values. Most frameworks (Vite, Next.js, CRA, Rails) load `.env.local` and override `.env` automatically.

```
main repo                          worktree (feat/auth)
├── apps/api/.env (secrets)        ├── apps/api/.env → symlink to main repo
│                                  ├── apps/api/.env.local (PORT=4045, CORS_ORIGIN=...)
├── apps/web/.env (secrets)        ├── apps/web/.env → symlink to main repo
│                                  ├── apps/web/.env.local (WEB_PORT=3045, VITE_API_URL=...)
```

Each service declares its own `env_file` and `env` vars. Service-scoped vars are written only to that service's `.env.local` — no cross-contamination. If you use `services.<name>.env`, you must also set `services.<name>.env_file` so Grove knows which `.env.local` to write.

**Template variables:**

| Template | Resolves to |
|----------|-------------|
| `{{service.port}}` | The assigned port for a service (e.g. `{{api.port}}` → `4045`) |
| `{{branch}}` | The worktree branch name (e.g. `feat/auth`) |

In session mode, grove also sets top-level `env` vars plus each service's port env var via `tmux set-environment` as a fallback for tools that read env vars directly instead of `.env` files. Service-scoped `services.<name>.env` values stay in that service's `.env.local`.

```yaml
services:
  api:
    env_file: apps/api/.env           # Symlinked; .env.local written next to it
    port:
      base: 4000
      env: PORT                       # PORT=4045 in apps/api/.env.local
    env:
      CORS_ORIGIN: "http://localhost:{{web.port}}"  # Scoped to api's .env.local
  web:
    env_file: apps/web/.env
    port:
      base: 3000
      env: WEB_PORT
    env:
      VITE_API_URL: "http://localhost:{{api.port}}"  # Scoped to web's .env.local
```

## Tmux Layouts

Grove supports four tiers of layout complexity. Use the simplest one that fits your needs.

### Preset

Just list your panes. Grove applies a tmux preset layout.

```yaml
tmux:
  layout: main-vertical    # or: even-horizontal, even-vertical, main-horizontal, tiled
  panes:
    - nvim
    - claude
    - pnpm dev
```

### Preset + Size

Control the main pane size.

```yaml
tmux:
  layout: main-vertical
  main_size: "70%"
  panes:
    - nvim
    - claude
    - pnpm dev
```

### Explicit Splits

Define the exact split structure with nested containers.

- `split: horizontal` arranges children left-to-right
- `split: vertical` arranges children top-to-bottom
- Child order matters: first child is left/top, second is right/bottom
- `size` applies along the split axis: width for `horizontal`, height for `vertical`
- To subdivide only one region further, nest another `split` inside that child

```yaml
tmux:
  panes:
    - cmd: nvim
      size: "70%"
    - split: vertical
      panes:
        - cmd: claude
          size: "60%"
        - cmd: pnpm dev
```

Example: two side-by-side `pi` panes with a small full-width terminal on the bottom.

```yaml
tmux:
  panes:
    - split: vertical
      panes:
        - split: horizontal
          panes:
            - pi
            - pi
        - cmd: ""
          name: terminal
          size: "20%"
```

### Raw Layout String

Paste a tmux layout string directly (from `tmux list-windows`).

```yaml
tmux:
  layout: "a]180x50,0,0{120x50,0,0,0,59x50,121,0[59x25,121,0,1,59x24,121,26,2]}"
  panes:
    - nvim
    - claude
    - pnpm dev
```

### Optional Panes

Mark panes as optional to skip them by default. Include them selectively with `--all` or `--with`.

```yaml
tmux:
  panes:
    - nvim
    - claude
    - cmd: pnpm dev
      name: dev
      optional: true
    - cmd: lazygit
      name: git
      optional: true
```

```bash
grove create feat/auth               # Just nvim + claude
grove create feat/auth --with dev    # nvim + claude + pnpm dev
grove create feat/auth --all         # Everything
```

### Setup Commands

Run a prerequisite before the main command — like `pnpm install` before `pnpm dev`. Setup always executes; the main command follows based on `autorun`.

```yaml
tmux:
  panes:
    - nvim
    - cmd: pnpm dev
      setup: pnpm install       # runs first, then pnpm dev starts
    - cmd: pnpm test
      setup: pnpm install
      autorun: false             # install runs, then "pnpm test" is typed but not executed
```

| `setup` | `cmd` | `autorun` | What happens |
|---------|-------|-----------|-------------|
| — | X | true | Runs `X` |
| — | X | false | Types `X`, waits for Enter |
| S | — | — | Runs `S` |
| S | X | true | Runs `S && X` |
| S | X | false | Runs `S`, then types `X` |

### Session vs Window Mode

```yaml
tmux:
  mode: session    # Each worktree gets its own tmux session
  # or
  mode: window     # Each worktree gets a window in your current tmux session (default)
```

**Window mode** (default) keeps all worktrees as windows in one tmux session. Run `grove create` from inside tmux so Grove knows which session to add the new window to. Each worktree gets its own `.env.local` files with branch-specific ports, so environment doesn't leak between windows.

**Session mode** gives each worktree a fully isolated tmux session. Grove also injects top-level `env` vars plus each service's port env var via `tmux set-environment` as a fallback for tools that read env vars directly. Service-scoped `services.<name>.env` values stay in each service's `.env.local`.

## Agent / CI Usage

Grove is designed to be fully operable by AI agents and scripts without human interaction.

**Discover the config format** — no docs needed:
```bash
grove schema            # full annotated .grove.yml reference
grove create --help     # explains branch resolution, optional panes, all flags
```

**Create configs non-interactively:**
```bash
grove init --service api:4000:PORT --service web:3000:WEB_PORT \
  --env-file .env --pane nvim --pane "pnpm dev:dev:optional"
```

**Structured output** — auto-JSON when piped, structured errors on stderr:
```bash
grove list              # terminal: human-readable text
output=$(grove list)    # piped: JSON automatically
grove list --json       # force JSON in terminal
```

**Safe mutations** — `grove clean --dry-run` previews before acting, `--force` skips prompts.

## Example Configs

### Single Service (Next.js, Rails, etc.)

```yaml
services:
  app:
    port:
      base: 3000
      env: PORT

tmux:
  panes:
    - nvim
    - pnpm dev
```

### Frontend + API

```yaml
services:
  api:
    env_file: apps/api/.env
    port:
      base: 4000
      env: PORT
    env:
      CORS_ORIGIN: "http://localhost:{{web.port}}"
  web:
    env_file: apps/web/.env
    port:
      base: 3000
      env: WEB_PORT
    env:
      VITE_API_URL: "http://localhost:{{api.port}}"

tmux:
  layout: main-vertical
  main_size: "70%"
  panes:
    - nvim
    - cmd: pnpm --filter api dev
      name: api
    - cmd: pnpm --filter web dev
      name: web
```

### Microservices

```yaml
services:
  gateway:
    port:
      base: 8080
      env: GATEWAY_PORT
  users:
    port:
      base: 8081
      env: USERS_PORT
  billing:
    port:
      base: 8082
      env: BILLING_PORT
  frontend:
    port:
      base: 3000
      env: FRONTEND_PORT

env:
  API_URL: "http://localhost:{{gateway.port}}"

tmux:
  layout: tiled
  panes:
    - nvim
    - docker compose up
    - claude
    - cmd: lazygit
      optional: true
      name: git
```

## Trust Model

Grove uses an **explicit-invocation** model — nothing happens until you run `grove create` or `grove attach`. This is the same model as `make`, `npm run`, `docker compose up`, and similar tools: you review the config, then run the command.

**Pane commands are code.** The `cmd` and `setup` fields in `tmux.panes` are executed as shell commands. Review `.grove.yml` before running `grove create` in an unfamiliar repo, just as you would review a `Makefile` or `package.json` scripts.

**Env files are constrained.** `env_files` paths must be relative to the project root and cannot escape it — absolute paths and `../` prefixes are rejected at config validation time.

## Releasing

Grove uses SemVer tags as the release source of truth:

- `v0.4.1` for fixes and security patches
- `v0.5.0` for backward-compatible features
- `v1.0.0` for breaking changes
- `v0.6.0-rc.1` for prereleases

Create a release with the helper script:

```bash
./scripts/release.sh minor --push

# Or via make
make release-minor PUSH=1
```

The helper script requires a clean working tree, finds the latest stable `v*` tag, computes the next version, and creates an annotated git tag. If there are no prior tags, it starts from `0.0.0`, so `minor` produces the recommended first public release: `v0.1.0`.

If you prefer, you can still tag manually:

```bash
git tag -a v0.4.1 -m "v0.4.1"
git push origin v0.4.1
```

Any pushed `v*` tag runs the release workflow, executes `go test ./...`, and publishes GitHub release assets with GoReleaser. Local `make build` also embeds a version string automatically from the nearest git tag, or you can override it with `make build VERSION=0.4.1`.

Homebrew is intentionally not part of the first release cut. A tap is convenient for macOS users, but it adds a second repository, publishing credentials, and ongoing formula maintenance. `go install` plus GitHub Releases stays cross-platform and is the lowest-complexity path until Grove has enough release volume to justify the extra packaging surface.

## Requirements

- Git
- tmux 3.2+ (for workspace features)
- Go 1.25+ (for source builds or `go install`)
- `gh` CLI (optional — PR safety checks on `grove delete`)

## License

MIT
