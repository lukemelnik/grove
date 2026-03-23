# Progress: #4 Built-in HTTPS Reverse Proxy

Issue: https://github.com/lukemelnik/grove/issues/4
Started: 2026-03-23
Last Updated: 2026-03-23

## Status

Current Sprint: Sprint 3

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

### Sprint 3: Daemon Management & Grove Integration
Completed: 2026-03-23

- Task 6: `grove proxy` commands - DONE
  - Created `internal/cmd/proxy.go` with `start` (foreground + `--daemon`/`-d` detach mode), `stop` (SIGTERM via PID file), `status` (running state, port, TLS, projects, routes), `projects` (list registered), `unregister` (by name or cwd), and `clean` (stop + remove all state). PID/port file management, file watcher polling every 2s for route rebuilds on registry/config/worktree changes. JSON output for all subcommands.
  - Files: `internal/cmd/proxy.go`, `internal/cmd/proxy_test.go`, `internal/cmd/root.go`

- Task 7: Config section & validation - DONE
  - Added proxy validation to `config.Validate()`: port range 1-65535, DNS-safe name validation via `isValidDNSLabel()`. Template validation: `{{service.url}}` and `{{service.host}}` produce clear errors when proxy config is absent. Updated `grove schema` with proxy section documentation. Added `--proxy`, `--proxy-name`, `--proxy-port` flags to `grove init`.
  - Files: `internal/config/config.go`, `internal/config/config_test.go`, `internal/cmd/schema.go`, `internal/cmd/init.go`, `internal/cmd/init_test.go`

- Task 8: Auto-setup and implicit project registration in `grove create` - DONE
  - Added `proxySetup()` function to `grove create` that runs after env file setup: generates CA if missing, prompts to trust CA interactively (macOS only), auto-starts proxy daemon if not running, registers project in registry, prints proxy URLs. All steps are non-fatal — worktree creation always succeeds. Idempotent registration.
  - Files: `internal/cmd/create.go`

- Task 9: Integration with list, status, and env templates - DONE
  - Added `ProxyInfo` struct to env package with `BuildProxyURL()` and `BuildProxyHost()` methods. Extended `ResolveTemplates()` to handle `{{service.url}}` and `{{service.host}}` templates. Updated `grove list` with Proxy column and `proxy_urls` JSON field. Updated `grove status` with Proxy section and `proxy_urls` JSON field. Port omitted from URL when it's the protocol default (443 for HTTPS, 80 for HTTP).
  - Files: `internal/env/env.go`, `internal/env/env_test.go`, `internal/cmd/list.go`, `internal/cmd/list_test.go`, `internal/cmd/status.go`, `internal/cmd/status_test.go`, `internal/cmd/attach.go`

- Task 10: README & attribution - DONE
  - Added comprehensive Proxy section to README covering: what it does, hostname scheme, configuration (simple + override forms), setup flow, manual commands, env templates, multi-project support. Added attribution to vercel-labs/portless (Apache 2.0). Added proxy config fields to Config Reference table. Updated Full Example with `proxy: true`.
  - Files: `README.md`

## Implementation Notes

- `DefaultStateDir` is a `var` (function variable) rather than a plain function, allowing test overrides. Same pattern as `getWorkingDir` and `tmuxRunnerFactory` in the cmd package.
- Trust command executors (`trustAddCA`, `trustRemoveCA`, `trustCheckCA`) are injectable vars for testability, matching the `tmuxRunnerFactory` pattern.
- The `clearHostCerts` method is only called during initialization (in `ensureCA`), before any concurrent access to the Manager. No mutex needed for the cache reset at that point.
- `security add-trusted-cert -d -r trustRoot` is used to add to admin cert store (requires macOS auth dialog), matching portless's approach.
- `ProxyConfig` uses a private `disabled` field to distinguish `proxy: false` from absent; `normalizeProxy()` nils out the pointer after YAML parsing so consumers see nil for disabled.
- `RouteTable` uses `sync.RWMutex` for concurrent route lookups during request handling and atomic full updates during route rebuilds.
- Added `golang.org/x/net` dependency for `http2.ConfigureServer` and `websocket` (test only).
- `ResolveTemplates`, `BuildManagedEnv`, and `Resolve` gained a `*ProxyInfo` parameter. Nil means no proxy — existing callers pass nil to preserve behavior.
- File watcher uses simple mtime polling (2s interval) rather than fsnotify to avoid adding a dependency. Watches `projects.json`, each project's `.grove.yml`, and worktree directories.
- Daemon mode uses `os/exec` + `syscall.Setpgid` to detach the proxy process from the parent terminal.
- `findExecutable` is a var for testability, same pattern as other injectable functions.
- `proxySetup()` in create.go reads stdin directly for the trust prompt since it runs outside the cobra flag parsing flow.

## Blockers

None.
