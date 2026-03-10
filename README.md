# Grove — isolated workspaces for every branch

![Grove Banner](assets/grove-banner.png)

[![License: MIT](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![tmux](https://img.shields.io/badge/tmux-3.2+-1BB91F?style=flat-square)](https://github.com/tmux/tmux)

One command gives you a git worktree with its own ports, env vars, and tmux layout. Same branch, same workspace, every time.

```bash
grove create feat/auth
# worktree + ports + env + tmux — done
```

- **Deterministic ports** — default branch uses base ports, others get a stable hash offset with no collisions
- **Layered env** — `.env` files, templated vars (`{{api.port}}`), and CLI overrides, all resolved per-branch
- **Tmux workspaces** — from a flat pane list to explicit splits to raw layout strings, four tiers of control
- **Agent-ready** — auto-JSON when piped, structured errors on stderr, `--dry-run` on destructive commands
- **Works from anywhere** — run grove commands from any worktree, not just the main repo

## Install

```bash
# Build and install to ~/.local/bin
git clone https://github.com/lukeroes/grove.git
cd grove
make install

# Or manually
go build -o grove ./cmd/grove
ln -sf "$(pwd)/grove" ~/.local/bin/grove
```

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

### Minimal

```yaml
services:
  app:
    port: 3000
    env: PORT
```

### Full Example

```yaml
worktree_dir: ../.grove-worktrees

env_files:
  - .env
  - apps/api/.env

services:
  api:
    port: 4000
    env: PORT
  web:
    port: 3000
    env: WEB_PORT

env:
  VITE_API_URL: "http://localhost:{{api.port}}"
  CORS_ORIGIN: "http://localhost:{{web.port}}"

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
| `worktree_dir` | `../.grove-worktrees` | Where worktrees are created (relative to project root) |
| `env_files` | — | List of `.env` files to load |
| `services` | — | Services with base ports and env var names |
| `env` | — | Additional env vars (supports `{{service.port}}` templates) |
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

### `grove create <branch>`

Creates a git worktree, assigns deterministic ports, resolves environment variables, and optionally sets up a tmux workspace.

```bash
grove create feat/auth                      # Create from default base branch
grove create feat/auth --from develop       # Create from specific base
grove create feat/auth --no-tmux            # Skip tmux, just create worktree
grove create feat/auth --all                # Include optional panes
grove create feat/auth --with dev --with test  # Include specific optional panes
grove create feat/auth -e DEBUG=true        # Override env vars
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
| `-e, --env KEY=VALUE` | Environment variable override (repeatable) |
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
grove delete feat/auth              # Checks for open PRs first
grove delete feat/auth --force      # Skip safety checks, force delete dirty worktrees
grove delete feat/auth --keep-branch  # Remove worktree but keep the git branch
```

Checks for open PRs via the `gh` CLI before deleting (unless `--force`). If the worktree has uncommitted changes, `--force` is required.

### `grove list`

List all active worktrees with their branches, paths, and port assignments.

```bash
grove list
```

```
Branch:   feat/auth
Worktree: /path/to/.grove-worktrees/feat-auth
Ports:
  api: 4045
  web: 3045

Branch:   feat/billing
Worktree: /path/to/.grove-worktrees/feat-billing
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
Worktree: /path/to/.grove-worktrees/feat-auth
Ports:
  api: 4045
  web: 3045
Env:
  CORS_ORIGIN=http://localhost:3045
  PORT=4045
  VITE_API_URL=http://localhost:4045
  WEB_PORT=3045
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

Environment variables are resolved in layers, with later layers overriding earlier ones:

1. **`.env` files** — all variables from files listed in `env_files`
2. **`env` block** — static values and `{{service.port}}` templates
3. **Service ports** — each service's `env` var is set to its assigned port
4. **`-e` flags** — command-line overrides (highest priority)

```yaml
env_files:
  - .env                          # DATABASE_URL, API_KEY, etc.

services:
  api:
    port: 4000
    env: PORT                     # PORT=4045 (overrides .env)

env:
  VITE_API_URL: "http://localhost:{{api.port}}"   # Resolves to http://localhost:4045
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
  mode: window     # Each worktree gets a window in your current session (default)
```

**Window mode** (default) keeps all worktrees as windows in one tmux session. Environment variables are injected per-pane via tmux's `-e` flag, so they don't leak between windows.

**Session mode** gives each worktree a fully isolated tmux session with its own environment.

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
    port: 3000
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
    port: 4000
    env: PORT
  web:
    port: 3000
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
    port: 8080
    env: GATEWAY_PORT
  users:
    port: 8081
    env: USERS_PORT
  billing:
    port: 8082
    env: BILLING_PORT
  frontend:
    port: 3000
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

## Requirements

- Git
- tmux 3.2+ (for workspace features — per-pane env injection via `-e`)
- Go 1.21+ (to build from source)
- `gh` CLI (optional — PR safety checks on `grove delete`)

## License

MIT
