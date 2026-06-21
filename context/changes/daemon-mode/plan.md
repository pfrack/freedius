# Daemon Mode with Foreground Attach — Implementation Plan

## Overview

Add daemon mode (headless proxy, no TUI) and foreground/attach capability to freedius. The proxy can run as a background process; users can attach the TUI dashboard to a running daemon via IPC. Platform-specific code uses Go build tags (`_unix.go` / `_windows.go`) — no runtime `runtime.GOOS` checks.

## Current State Analysis

The proxy and TUI are tightly coupled in `cmd/freedius/main.go:217-226` — `tea.Program.Run()` blocks the main goroutine, and TUI exit triggers `server.Shutdown()`. There is no signal handling in Go code (Bubble Tea handles it internally for the TUI path). The `EventBus` and `LogSink` use single shared channels with no replay capability.

### Key Discoveries:

- No signal handling exists in Go code — Bubble Tea handles SIGINT/SIGTERM internally for TUI path (`cmd/freedius/main.go:217-226`)
- TUI exit = process exit — `prog.Run()` blocks, then `server.Shutdown()` runs (`main.go:228-233`)
- EventBus/LogSink are single-channel, no replay — `Subscribe()` returns shared `<-chan` (`proxy/eventbus.go:72-78`, `proxy/logtee.go:38-43`)
- Bubble Tea v2 has native suspend/resume on Unix — `suspendSupported=true` on Unix, `false` on Windows (`tty_unix.go:37`)
- Logger writes to `io.Discard` — logs only visible in TUI ring buffer (`main.go:128`)
- `$XDG_RUNTIME_DIR` doesn't exist on macOS — fallback to `$TMPDIR` (per-user on macOS)
- Platform support: Linux + macOS in scope, Windows out of scope per roadmap (`context/foundation/roadmap.md:217`) — but build-tag structure allows future Windows support

## Desired End State

After this plan:
- `freedius` (no args) — unchanged: TUI + proxy, `Ctrl+Z` to suspend, `fg` to resume
- `freedius --fg` — headless proxy in foreground (Docker, systemd, scripts)
- `freedius --daemon` / `freedius -d` — proxy forks to background, writes PID file
- `freedius attach` — TUI connects to running daemon via Unix socket, sees live events/logs
- `freedius stop` — sends SIGTERM to daemon via PID file
- `freedius status` — checks if daemon is running

### Verify with:

1. `freedius` starts TUI, `Ctrl+Z` suspends, `fg` resumes with state intact
2. `freedius --fg` runs proxy without TUI, `Ctrl+C` shuts down gracefully
3. `freedius --daemon` forks to background, `freedius status` shows running
4. `freedius attach` connects to daemon, shows live logs, `q` detaches without killing daemon
5. `freedius stop` terminates daemon cleanly

## What We're NOT Doing

- Windows support (parked per roadmap — build-tag structure allows adding later)
- SIGHUP config reload (non-trivial, defer to future iteration)
- Multiple simultaneous TUI clients
- Config mutation from attached TUI (read-only in v1 of attach)
- Web UI / HTTP dashboard endpoint
- Log file rotation (`--log-file` flag deferred — stderr is sufficient)

## Implementation Approach

Four phases, each independently testable:

1. **Ctrl+Z** — 2-line Bubble Tea integration, zero risk
2. **`--fg`** — headless mode with platform-specific signal handling via build tags
3. **`--daemon`** — fork/re-exec with PID file, platform-specific via build tags
4. **IPC attach** — Unix socket server with SSE streaming, event replay, `freedius attach` command

Platform-specific code lives in `cmd/freedius/signal_unix.go`, `cmd/freedius/daemon_unix.go`, `cmd/freedius/daemon_windows.go`, `cmd/freedius/ipc_unix.go` etc. with matching `//go:build` constraints. Shared logic stays in `main.go` and calls exported functions from platform files.

## Critical Implementation Details

- **Bubble Tea suspend**: On Unix, Bubble Tea's `suspend()` calls `releaseTerminal(true)` → sends `SIGTSTP` → blocks on `SIGCONT` → `RestoreTerminal()`. The `Dashboard` model survives in-process. On Windows, `suspendSupported=false` — `ctrl+z` handler should be a no-op or show "suspend not supported on Windows".
- **Event replay gap**: While TUI is suspended or detached, events queue in buffered channels (1000 cap). If daemon produces >1000 events during detach, older events are dropped. The IPC replay ring buffer (Phase 4) uses a separate 10000-entry ring to survive longer detach periods.
- **PID file race**: Two `freedius --daemon` invocations could race. The amended contract (Phase 3 §4) closes this via `syscall.Flock` on a sidecar `freedius.lock` file before the probe + `syscall.Kill(pid, 0)` liveness check + `<pid>\t<start_time_unix_nano>` file format (start time detects PID reuse after fast crash).
- **Socket cleanup**: On daemon crash, the Unix socket file may be stale. The amended contract (Phase 4 §6) drives cleanup via `IPCServer.Shutdown` which removes `<runtimeDir>/freedius.sock`; the cleanup arg is wired through `waitForShutdown(server, ipcServer.Shutdown)` so SIGTERM-driven shutdown always removes the socket. On startup, the IPCServer attempts `net.Dial(socketPath)` — if fails, remove the stale socket and re-listen.

---

## Phase 1: Ctrl+Z Suspend/Resume

### Overview

Add `ctrl+z` keybinding to suspend the TUI while the proxy keeps running. Bubble Tea v2 handles all the terminal teardown/restore natively. On resume, the TUI receives queued events from the buffered channels.

### Changes Required:

#### 1. TUI keybinding

**File**: `proxy/tui/model.go`

**Intent**: Add `ctrl+z` handler in `handleTabModeKeyPress` (line 275) to return `tea.Suspend` command. Add `tea.ResumeMsg` handler in `Update` to clear status message on resume.

**Contract**: `case "ctrl+z": return d, tea.Suspend` in the switch at line 275. In `Update`, add `case tea.ResumeMsg:` that clears `d.stats.message`.

#### 2. Platform note

**File**: `proxy/tui/model.go`

**Intent**: No build tags needed — Bubble Tea's `suspendSupported` is already platform-gated internally. On Windows, `tea.Suspend` is silently ignored by Bubble Tea's event loop. The `ctrl+z` handler can remain in shared code.

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go test ./proxy/tui/...` passes
- `go build ./cmd/freedius` succeeds on linux/amd64 and darwin/amd64

#### Manual Verification:

- Start `freedius`, press `Ctrl+Z` — TUI disappears, shell prompt returns
- Proxy continues serving requests (verify with `curl http://127.0.0.1:8082/health`)
- Run `fg` — TUI resumes with full state (active tab, scroll position, log buffer)
- Events accumulated during suspension appear on resume

---

## Phase 2: `--fg` Headless Mode

### Overview

Add `--fg` flag to run the proxy in foreground without the TUI. Enables Docker, systemd, scripts, and CI usage. Platform-specific signal handling via build tags.

### Changes Required:

#### 1. Flag definition

**File**: `cmd/freedius/main.go`

**Intent**: Add `--fg` boolean flag to the `flag.FlagSet` at line 86. Add to `printUsage()` at line 244.

**Contract**: `flagFg := fs.Bool("fg", false, "run headless in foreground (no TUI, for Docker/scripts)")`

#### 2. Branch in run()

**File**: `cmd/freedius/main.go`

**Intent**: After server startup and `waitForBind` (line 213), branch on `fg`. When true, skip TUI creation entirely. Instead, call `waitForShutdown(server, ipcServer.Shutdown)` which blocks on signals. Pass `nil` as the cleanup arg when no IPCServer is running (in-process TUI mode without IPC). When false, proceed with existing TUI path.

**Contract**: The `waitForShutdown` function is platform-specific (see below). It blocks until a shutdown signal is received, then calls `server.Shutdown()` followed by the cleanup arg (which removes the IPC socket in daemon mode — see Phase 4 §6).

#### 3. Platform-specific signal handling — Unix

**File**: `cmd/freedius/signal_unix.go` (new)

**Intent**: Implement `waitForShutdown(server *http.Server, cleanup func() error) error` for Unix. Uses `signal.NotifyContext` with `os.Interrupt, syscall.SIGTERM` (drop `syscall.SIGINT` — `os.Interrupt` is the portable alias for SIGINT on every supported OS, so the third entry is redundant). The function:
1. `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`
2. `defer stop()` — required to restore default signal handling on exit.
3. `<-ctx.Done()` — blocks until a signal arrives.
4. `shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout); defer cancel()`.
5. `server.Shutdown(shutdownCtx)`.
6. If `cleanup != nil`, call `cleanup()` — this is the IPC server's `Shutdown` which removes the Unix socket file. Always called so a graceful stop also removes the socket.

This contract guarantees that `freedius stop` (which sends SIGTERM) triggers both the proxy shutdown AND the socket cleanup.

**Contract**:

```go
//go:build !windows

package main

func waitForShutdown(server *http.Server) error
```

#### 4. Platform-specific signal handling — Windows

**File**: `cmd/freedius/signal_windows.go` (new)

**Intent**: Implement `waitForShutdown` for Windows. Uses `signal.NotifyContext` with `os.Interrupt` only (SIGTERM/SIGHUP don't exist on Windows). **The Windows signature MUST match the Unix signature** (per review F2): the second arg is a `cleanup func() error` that the Unix path uses for socket removal; on Windows, IPCServer is not built (no Unix socket), so the cleanup arg is always nil at the call site — discard it via `_`.

**Contract**:

```go
//go:build windows

package main

func waitForShutdown(server *http.Server, _ func() error) error
```

#### 5. Log output for headless mode

**File**: `cmd/freedius/main.go`

**Intent**: When `--fg` is active, pass `os.Stderr` instead of `io.Discard` to `newLogger()` at line 128. This makes logs visible in headless mode (captured by systemd/Docker/nohup).

**Contract**: Conditional: `logWriter := io.Discard; if fg { logWriter = os.Stderr }`. Pass `logWriter` to `newLogger()`.

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go test ./cmd/freedius/...` passes
- `go build ./cmd/freedius` succeeds
- `./freedius --fg --port 0 &` starts and `/health` returns 200

#### Manual Verification:

- `freedius --fg` starts proxy, logs appear on stderr, no TUI
- `Ctrl+C` shuts down gracefully (5s drain, clean exit)
- `freedius --fg --port 9090` respects port override
- `freedius --fg --log-format json` outputs structured JSON to stderr

---

## Phase 3: `--daemon` Background Mode

### Overview

Add `--daemon`/`-d` flags to fork the proxy to background. Re-exec self with `--fg`, write PID file to `$XDG_RUNTIME_DIR`. Add `freedius stop` and `freedius status` subcommands.

### Changes Required:

#### 1. Flag definitions

**File**: `cmd/freedius/main.go`

**Intent**: Add `--daemon` and `-d` boolean flags. Add mutual exclusion check: `--daemon` + `--fg` = exit 2 with error message.

**Contract**: `flagDaemon := fs.Bool("daemon", false, ...)`, `flagDaemonShorthand := fs.Bool("d", false, ...)`. After parse: `if daemon && fg { return failf("freedius: --daemon and --fg are mutually exclusive") }`.

#### 2. Platform-specific daemonization — Unix

**File**: `cmd/freedius/daemon_unix.go` (new)

**Intent**: Implement `startDaemon(args []string) error` for Unix. Re-exec self with `--fg` appended to args. **Resolve the re-exec target via `os.Executable()` (NOT `os.Args[0]` — `os.Args[0]` is unreliable under `go run`, `go install`, and Homebrew; the error-hardening research at context/archive/error-hardening/research.md:287 explicitly rejected it).** Refuse to start under `go run`: if `os.Executable()` returns a path ending in `/go-build<hex>/exe/...` or any path under `os.TempDir()` that doesn't match the binary name, exit with: `freedius: --daemon requires a built binary; run 'go build -o freedius ./cmd/freedius' first`. Use `exec.Command` with `SysProcAttr.Setsid = true` to detach from terminal. Inherit env via the default (`exec.Command` propagates `os.Environ()` — do NOT set `cmd.Env`). Redirect stdout/stderr to `/dev/null`. Write PID file (PID + start_time, per F2). Print "daemon started (PID N)" to stderr.

**Contract**:

```go
//go:build !windows

package main

func startDaemon(args []string) error
func stopDaemon() error
func daemonStatus() (running bool, pid int, err error)
```

**`stopDaemon` contract**: (1) Read PID file via `readPIDFile()` — returns `(int, int64, error)` for `(pid, startTime, err)`. (2) If PID file not found, return `fmt.Errorf("freedius: not running (no PID file at %s)", pidFilePath)`. (3) Send `syscall.SIGTERM` via `syscall.Kill(pid, syscall.SIGTERM)`. (4) Poll process exit: try `syscall.Kill(pid, 0)` every 50ms, up to 200ms. (5) If process exits within 200ms, return nil (PID file cleanup is handled by the daemon's signal handler, which calls `waitForShutdown` → `removePIDFile()` — per Phase 3 §4 / Phase 4 §6). (6) If timeout: return `fmt.Errorf("freedius: daemon (PID %d) did not exit within 200ms", pid)` — the user may force-kill with `kill -9`.

**`daemonStatus` contract**: (1) Read PID file via `readPIDFile()`. (2) If not found, return `(false, 0, nil)` — not an error, just "not running". (3) Attempt `syscall.Kill(pid, 0)`. (4) If `err == nil` (process exists, we have permission) or `errors.Is(err, syscall.EPERM)` (process exists, we lack permission to signal but it's alive): return `(true, pid, nil)`. (5) If `errors.Is(err, syscall.ESRCH)` (no such process): return `(false, pid, nil)` — stale PID file; the caller (handleStatus) may clean it up. (6) Other errors: return `(false, 0, err)`.

**`handleStop()` and `handleStatus()` subcommand routing** (Phase 3 §5): both call the corresponding daemon_*.go function and print the result. `handleStop` prints either "freedius: daemon stopped" (on nil) or the error. `handleStatus` prints "freedius: running (PID N)" on running=true, or "freedius: not running" on running=false.

#### 3. Platform-specific daemonization — Windows

**File**: `cmd/freedius/daemon_windows.go` (new)

**Intent**: Stub implementation for Windows. `startDaemon` returns an error: "daemon mode not supported on Windows, use --fg with a service manager". `stopDaemon` and `daemonStatus` similarly stub.

**Contract**:

```go
//go:build windows

package main

func startDaemon(args []string) error  // returns error
func stopDaemon() error                // returns error
func daemonStatus() (running bool, pid int, err error) // returns error
```

#### 4. PID file management — Unix

**File**: `cmd/freedius/daemon_unix.go`

**Intent**: Implement `pidFilePath() string` returning `$XDG_RUNTIME_DIR/freedius.pid` with fallback to `os.TempDir()/freedius.pid` (use `runtimeDir()` shared helper from F11). Implement `writePIDFile(pid int) error`, `readPIDFile() (int, error)`, `removePIDFile() error`, `isProcessAlive(pid int) bool`.

**Contract**: PID file contains `<pid>\t<start_time_unix_nano>\n` (tab-separated; start_time detects PID reuse). Liveness check uses `syscall.Kill(pid, 0)` directly (NOT `os.FindProcess().Signal()` — the latter is lazy on macOS and yields false positives); accept `EPERM` as alive (process exists, no perm), reject `ESRCH` as dead. Race protection via `syscall.Flock(lockfile_fd, LOCK_EX|LOCK_NB)` on a sidecar `<runtimeDir>/freedius.lock` file: acquire before probe, release in `defer`. On Linux additionally `os.Stat(fmt.Sprintf("/proc/%d", pid))` to guard against PID recycling.

#### 5. Subcommand dispatch

**File**: `cmd/freedius/main.go`

**Intent**: After the existing `--help`/`--version` scan (lines 75-84) but before `fs.Parse(args)`, check if the first arg is `stop`, `status`, or `attach`. If so, dispatch to the corresponding function and exit. This avoids needing a full subcommand framework. Placing the dispatch AFTER the help scan ensures `freedius stop --help` still shows usage (the `--help` arg is caught by the scan at lines 75-84 before dispatch runs). Placing it BEFORE `fs.Parse` ensures subcommand args aren't fed to the flag parser.

**Contract**: In `run()`, after the `--help`/`--version` loop (line 84) and before `fs := flag.NewFlagSet(...)` (line 86):

```go
if len(args) > 0 {
    switch args[0] {
    case "stop": return handleStop()
    case "status": return handleStatus()
    case "attach": return handleAttach(args[1:])
    }
}
```

#### 6. Usage update

**File**: `cmd/freedius/main.go`

**Intent**: Update `printUsage()` to document `--daemon`, `-d`, and subcommands (`stop`, `status`).

#### 7. Shared `runtimeDir()` helper (per review F1)

**File**: `cmd/freedius/paths_unix.go` (new) + `cmd/freedius/paths_windows.go` (new)

**Intent**: Single source of truth for `$XDG_RUNTIME_DIR` vs `os.TempDir()` resolution. Used by both PID file path (§3.4) and socket path (§4.5) so they cannot diverge.

**Contract**:

```go
//go:build !windows

package main

import "os"

// runtimeDir returns the directory for daemon state files (PID,
// socket). On Linux, $XDG_RUNTIME_DIR is set by systemd-logind
// and is per-user + tmpfs (auto-cleaned on reboot). On macOS,
// $XDG_RUNTIME_DIR is not standard, so fall back to os.TempDir()
// which respects $TMPDIR (per-user on macOS).
func runtimeDir() string {
    if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
        return d
    }
    return os.TempDir()
}
```

```go
//go:build windows

package main

import "os"

func runtimeDir() string {
    return os.TempDir()
}
```

Both PID path (`pidFilePath() = filepath.Join(runtimeDir(), "freedius.pid")`) and socket path (`socketPath() = filepath.Join(runtimeDir(), "freedius.sock")`) MUST call this helper.

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go test ./cmd/freedius/...` passes
- `go build ./cmd/freedius` succeeds
- `./freedius --daemon --port 0` forks and returns immediately

#### Manual Verification:

- `freedius --daemon` prints "daemon started (PID N)"
- `freedius status` shows "running (PID N)"
- `curl http://127.0.0.1:8082/health` returns 200
- `freedius stop` sends SIGTERM, daemon exits cleanly
- `freedius status` shows "not running" after stop
- `freedius --daemon` when already running: exits with "already running" error
- PID file at `$XDG_RUNTIME_DIR/freedius.pid` is created on start, removed on stop
- Stale PID file (process dead) is detected and cleaned up

---

## Phase 4: IPC-Based TUI Attach

### Overview

Add a Unix socket IPC server to the daemon that streams events and logs via SSE. Add `freedius attach` command that connects to the socket and runs the TUI. Event replay on late attach via sequence numbers.

### Changes Required:

#### 1. Event replay — EventBus

**File**: `proxy/eventbus.go`

**Intent**: Add a ring buffer alongside the existing channel. Every `Emit()` stores the event in the ring with a monotonically increasing sequence number. Add `Since(seq int64) ([]RequestEvent, int64, bool)` method that returns buffered events with `Seq >= seq` plus the current high-water mark and an `evicted` flag.

**Contract**: Add fields to `EventBus`: `ring []RequestEvent`, `ringMu sync.RWMutex`, `ringSize int`, `seq atomic.Int64`. `Since` returns `(events []RequestEvent, currentSeq int64, evicted bool)`. `evicted == true` means the oldest buffered event has `Seq > seq` (i.e. requested seq is below the ring's low-water mark and partial history was lost). Edge cases:
- `seq <= 0` (initial attach): return entire ring, evicted=false.
- `seq > currentSeq` (client ahead of server): return `nil, currentSeq, false` (nothing to replay yet).
- `seq == currentSeq`: return `nil, currentSeq, false` (caught up, switch to live).
- `seq < oldest_in_ring`: return what's left, evicted=true.

The SSE endpoint emits `event: replay\ndata: {"complete": false, ...}` whenever evicted=true so the attached TUI can show "showing recent events, earlier history unavailable". Ring buffer capacity: 10000.

#### 2. Event replay — LogSink

**File**: `proxy/logtee.go`

**Intent**: Same ring-buffer pattern as EventBus (F4 contract): add `ring []LogEntry`, `ringMu sync.RWMutex`, `ringSize int`, `seq atomic.Int64` fields. Mirror `Since(seq) (entries []LogEntry, currentSeq int64, evicted bool)`. **Do NOT change `Snapshot()` semantics** — it remains destructive (drains the channel) because logtee_test.go:45,75,101,131 assert Snapshot overflow behavior; the TUI Log tab reads from its own `d.logBuffer` (model.go:560), not from `sink.Snapshot()`. Add `SnapshotSince(seq int64) (entries []LogEntry, currentSeq int64, evicted bool)` for the IPC replay path that reads from the ring buffer copy (non-destructive). **Edge cases** (mirror the F6 EventBus Since contract):
- `seq <= 0` (initial attach): return entire ring, evicted=false.
- `seq > currentSeq`: return `nil, currentSeq, false`.
- `seq == currentSeq`: return `nil, currentSeq, false`.
- `seq < oldest_in_ring`: return what's left, evicted=true.

#### 3. IPC server — Unix

**File**: `cmd/freedius/ipc_unix.go` (new)

**Intent**: Implement Unix socket HTTP server. Serves SSE endpoints for events and logs, plus `/v1/stats` and `/v1/config` JSON endpoints. Starts alongside the HTTP proxy server in daemon mode.

**Contract**:

```go
//go:build !windows

package main

type IPCServer struct { ... }

func NewIPCServer(socketPath string, bus *proxy.EventBus, logSink *proxy.LogSink, cfg *config.Config, registry *proxy.Registry) *IPCServer
func (s *IPCServer) ListenAndServe() error
func (s *IPCServer) Shutdown(ctx context.Context) error
```

`Shutdown(ctx)` MUST remove the Unix socket file at `socketPath` (use `defer os.Remove(socketPath)` inside the method body) and close the listener. The cleanup-on-shutdown contract is wired through Phase 2 §3's `waitForShutdown(server, cleanup func() error)` — see Phase 4 §6 for the call-site wiring.

Endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/v1/events?since=N` | GET | SSE stream of `RequestEvent` JSON. Replay buffered events first, then live. |
| `/v1/logs?since=N` | GET | SSE stream of `LogEntry` JSON. Same replay-then-live. |
| `/v1/stats` | GET | JSON: uptime, total requests, errors, port, host. |
| `/v1/config` | GET | Full config JSON snapshot. |

**SSE contract (lessons.md §1, §2)**: emission MUST use `json.Marshal` (NOT `json.NewEncoder`, which appends `\n` and corrupts the `data: ...\n\n` SSE framing). Reading the inbound `since=N` query parameter MUST use `net/http`'s `r.URL.Query()` (no SSE reader needed — that's the client side, per §4.9 below).

#### 4. IPC server — Windows stub

**File**: `cmd/freedius/ipc_windows.go` (new)

**Intent**: Stub `IPCServer` that returns errors. Unix sockets are not available on Windows; this would need named pipes or TCP in the future.

**Contract**:

```go
//go:build windows

package main

type IPCServer struct{}
func NewIPCServer(...) *IPCServer { return &IPCServer{} }
func (s *IPCServer) ListenAndServe() error { return fmt.Errorf("IPC not supported on Windows") }
func (s *IPCServer) Shutdown(ctx context.Context) error { return nil }
```

#### 5. Socket path and lifecycle

**File**: `cmd/freedius/ipc_unix.go`

**Intent**: Socket file at `$XDG_RUNTIME_DIR/freedius.sock` (fallback: use `runtimeDir()` from Phase 3 §7 and append `freedius.sock`). On startup, check for stale socket (try `net.Dial` — if fails, remove and re-listen). On shutdown, `defer os.Remove(socketPath)` (also driven by `IPCServer.Shutdown` — see §6). Socket permissions: `0600` (owner-only).

#### 6. Wire IPC server into daemon mode

**File**: `cmd/freedius/main.go`

**Intent**: When running in daemon/fg mode, create and start the `IPCServer` alongside the HTTP server. Store socket path in PID file (or a companion `.sock` path file) for `attach` command to discover.

**Contract**: IPC server goroutine starts after `waitForBind`. **Wire `ipcServer.Shutdown` as the `cleanup` arg to `waitForShutdown` (see Phase 2 §3).** On daemon child startup, the call site is:

```go
ipcServer := NewIPCServer(socketPath, bus, logSink, cfg, registry)
go func() { _ = ipcServer.ListenAndServe() }()
// ... waitForBind succeeds ...
waitForShutdown(server, ipcServer.Shutdown)
```

This guarantees the socket file is removed on graceful SIGTERM-driven shutdown (the daemon child runs `--fg`, traps SIGTERM via Phase 2 §3, calls `server.Shutdown`, then `ipcServer.Shutdown(ctx)` which removes the socket — all in sequence).

#### 7. TUI client for attach

**File**: `cmd/freedius/attach.go` (new)

**Intent**: Implement `runAttach(args []string) int` that reads the socket path, dials the daemon, builds an `IPCClient`, and runs the TUI on top of it.

**Contract**: Reuse the existing `Dashboard` struct — do NOT create a parallel `DashboardIPC` type. **Add a new constructor `tui.NewAttachDashboard(events, logs, cfgPath, host, port) *Dashboard` in proxy/tui/model.go** that accepts nil reg/dispatcher (the attach client has neither — it only observes via SSE). The existing `tui.NewDashboard` keeps its hard nil-check contract for the in-process TUI path; `NewAttachDashboard` is the attach-specific entry point. The IPCClient's `Events()` and `Logs()` methods return `<-chan proxy.RequestEvent` and `<-chan proxy.LogEntry` (driven by SSE), which match Dashboard's existing `events`/`logs` channel fields (model.go:80–81) exactly. `runAttach()` calls `tui.NewAttachDashboard(...)` with `detachOnQuit: true` (a new Dashboard field, default false in in-process TUI). `runAttach()` runs `tea.NewProgram(model).Run()` and returns 0 on exit — it does NOT call `server.Shutdown()` (the daemon keeps running). In attach mode, the existing `q` handler at model.go:285–287 still returns `tea.Quit` (good — that's detach), but the openEditForm/openAddProviderForm/openAddMappingForm functions at model.go:634, 670, 689 must short-circuit with a no-op when `d.detachOnQuit` is true so config mutation is impossible from the attached TUI. **Also wrap `toggleVerboseErrors` at model.go:396-406 with `if d.dispatcher != nil`** so Ctrl+E in attach mode (which has no dispatcher) does not NPE.

#### 8. Subcommand dispatch for attach

**File**: `cmd/freedius/main.go`

**Intent**: Add `attach` to the subcommand dispatch in `run()` alongside `stop` and `status`.

**Contract**: `case "attach": return handleAttach(args[1:])` in the switch at the top of `run()`.

#### 9. IPC event/log client

**File**: `cmd/freedius/ipc_client.go` (new)

**Intent**: Implement SSE client that connects to the daemon's Unix socket and streams events/logs. Provides `<-chan proxy.RequestEvent` and `<-chan proxy.LogEntry` for the TUI to consume (same interface as in-memory channels).

**Contract**:

```go
type IPCClient struct { ... }

func NewIPCClient(socketPath string) (*IPCClient, error)
func (c *IPCClient) Events() <-chan proxy.RequestEvent
func (c *IPCClient) Logs() <-chan proxy.LogEntry
func (c *IPCClient) Stats() (StatsSnapshot, error)
func (c *IPCClient) Config() (*config.Config, error)
func (c *IPCClient) Close() error
```

**SSE client contract (lessons.md §1, §2)**: SSE reading MUST use `bufio.Reader.ReadBytes('\n')` (NOT `bufio.Scanner` — its default 64 KB `MaxScanTokenSize` truncates large tool-use arguments). The reader loop:
1. `bufio.NewReader(resp.Body)`.
2. Loop `r.ReadBytes('\n')` — each line is either `event: <type>`, `data: <payload>`, blank (frame boundary), or `: comment` (skip).
3. On `event: replay` with `data: {"complete": false, ...}` (per F6 contract), the attached TUI shows "showing recent events, earlier history unavailable".
4. JSON decoding of `data:` lines uses `json.Unmarshal` (no trailing-newline issue at decode time, only at encode time per §1).

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go test ./proxy/...` passes (EventBus/LogSink changes)
- `go test ./cmd/freedius/...` passes
- `go build ./cmd/freedius` succeeds

#### Manual Verification:

- Start daemon: `freedius --daemon`
- Attach: `freedius attach` — TUI appears, shows live logs
- Send request: `curl -X POST http://127.0.0.1:8082/v1/messages ...` — appears in attached TUI
- Detach: press `q` in attached TUI — daemon keeps running
- Re-attach: `freedius attach` again — sees accumulated log buffer
- Late attach: start daemon, send 10 requests, then attach — first 10 requests appear via replay
- Stop: `freedius stop` — daemon exits, attach client disconnected gracefully

---

## Testing Strategy

### Unit Tests:

- `proxy/eventbus.go`: `Since()` returns correct events, handles empty ring, handles wrap-around
- `proxy/logtee.go`: `Since()` returns correct entries, `Snapshot()` is non-destructive
- `cmd/freedius/main_test.go`: `--fg` starts headless, `--daemon` + `--fg` mutual exclusion, `stop`/`status` dispatch
- `cmd/freedius/daemon_unix_test.go`: PID file write/read/remove, stale PID detection
- `cmd/freedius/ipc_unix_test.go`: SSE streaming, event replay, socket lifecycle

### Integration Tests:

- Full lifecycle: daemon start → attach → send requests → detach → stop
- Stale socket cleanup: kill daemon uncleanly, restart — socket is reclaimed
- Event replay: start daemon, send N requests, attach — verify N events appear

### Manual Testing Steps:

1. `freedius` — TUI starts, `Ctrl+Z` suspends, `fg` resumes
2. `freedius --fg` — headless, `Ctrl+C` shuts down
3. `freedius --daemon` — forks, `freedius status` shows running
4. `freedius attach` — TUI connects to daemon
5. `freedius stop` — daemon exits cleanly
6. Stale PID: `kill -9 <daemon-pid>`, `freedius status` shows not running, `freedius --daemon` succeeds

## Performance Considerations

- Event replay ring buffer: 10000 entries × ~500 bytes = ~5MB memory. Acceptable for a local dev tool.
- SSE connections: one per attached TUI. Typically 0-1. No scalability concern.
- Socket IPC latency: kernel-level, sub-millisecond. Negligible vs proxy latency.

## References

- Research: `context/changes/daemon-mode/research.md`
- Prior TUI decision: `context/archive/unified-server-logs-tab/plan.md:43` (chose "no headless mode")
- Platform scope: `context/foundation/roadmap.md:217` (Linux + macOS only)
- Bubble Tea suspend: `tty_unix.go:37-47` in bubbletea module

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands (each row may carry a different SHA if the phase is split across commits). Do not rename step titles.

### Phase 1: Ctrl+Z Suspend/Resume

#### Automated

- [x] 1.1 `go vet ./...` passes after adding ctrl+z handler — e08a497
- [x] 1.2 `go test ./proxy/tui/...` passes — e08a497
- [x] 1.3 `go build ./cmd/freedius` succeeds — e08a497

#### Manual

- [x] 1.4 Ctrl+Z suspends TUI, proxy keeps running, fg resumes with state — b937b52

### Phase 2: --fg Headless Mode

#### Automated

- [x] 2.1 `go vet ./...` passes — b937b52
- [x] 2.2 `go test ./cmd/freedius/...` passes (new tests for --fg flag parsing) — b937b52
- [x] 2.3 `go build ./cmd/freedius` succeeds with platform-specific signal files — b937b52
- [x] 2.4 `./freedius --fg --port 0 &` starts, `/health` returns 200 — b937b52

#### Manual

- [x] 2.5 `freedius --fg` shows logs on stderr, no TUI — b937b52
- [x] 2.6 Ctrl+C shuts down gracefully — b937b52
- [x] 2.7 `freedius --daemon --fg` exits with mutual exclusion error — b937b52

### Phase 3: --daemon Background Mode

#### Automated

- [x] 3.1 `go vet ./...` passes — c6014a9
- [x] 3.2 `go test ./cmd/freedius/...` passes (PID file, daemon lifecycle) — c6014a9
- [x] 3.3 `go build ./cmd/freedius` succeeds — c6014a9

#### Manual

- [x] 3.4 `freedius --daemon` forks, prints PID — 0f4b1c5
- [x] 3.5 `freedius status` shows running — 0f4b1c5
- [x] 3.6 `freedius stop` terminates daemon — 0f4b1c5
- [x] 3.7 Stale PID detection works — 0f4b1c5
- [x] 3.8 Already-running detection works — 0f4b1c5

### Phase 4: IPC-Based TUI Attach

#### Automated

- [x] 4.1 `go vet ./...` passes — 0f4b1c5
- [x] 4.2 `go test ./proxy/...` passes (EventBus/LogSink Since methods) — 0f4b1c5
- [x] 4.3 `go test ./cmd/freedius/...` passes (IPC server, client, attach) — 0f4b1c5
- [x] 4.4 `go build ./cmd/freedius` succeeds — 0f4b1c5

#### Manual

- [x] 4.5 `freedius attach` connects to running daemon, shows TUI — 0f4b1c5
- [x] 4.6 Detach with `q` does not kill daemon — 0f4b1c5
- [x] 4.7 Late attach shows replayed events — 0f4b1c5
- [x] 4.8 Full lifecycle: daemon → attach → requests → detach → stop — 0f4b1c5
