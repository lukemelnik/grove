# Progress: #4 Built-in HTTPS Reverse Proxy

Issue: https://github.com/lukemelnik/grove/issues/4
Started: 2026-03-23
Last Updated: 2026-03-23

## Status

Current Sprint: Sprint 2

## Completed Work

### Sprint 1: TLS Certificate Engine & Trust
Completed: 2026-03-23

- Task 1: Certificate generation library - DONE
  - Created `internal/certs/` package with pure Go TLS cert generation (ECDSA P-256 + x509), CA management, per-hostname SNI callback with memory+disk caching, expiry detection (7-day renewal window), concurrent request deduplication, and CA cascade clearing. 14 tests covering generation, caching, expiry, concurrency, permissions.
  - Files: `internal/certs/certs.go`, `internal/certs/certs_test.go`

- Task 2: `grove trust` command - DONE
  - Added `grove trust` command for macOS keychain CA management with `--check` (exit 0/1), `--remove`, JSON output support, missing CA detection, and platform gating. 10 tests with injectable command executors.
  - Files: `internal/cmd/trust.go`, `internal/cmd/trust_test.go`, `internal/cmd/root.go`

### Sprint 2: Reverse Proxy & Route Computation
Completed: 2026-03-23

- Task 3: Hostname sanitization - DONE
  - Created `SanitizeBranch()` in `internal/proxy/hostname.go` converting branch names to DNS-safe labels (lowercase, hyphens for non-alphanumeric, collapse consecutive hyphens, trim, 63-char truncation with SHA-256 hash suffix). Also `BuildHostname()` for full `<service>.<branch>.<project>.localhost` construction with default-branch shortening. 17 tests.
  - Files: `internal/proxy/hostname.go`, `internal/proxy/hostname_test.go`

- Task 4: Project registry and route computation - DONE
  - Created `internal/proxy/registry.go` with `RegisterProject`, `UnregisterProject`, `UnregisterByName`, `LoadAndPrune` (auto-pruning stale entries), name-collision detection, and thread-safe JSON persistence. Created `internal/proxy/routes.go` with `ComputeAllRoutes()` iterating registered projects, loading configs, enumerating worktrees, and calling `ports.Assign()` for the combined route table. Added `ProxyConfig` struct with boolean+object `UnmarshalYAML` to `internal/config/config.go`. 18 tests covering registration, dedup, collision, pruning, route computation, route table operations.
  - Files: `internal/proxy/registry.go`, `internal/proxy/registry_test.go`, `internal/proxy/routes.go`, `internal/proxy/routes_test.go`, `internal/config/config.go`, `internal/config/config_test.go`

- Task 5: HTTP/HTTPS reverse proxy - DONE
  - Created `internal/proxy/server.go` with `httputil.ReverseProxy` for HTTP/HTTPS routing by Host header, TLS termination via SNI callback from Sprint 1, HTTP/2 via `golang.org/x/net/http2`, WebSocket upgrade via hijack+bidirectional TCP pipe, X-Forwarded-{For,Proto,Host} headers, styled 404 (route listing) and 502 (service details) error pages, graceful shutdown. 9 integration tests covering HTTP routing, forwarded headers, 404, 502, HTTPS, WebSocket, and shutdown.
  - Files: `internal/proxy/server.go`, `internal/proxy/server_test.go`

## Implementation Notes

- `DefaultStateDir` is a `var` (function variable) rather than a plain function, allowing test overrides. Same pattern as `getWorkingDir` and `tmuxRunnerFactory` in the cmd package.
- Trust command executors (`trustAddCA`, `trustRemoveCA`, `trustCheckCA`) are injectable vars for testability, matching the `tmuxRunnerFactory` pattern.
- The `clearHostCerts` method is only called during initialization (in `ensureCA`), before any concurrent access to the Manager. No mutex needed for the cache reset at that point.
- `security add-trusted-cert -d -r trustRoot` is used to add to admin cert store (requires macOS auth dialog), matching portless's approach.
- `ProxyConfig` uses a private `disabled` field to distinguish `proxy: false` from absent; `normalizeProxy()` nils out the pointer after YAML parsing so consumers see nil for disabled.
- `RouteTable` uses `sync.RWMutex` for concurrent route lookups during request handling and atomic full updates during route rebuilds.
- Added `golang.org/x/net` dependency for `http2.ConfigureServer` and `websocket` (test only).

## Blockers

None.
