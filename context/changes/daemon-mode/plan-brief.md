# Daemon Mode with Foreground Attach — Plan Brief

> Full plan: `context/changes/daemon-mode/plan.md`
> Research: `context/changes/daemon-mode/research.md`

## What & Why

Add daemon mode so freedius can run as a background proxy without the TUI, and an `attach` command to connect the TUI to a running daemon. Motivation: users want the proxy always-on in the background while being able to check live logs on demand — like `tmux attach` for a proxy.

## Starting Point

Today, freedius always starts TUI + proxy in one foreground process (`cmd/freedius/main.go:217-226`). `prog.Run()` blocks; TUI exit kills the server. No signal handling exists in Go code (Bubble Tea handles it internally). EventBus/LogSink are single-channel with no replay. No headless or daemon mode.

## Desired End State

| Command | Behavior |
|---------|----------|
| `freedius` | Unchanged: TUI + proxy. New: `Ctrl+Z` suspends, `fg` resumes. |
| `freedius --fg` | Headless proxy in foreground. For Docker, systemd, scripts. |
| `freedius --daemon` / `-d` | Proxy forks to background, writes PID file. |
| `freedius attach` | TUI connects to running daemon via Unix socket. |
| `freedius stop` | Sends SIGTERM to daemon via PID file. |
| `freedius status` | Shows if daemon is running. |

## Key Decisions Made

| Decision | Choice | Why | Source |
|----------|--------|-----|--------|
| IPC mechanism | Unix domain sockets + HTTP/SSE | Zero new deps, reuses `net/http` stack, stdlib only | Research |
| Platform support | Linux + macOS, build-tagged | Windows parked per roadmap; `_unix.go`/`_windows.go` allows future addition | Research |
| Daemon strategy | Re-exec self with `--fg` | No double-fork fragility, clean separation | Research |
| PID file location | `$XDG_RUNTIME_DIR/freedius.pid` | Tmpfs-mounted, auto-cleaned on reboot, user-scoped | Research |
| Event replay | Ring buffer + sequence numbers on EventBus/LogSink | Enables late-attach with history; 10000-entry ring (~5MB) | Plan |
| Log output in headless | stderr only | Works with systemd/Docker/nohup; no `--log-file` flag | User choice |
| Attach scope | Read-only TUI (no config mutation) | Config mutation over IPC adds complexity; defer to v2 | Plan |

## Scope

**In scope:**
- `Ctrl+Z` suspend/resume in TUI
- `--fg` headless flag with platform-specific signal handling
- `--daemon`/`-d` flags with fork/re-exec and PID file
- `freedius stop` / `freedius status` subcommands
- Unix socket IPC server in daemon mode
- SSE streaming for events/logs over IPC
- Event replay with sequence numbers on EventBus/LogSink
- `freedius attach` command
- Build-tagged platform files (`_unix.go`, `_windows.go` stubs)

**Out of scope:**
- Windows support (parked per roadmap)
- SIGHUP config reload
- Config mutation from attached TUI (read-only attach)
- Multiple simultaneous TUI clients
- `--log-file` flag (stderr sufficient)
- Web UI / HTTP dashboard

## Architecture / Approach

Platform-specific code in `cmd/freedius/` uses `//go:build` tags: `signal_unix.go`, `daemon_unix.go`, `ipc_unix.go` (plus `_windows.go` stubs). Shared logic in `main.go` calls exported functions from platform files. The IPC server runs alongside the HTTP proxy server in daemon/fg mode, serving SSE streams over a Unix socket at `$XDG_RUNTIME_DIR/freedius.sock`. EventBus and LogSink gain ring buffers with sequence numbers for replay on late attach.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Ctrl+Z | Suspend/resume TUI, proxy keeps running | Zero — Bubble Tea native support |
| 2. --fg | Headless foreground mode | Signal handling correctness per platform |
| 3. --daemon | Background fork + PID management | PID file races, stale detection |
| 4. IPC attach | TUI connects to daemon via socket | Event replay correctness, socket lifecycle |

**Prerequisites:** None (each phase builds incrementally on the previous but can be tested independently).
**Estimated effort:** ~4 sessions across 4 phases. Phase 1 is trivial (~15 min). Phases 2-3 are moderate (~1 session each). Phase 4 is the largest (~2 sessions).

## Open Risks & Assumptions

- `$XDG_RUNTIME_DIR` may not be set on some Linux distros — fallback to `$TMPDIR` handles this
- Bubble Tea's suspend/resume on macOS Terminal.app vs iTerm2 vs Alacritty may behave differently — needs manual testing on each
- Event replay ring buffer (10000 entries) may be insufficient for very long detach periods — acceptable for a local dev tool
- IPC client (`freedius attach`) needs its own TUI model variant backed by SSE channels — not just the in-memory `Dashboard`

## Success Criteria (Summary)

1. `freedius` with no args works exactly as before, plus `Ctrl+Z` suspend/resume
2. `freedius --fg` runs proxy headless in foreground, logs to stderr
3. `freedius --daemon` forks to background, `freedius stop`/`freedius status` work
4. `freedius attach` connects to daemon, shows live logs, detach doesn't kill daemon
