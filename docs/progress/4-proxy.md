# Progress: #4 Built-in HTTPS Reverse Proxy

Issue: https://github.com/lukemelnik/grove/issues/4
Started: 2026-03-23
Last Updated: 2026-03-23

## Status

Current Sprint: Sprint 1

## Completed Work

### Sprint 1: TLS Certificate Engine & Trust
Completed: 2026-03-23

- Task 1: Certificate generation library - DONE
  - Created `internal/certs/` package with pure Go TLS cert generation (ECDSA P-256 + x509), CA management, per-hostname SNI callback with memory+disk caching, expiry detection (7-day renewal window), concurrent request deduplication, and CA cascade clearing. 14 tests covering generation, caching, expiry, concurrency, permissions.
  - Files: `internal/certs/certs.go`, `internal/certs/certs_test.go`

- Task 2: `grove trust` command - DONE
  - Added `grove trust` command for macOS keychain CA management with `--check` (exit 0/1), `--remove`, JSON output support, missing CA detection, and platform gating. 10 tests with injectable command executors.
  - Files: `internal/cmd/trust.go`, `internal/cmd/trust_test.go`, `internal/cmd/root.go`

## Implementation Notes

- `DefaultStateDir` is a `var` (function variable) rather than a plain function, allowing test overrides. Same pattern as `getWorkingDir` and `tmuxRunnerFactory` in the cmd package.
- Trust command executors (`trustAddCA`, `trustRemoveCA`, `trustCheckCA`) are injectable vars for testability, matching the `tmuxRunnerFactory` pattern.
- The `clearHostCerts` method is only called during initialization (in `ensureCA`), before any concurrent access to the Manager. No mutex needed for the cache reset at that point.
- `security add-trusted-cert -d -r trustRoot` is used to add to admin cert store (requires macOS auth dialog), matching portless's approach.

## Blockers

None.
