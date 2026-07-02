---
date: 2026-06-21T15:30:00+02:00
researcher: pawel
git_commit: 0fc1afe
branch: providers
repository: pfrack/freedius
topic: "Daemon mode with foreground attach for freedius"
tags: [research, daemon, tui, bubbletea, signal-handling, ipc, process-management]
status: complete
last_updated: 2026-06-21
last_updated_by: pawel
---

# Research: Daemon Mode with Foreground Attach

**Date**: 2026-06-21T15:30:00+02:00
**Researcher**: pawel
**Git Commit**: 0fc1afe
**Branch**: providers
**Repository**: pfrack/freedius

## Research Question

How to add daemon mode (headless proxy) and foreground/attach capability so the proxy runs in the background with optional TUI resumption?

## Summary

Three complementary approaches were identified, ordered by implementation complexity and user value:

1. **Ctrl+Z suspend/resume** (trivial, 2 lines) — Bubble Tea natively supports `SIGTSTP`/`SIGCONT`. Add `ctrl+z` handler to suspend the TUI while the proxy keeps running. `fg` resumes the TUI with full state.
2. **`--fg` headless flag** (moderate) — Run proxy without TUI for Docker/systemd/scripts. Explicit `signal.NotifyContext` for graceful shutdown.
3. **`--daemon` background flag** (moderate-high) — Fork to background with PID file management. Re-exec self with `--fg`.
4. **IPC-based TUI attach** (future, large) — Separate TUI client connects to daemon via Unix socket. Requires event replay protocol.

**Recommendation**: Implement all three in phases. Ctrl+Z is immediate value with zero risk. `--fg` enables container usage. `--daemon` adds convenience. IPC attach is a future enhancement.

## Detailed Findings

### Phase 1: Ctrl+Z Suspend/Resume (Bubble Tea Native)

Bubble Tea v2 has **built-in suspend/resume support** on Unix. The machinery is fully implemented:

- `tea.SuspendMsg` / `tea.ResumeMsg` message types (`tea.go:563-578`)
- `p.suspend()` calls `releaseTerminal(true)` → `suspendProcess()` → `RestoreTerminal()` (`tty.go:12-22`)
- `suspendProcess()` sends `SIGTSTP` to the process group, blocks on `SIGCONT` (`tty_unix.go:39-47`)
- `suspendSupported = true` on Unix (`tty_unix.go:37`)

**Implementation**: Two lines of code in `proxy/tui/model.go:275`:

```go
case "ctrl+z":
    return d, tea.Suspend
```

Optional `ResumeMsg` handler in `Update()`:

```go
case tea.ResumeMsg:
    d.stats.message = ""
    return d, nil
```

**State preservation**: All `Dashboard` fields survive suspension (in-process memory). Channel reads pause during suspension; events queue in the buffered channels (1000 capacity). On resume, the TUI receives a burst of queued events.

**Caveat**: While suspended, the proxy server continues running in its goroutine (`main.go:207-211`). This is the desired behavior — requests are proxied, events accumulate.

### Phase 2: `--fg` Headless Flag

Run the proxy in foreground without the TUI. For Docker, systemd, scripts, and CI.

**Flag definition** (add to `cmd/freedius/main.go:86-107`):

```go
flagFg := fs.Bool("fg", false, "run headless in foreground (no TUI, for Docker/scripts)")
```

**Branch in `run()`** (replace `prog.Run()` at `main.go:217-226`):

```go
if fg {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
    defer cancel()
    server.Shutdown(shutdownCtx)
} else {
    // existing TUI path
    model := tui.NewDashboard(...)
    prog := tea.NewProgram(model)
    prog.Run()
    // ...existing shutdown...
}
```

**Log output**: Replace `io.Discard` (`main.go:128`) with `os.Stderr` when headless. The `--log-format json` flag becomes important for machine-parseable output.

**No signal handling currently exists** in the Go code. Bubble Tea handles signals internally for the TUI path, but the headless path needs explicit `signal.NotifyContext`.

### Phase 3: `--daemon` Background Flag

Fork to background with PID file management.

**Flag definition**:

```go
flagDaemon := fs.Bool("daemon", false, "run as background daemon (no TUI, forks to background)")
flagDaemonShorthand := fs.Bool("d", false, "shorthand for --daemon")
```

**Mutual exclusion**: `--daemon` + `--fg` = error, exit 2.

**Fork strategy**: Re-exec self with `--fg`:

```go
if daemon {
    cmd := exec.Command(os.Args[0], append(fgArgs(args), "--fg")...)
    cmd.Stdout = logFile
    cmd.Stderr = logFile
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
    cmd.Start()
    writePIDFile(cmd.Process.Pid)
    fmt.Fprintf(os.Stderr, "freedius: daemon started (PID %d)\n", cmd.Process.Pid)
    return 0
}
```

**PID file location**: `$XDG_RUNTIME_DIR/freedius.pid` (typically `/run/user/<uid>/freedius.pid`). Fallback: `/tmp/freedius.pid`. `$XDG_RUNTIME_DIR` is preferred because it's user-scoped and tmpfs-mounted (cleared on reboot).

**PID lifecycle**:

```
Startup:   check PID file -> if alive, exit 1 ("already running") -> write PID file
Runtime:   PID file remains on disk
Shutdown:  defer os.Remove(pidFile) -> signal handler removes on SIGTERM/SIGINT
Crash:     stale PID detected on next startup via signal-0 probe
```

**Process detection**: `os.FindProcess(pid)` + `process.Signal(syscall.Signal(0))` on Unix.

### Phase 4 (Future): IPC-Based TUI Attach

Separate TUI client connects to a running daemon. Requires:

- **Unix Domain Socket** (`~/.freedius/freedius.sock`) — zero new deps, stdlib `net` package
- **SSE streaming** for events/logs over HTTP on the socket
- **Sequence numbers** on EventBus/LogSink for replay on late attach
- **Command endpoint** for config mutation from TUI

**Protocol design over single UDS**:

| Endpoint | Method | Purpose |
|---|---|---|
| `/v1/events?since=N` | GET | SSE stream of `RequestEvent` JSON |
| `/v1/logs?since=N` | GET | SSE stream of `LogEntry` JSON |
| `/v1/stats` | GET | JSON snapshot (uptime, requests, errors) |
| `/v1/command` | POST | JSON command dispatch |
| `/v1/config` | GET | Full config JSON snapshot |

**Changes required**:
- Add sequence counter + ring buffer + `Since()` method to `EventBus` (`proxy/eventbus.go`) and `LogSink` (`proxy/logtee.go`)
- TUI refactor: `NewDashboard` gains optional IPC client instead of raw channels
- `submitForm()` becomes HTTP POST to `/v1/command` instead of direct config mutation
- Daemon writes PID + socket path to `~/.freedius/` for attach command to discover

## Architecture Insights

### Current Coupling

The TUI and proxy are tightly coupled in `main.go:217-226`:
- `tea.Program.Run()` blocks the main goroutine
- TUI exit triggers `server.Shutdown()` (`main.go:228-233`)
- No signal handling exists in Go code — Bubble Tea handles SIGINT/SIGTERM internally
- Config mutation from TUI is direct (`d.config.Lock()` at `model.go:763`)

### Event/Log Architecture

Both `EventBus` and `LogSink` use a single shared channel pattern (`Subscribe()` returns `<-chan`). All consumers compete for the same events. No replay capability exists — once consumed, events are gone. `LogSink.Snapshot()` is destructive (drains the channel).

### Flag Precedence Pattern

`cmd/freedius/main.go:115-116` uses `fs.Visit()` to build a `setFlags` map, enabling flag > env > default precedence. New flags should follow this pattern.

### Historical Context

- The codebase deliberately consolidated from multi-subcommand to unified mode (no subcommands)
- `context/archive/unified-server-logs-tab/plan.md:43` explicitly chose "No headless mode"
- `context/foundation/roadmap.md:185` noted `freedius tui` vs `freedius --tui` as an open question
- PRD §Non-Goals: "No web UI in v1" — daemon mode doesn't violate this

## Code References

- `cmd/freedius/main.go:67-69` — entry point: `main()` → `os.Exit(run(os.Args[1:]))`
- `cmd/freedius/main.go:86-107` — flag definitions (where new flags go)
- `cmd/freedius/main.go:115-116` — `setFlags` map for flag precedence
- `cmd/freedius/main.go:127-132` — logger creation, `io.Discard` (needs `os.Stderr` for headless)
- `cmd/freedius/main.go:189` — `NewEventBus(1000)` — buffered channel
- `cmd/freedius/main.go:206-211` — server goroutine (`ListenAndServe`)
- `cmd/freedius/main.go:217-226` — TUI creation and `prog.Run()` blocking
- `cmd/freedius/main.go:228-233` — TUI exit → `server.Shutdown()`
- `cmd/freedius/main.go:244-267` — `printUsage()` (needs daemon/fg docs)
- `cmd/freedius/main.go:274-284` — `waitForBind()` (port-in-use detection)
- `proxy/eventbus.go:31-78` — EventBus struct, Emit, Subscribe (no replay)
- `proxy/logtee.go:24-59` — LogSink struct, Subscribe, Snapshot (destructive)
- `proxy/tui/model.go:78-115` — Dashboard struct (all TUI state)
- `proxy/tui/model.go:172-174` — `Init()` starts channel reads
- `proxy/tui/model.go:212-215` — `esc` quits
- `proxy/tui/model.go:275-347` — `handleTabModeKeyPress` (where ctrl+z goes)
- `proxy/tui/model.go:280-282` — `q`/`ctrl+c` quits
- `proxy/tui/model.go:525-582` — `View()` sets AltScreen per frame
- `proxy/tui/model.go:584-608` — `waitForEvent`/`waitForLog` channel-blocking commands
- `proxy/tui/model.go:760-828` — `submitForm()` direct config mutation
- `config/config.go:28-33` — `Config` struct with `sync.RWMutex`
- `config/config.go:315-367` — `Save()` atomic write
- `test-manual.sh:61` — `script -eq` provides pseudo-TTY for Bubble Tea

## Historical Context (from prior changes)

- `context/archive/unified-server-logs-tab/plan.md:43` — deliberately chose "No headless mode" during consolidation. This research reopens that decision.
- `context/foundation/roadmap.md:185` — open question: `freedius tui` (subcommand) vs `freedius --tui` (flag). This research answers it: flags, not subcommands.
- `context/archive/tui-dashboard/research.md` — TUI architecture decision. Recommended `freedius tui` subcommand; the codebase went with unified mode instead.
- `context/foundation/prd.md:107` — "No web UI in v1" non-goal. Daemon mode is orthogonal (headless proxy, not a web UI).

## Related Research

- `context/archive/tui-dashboard/research.md` — TUI vs Web UI vs Native GUI decision
- `context/archive/unified-server-logs-tab/research.md` — unified mode consolidation

## Open Questions

1. **Should `--daemon` fork or just run headless?** Docker/systemd prefer `--fg` (no fork). `--daemon` with fork is a convenience for manual runs. Could drop `--daemon` and just have `--fg` + shell `&`.
2. **PID file location**: `$XDG_RUNTIME_DIR` vs `~/.config/freedius/`? Former is cleaner (auto-cleaned on reboot), latter is more discoverable.
3. **Log file for daemon mode**: `--log-file` flag or just stderr? stderr is sufficient for systemd/Docker; `--log-file` helps manual background runs.
4. **SIGHUP config reload**: Should SIGHUP trigger config reload in daemon mode? Non-trivial due to config flowing through multiple layers. Defer to future iteration.
5. **IPC attach scope**: Is `freedius attach` (separate TUI client connecting via Unix socket) needed, or is Ctrl+Z suspend/resume sufficient for the single-user case?
