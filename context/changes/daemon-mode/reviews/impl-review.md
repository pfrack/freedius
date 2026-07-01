<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Daemon Mode with Foreground Attach

- **Plan**: context/changes/daemon-mode/plan.md
- **Scope**: Full plan (4 of 4 phases)
- **Date**: 2026-06-22
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical | 3 warnings | 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️ (F1, F2, F3) |
| Scope Discipline | PASS ✅ |
| Safety & Quality | WARNING ⚠️ (F4) |
| Architecture | PASS ✅ |
| Pattern Consistency | OBSERVATION (F5) |
| Success Criteria | PASS ✅ |

## Grounding

- Plan: 4 phases, 619 lines
- Commits: `b937b52` (p1+p2), `c6014a9` (p3), `0f4b1c5` (p4), `8640c46` (epilogue)
- Files changed: 18 (11 new, 7 modified)
- Progress: all `- [x]` with SHAs

## Automated Verification

| Check | Result |
|-------|--------|
| `go vet ./...` | PASS |
| `go test ./...` | PASS (all packages) |
| `go build ./cmd/freedius` | PASS |

## Findings

### F1 — `/v1/stats` Port and Host fields never populated

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `cmd/freedius/ipc_unix.go:199-207`
- **Detail**: `StatsSnapshot` declares `Host` and `Port` fields with JSON tags, but `handleStats` never sets them — they serialize as `""`. Plan contract says the stats endpoint should return port and host.
- **Fix**: Add host/port fields to `NewIPCServer` constructor, populate from the HTTP server's address or config. One-line fix at `handleStats`.
- **Decision**: FIXED — added host/port to NewIPCServer, populated in handleStats

### F2 — SSE client ignores `event: replay` lines

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: `cmd/freedius/ipc_client.go:161-167`
- **Detail**: The SSE reader processes only `data:` lines and ignores `event:` lines entirely. The `event: replay` message with `complete: false` is never parsed, so the attached TUI cannot detect incomplete history and show "showing recent events, earlier history unavailable". The IPC server emits these events correctly, but the client discards them.
- **Fix**: Parse `event:` lines in `streamSSE`, track current event type, and surface replay metadata to the TUI via a channel or callback.
- **Decision**: FIXED — streamSSE now tracks event types, surfaces replay metadata via ReplayStatus channel

### F3 — O(n) ring eviction in EventBus/LogSink

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: `proxy/eventbus.go:69-72`, `proxy/logtee.go:196-198`
- **Detail**: `copy(b.ring, b.ring[1:])` shifts the entire 10k-element slice left by one on every eviction. Under sustained high throughput, this is O(n) per `Emit()` while holding `ringMu` (write lock), blocking all `Since()` readers. The TUI already has a circular buffer implementation (`proxy/tui/model.go:34-70`) that avoids this.
- **Fix**: Refactor to use a circular buffer (head index + modular arithmetic). Apply to both EventBus and LogSink ring buffers.
- **Decision**: FIXED — refactored both EventBus and LogSink to circular buffer pattern

### F4 — `go run` detection uses broad temp dir check

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `cmd/freedius/daemon_unix.go:32`
- **Detail**: Plan specified `/go-build<hex>/exe/` pattern for detecting `go run`. Implementation uses `strings.HasPrefix(exePath, os.TempDir())` which is broader — may false-positive on legitimate binaries in `/tmp`. Also, plan mentioned `os.Stat(/proc/%d)` on Linux for PID-reuse guarding — omitted (only `Kill(pid,0)` is used).
- **Fix**: Narrow the check to match the plan's pattern, or document the broader approach as intentional. The current approach is functionally safe.
- **Decision**: FIXED — narrowed to match /go-build<hex>/exe/ pattern via isGoRunExecutable helper

### F5 — Duplicated socket path construction

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: `cmd/freedius/main.go:226`, `cmd/freedius/attach.go:18`
- **Detail**: `filepath.Join(runtimeDir(), "freedius.sock")` is written inline in both files. The plan created `pidFilePath()` and `lockFilePath()` helpers to prevent path divergence. The socket path has no equivalent helper.
- **Fix**: Add `func socketPath() string` to `paths_unix.go` (and Windows stub), use in both call sites.
- **Decision**: FIXED — added socketPath() to paths_unix.go and paths_windows.go

### F6 — IPC server goroutine started before bind confirmation

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: `cmd/freedius/main.go:228`
- **Detail**: `ipcServer.ListenAndServe()` goroutine starts before `waitForBind(serverErr)`. If the HTTP proxy fails to bind, the IPC socket is already listening. The error from `ListenAndServe()` is silently discarded. Low-risk since the IPC socket is cleaned up on shutdown, but it's a minor ordering inconsistency.
- **Fix**: Move IPC server start to after `waitForBind` succeeds. Log the error instead of discarding.
- **Decision**: FIXED — IPC goroutine is already after waitForBind; logged error instead of discarding

### F7 — SSE client has no reconnection logic

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: `cmd/freedius/ipc_client.go:134-167`
- **Detail**: `streamSSE` returns silently on any read error. If the daemon restarts or the connection drops, the goroutine exits and the TUI stops receiving events with no user feedback. Acceptable for v1 of attach (single-user, local tool), but worth noting for future improvement.
- **Fix**: Add retry loop with backoff, or close channels on exit so TUI can detect disconnect.
- **Decision**: FIXED — close events/logs channels on SSE exit so TUI detects disconnect

## Notes

The implementation is solid. All 4 phases are implemented correctly with build-tagged platform files, proper signal handling, PID file race protection via flock, and SSE encoding per lessons.md. The 3 warnings are minor: 2 are plan-adherence drifts (stats fields, replay event parsing) and 1 is a performance pattern (O(n) ring eviction). The 4 observations are hygiene and resilience improvements for future iteration.
