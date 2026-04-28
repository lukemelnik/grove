# Grove current-pane + reopen workflow

## Context

**What:** Add a cleaner separation between creating a worktree, opening the full tmux setup, entering a worktree in the current pane, and interactively picking existing worktrees.

**Why:** Sometimes the user wants Grove’s deterministic worktree/ports/env setup without immediately creating or switching tmux windows. Other times they want to recover or reopen a closed Grove tmux workspace.

**Key Decisions:**
- Add `grove create <branch> --no-open` as the preferred “provision only” flag.
- Keep `--no-tmux` working as a backwards-compatible alias.
- Add `grove open <branch>` for the canonical full tmux setup.
- Add `grove enter <branch>` for current-pane/subshell workflow.
- Keep `grove attach <branch>` as a backwards-compatible alias/path to `open`.
- Use tmux user-option labels like `@grove.branch`, `@grove.project_root`, `@grove.worktree_path`, and `@grove.role` instead of relying on window names.
- In interactive `grove list`, support fzf actions:
  - `Enter`: canonical `grove open <branch>`
  - `Ctrl-P`: `grove enter <branch>`
  - `Ctrl-W`: force a new tmux window
  - `Ctrl-D`: delete, with confirmation
- Agents should be documented to use non-interactive commands and JSON, not the fzf picker.

**Relevant Files:**
- `internal/cmd/create.go` / `internal/cmd/create_test.go` — add `--no-open`, preserve `--no-tmux`.
- `internal/cmd/attach.go` / `internal/cmd/attach_test.go` — likely refactor into reusable open flow.
- `internal/cmd/open.go` / `internal/cmd/open_test.go` — new canonical tmux open/reopen behavior.
- `internal/cmd/enter.go` / `internal/cmd/enter_test.go` — new current-pane subshell behavior.
- `internal/cmd/list.go` / `internal/cmd/list_test.go` — optional fzf picker and key bindings.
- `internal/tmux/tmux.go` / `internal/tmux/tmux_test.go` — label Grove-created tmux targets and find them by labels.
- `README.md`, command help, and `internal/cmd/help_test.go` — clear human and agent instructions.

**Constraints:**
- Preserve existing uncommitted user changes in `internal/cmd/help_test.go`, `internal/cmd/init.go`, and `internal/cmd/root.go`.
- Do not rely on tmux window names for canonical lookup; users may rename windows.
- Do not automatically adopt arbitrary manually-created tmux windows unless an explicit future adoption flow is added.
- Do not expose resolved secret-like environment values in JSON output.
- Interactive `grove list` behavior must not break agent/script usage of `grove list --json` or piped auto-JSON.

## Scope

**In:**
- `grove create <branch> --no-open`
- `grove open <branch>`
- `grove open <branch> --new-window`
- `grove enter <branch>`
- interactive `grove list` picker with fzf key bindings
- tmux labels for Grove-managed sessions/windows
- docs/help for humans and agents
- tests for command behavior, tmux labeling, picker actions, and subshell launch

**Out:**
- True parent-shell `cd` integration/function for now.
- Persisted metadata database/file unless tmux labels prove insufficient.
- Automatically adopting arbitrary manually-created tmux windows.
- Force-delete from picker; destructive picker action should still respect normal delete safety checks.

## Tasks

### Task 1: Add provision-only creation

Add `--no-open` as the user-facing way to create/reuse a worktree without opening tmux.

**Done when:**
- [x] `grove create <branch> --no-open` creates/reuses the worktree, assigns ports, writes env files, runs hooks, and does not call tmux.
- [x] `--no-tmux` still works as a backwards-compatible alias.
- [x] Human output tells users how to proceed: `grove open <branch>` or `grove enter <branch>`.
- [x] JSON output remains machine-safe and does not expose env values.
- [x] Tests cover `--no-open`, `--no-tmux`, JSON, hooks/env still running, and no tmux calls.

### Task 2: Add canonical `grove open`

Add `grove open <branch>` as the main command for opening or restoring Grove’s full tmux setup.

**Done when:**
- [x] `grove open <branch>` finds the worktree or errors with a clear `grove create` suggestion.
- [x] If the Grove-labeled canonical tmux target exists, Grove switches/attaches to it even if the window was renamed.
- [x] If the canonical tmux target is gone, Grove recreates the full tmux layout.
- [x] Existing `grove attach <branch>` continues to work, ideally routed through the same open logic.
- [x] Legacy unlabeled Grove windows/sessions created by older versions are handled by existing name fallback where practical.
- [x] Tests cover existing target, missing target recreation, renamed/labeled window, no worktree, and invalid env failing before tmux.

### Task 3: Label Grove-created tmux targets

Label tmux sessions/windows created by Grove so lookup is stable even when names change.

**Done when:**
- [x] Newly-created tmux windows/sessions get labels for project root, branch, worktree path, and role.
- [x] Grove lookup uses labels, not window names, for canonical reopen.
- [x] `grove open --new-window <branch>` creates an additional full tmux window even if canonical exists.
- [x] If no canonical exists, the forced new window may become canonical; if canonical exists, mark the new one as extra.
- [x] Delete/clean kills Grove-labeled targets for the worktree, with legacy fallback by old name.
- [x] Tests cover label setting, lookup disambiguation by project root, extra windows, and delete cleanup.

### Task 4: Add `grove enter`

Add a current-pane workflow that starts an interactive subshell in the selected worktree.

**Done when:**
- [x] `grove enter <branch>` launches the user’s shell in the worktree cwd.
- [x] It exports Grove-managed env vars plus clear markers like `GROVE_BRANCH`, `GROVE_WORKTREE`, and `GROVE_PROJECT_ROOT`.
- [x] It prints a short message like “Entering Grove worktree … type exit/Ctrl-D to return.”
- [x] It does not call tmux or alter tmux layout.
- [x] Tests use an injectable shell launcher so cwd/env can be verified without spawning an interactive shell.

### Task 5: Add interactive list picker

Enhance terminal `grove list` with fzf actions while keeping JSON behavior stable.

**Done when:**
- [x] `grove list --json` and non-TTY output stay agent/script friendly.
- [x] Interactive `grove list` uses fzf when available.
- [x] fzf key bindings map to:
  - [x] `Enter` → `open`
  - [x] `Ctrl-P` → `enter`
  - [x] `Ctrl-W` → open in new tmux window
  - [x] `Ctrl-D` → confirm then delete
- [x] Missing fzf falls back gracefully to plain list output.
- [x] Tests mock picker output and verify each action dispatches correctly.

### Task 6: Add agent-facing documentation

Update README and command help so agents know when to use each command.

**Done when:**
- [x] Agents are told to use `grove create <branch> --no-open --json` to provision without stealing tmux focus.
- [x] Agents are told to use `grove list --json` to discover existing worktrees.
- [x] Agents are told to use the returned `worktree` path as their working directory.
- [x] Agents are told to use `grove open <branch>` only when the user asks to open/restore the full tmux UI.
- [x] Agents are told to avoid `grove enter` unless explicitly asked because it starts an interactive shell.
- [x] Agents are told to avoid interactive `grove list`/fzf; use `--json`.
- [x] Agents are told not to pass `--force` to delete/clean commands without explicit user approval.

## Acceptance Criteria

- [x] `go test ./...` passes.
- [x] Targeted tests exist for `create`, `open`, `enter`, `list`, `delete/clean`, and `tmux` behavior touched by this feature.
- [x] `grove create --help`, `grove open --help`, `grove enter --help`, and README explain the intended human and agent workflows.
- [x] Existing scripts using `grove create --no-tmux`, `grove attach`, and `grove list --json` continue to work.
- [x] Existing manually renamed Grove tmux windows are findable after they have Grove labels.
- [x] Closing the canonical tmux target and running `grove open <branch>` recreates the full layout.

## Changes During Implementation

- Tmux label path matching normalizes symlinks/absolute paths before comparing project roots and worktree paths, so labels remain stable across macOS `/var` vs `/private/var` path forms.
