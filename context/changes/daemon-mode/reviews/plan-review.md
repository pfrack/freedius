<!-- PLAN-REVIEW-REPORT -->
# Plan Review: Daemon Mode with Foreground Attach

- **Plan**: context/changes/daemon-mode/plan.md (amended, 9 plan.md commits)
- **Mode**: Deep
- **Date**: 2026-06-21
- **Verdict**: REVISE → SOUND (after triage fixes)
- **Findings**: 1 critical | 7 warnings | 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| End-State Alignment | WARNING ⚠️ (F4) |
| Lean Execution | PASS ✅ |
| Architectural Fitness | WARNING ⚠️ (F1, F2+F3) |
| Blind Spots | WARNING ⚠️ (F5, F6, F9) |
| Plan Completeness | WARNING ⚠️ (F7, F8, F10) |

## Grounding

- Paths: 6/6 critical references ✓
  - proxy/tui/model.go:288-289 (ctrl+z handler) ✓
  - proxy/tui/model.go:236-239 (ResumeMsg) ✓
  - proxy/tui/model.go:137-145 (NewDashboard nil-panic contract) ✓
  - proxy/tui/model.go:398-399 (d.dispatcher usage) ✓
  - proxy/logtee.go:46-59 (Snapshot destructive) ✓
  - error-hardening/research.md:287 (os.Args[0] rejected) ✓
- Symbols: 8/8 ✓
- Brief↔plan: ✓ (brief untouched since creation; plan amendments consistent with brief)

## Findings

### F1 — `runtimeDir()` helper referenced but never defined

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:253 (Phase 3 §4 PID file management)
- **Detail**: The amended Phase 3 §4 contract says "use `runtimeDir()` shared helper from F11" but `runtimeDir` is referenced nowhere else in plan.md. The plan asks the implementer to use a function that doesn't exist and isn't defined anywhere. Phase 4 §5 (line 384) needs the same helper for the socket path but doesn't reference it. Without `runtimeDir()`, both PID file and socket path resolution are duplicated and may diverge (the prior F11 finding).
- **Fix**: Add a Phase 3 §7 (new section, before Success Criteria) defining `func runtimeDir() string { if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" { return d }; return os.TempDir() }` to live in `cmd/freedius/paths_unix.go` and `cmd/freedius/paths_windows.go`. Reference it from §3.4 PID path and §4.5 socket path.

### F2 — `waitForShutdown` signature mismatch between Unix and Windows

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Architectural Fitness
- **Location**: plan.md:152 (Unix contract) vs plan.md:168 (Windows stub contract)
- **Detail**: After F7 amendment, the Unix contract is `func waitForShutdown(server *http.Server, cleanup func() error) error` (takes cleanup). The Windows stub at plan.md:164-168 still shows `func waitForShutdown(server *http.Server) error` (no cleanup). Since both functions are called from the same `run()` (Phase 2 §2), the call site would need conditional compilation. Plan §2 §2 (line 127) still references the old signature `waitForShutdown(server)`.
- **Fix A ⭐ Recommended**: Make the Windows stub match: `func waitForShutdown(server *http.Server, _ func() error) error` — discard the cleanup arg on Windows since there's no socket to remove. Update plan.md:127 to `waitForShutdown(server, cleanup)` and update the §4 contract block at line 168.
  - Strength: zero conditional call sites; one signature for both platforms.
  - Tradeoff: wastes one argument on Windows.
  - Confidence: HIGH.
  - Blind spot: Windows has no IPC server in this change so cleanup is always nil — passing a closure would be wasteful but harmless.
- **Fix B**: Two separate functions (`waitForShutdownUnix`, `waitForShutdownWindows`) with build-tag dispatch. More boilerplate.

### F3 — Phase 2 §2 `run()` contract still calls `waitForShutdown(server)` (old signature)

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:127 (Phase 2 §2 Branch in run())
- **Detail**: Plan §2.2 line 127 says "call `waitForShutdown(server)` which blocks on signals". Phase 2 §3 (line 135) amended `waitForShutdown` to take `cleanup func() error`. The call site in §2.2 was not updated. Without this fix, the implementer will compile a call mismatch.
- **Fix**: Update plan.md:127 to: "call `waitForShutdown(server, ipcServer.Shutdown)` where `ipcServer` is the IPCServer instance (Phase 4 §6); pass `nil` when no IPCServer is running (in-process TUI mode without IPC)."

### F4 — Phase 4 §7 attach flow doesn't satisfy NewDashboard's nil-reg contract

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: End-State Alignment
- **Location**: plan.md:400 (Phase 4 §7 TUI client for attach); proxy/tui/model.go:125-145 (NewDashboard signature)
- **Detail**: After F4 amendment, Phase 4 §7 says reuse Dashboard with `detachOnQuit: true`. But `NewDashboard` (model.go:125-145) has hard nil-checks that panic on nil `reg *proxy.Registry` and `dispatcher *proxy.Dispatcher`. The attach client has no Registry or Dispatcher (the daemon has them; attach only observes via SSE). Without a stub or constructor change, attach mode panics at startup. Even with `detachOnQuit`, the existing `d.dispatcher.SetVerboseErrors` call at model.go:398-399 (in `toggleVerboseErrors`) would NPE if dispatcher is nil — Ctrl+E in attach mode would crash the TUI.
- **Fix A ⭐ Recommended**: Add `detachOnQuit`-aware nil-checks. In `toggleVerboseErrors` at model.go:396-406, wrap with `if d.dispatcher != nil`. Add a separate `NewAttachDashboard(events, logs, cfgPath, host, port) *Dashboard` constructor (in proxy/tui/model.go) that accepts nil reg/dispatcher; the constructor panics only if cfg is nil (matching existing pattern) but tolerates nil reg/dispatcher. `runAttach` uses the new constructor.
  - Strength: zero impact on in-process TUI; clear separation of concerns.
  - Tradeoff: two constructors to maintain.
  - Confidence: HIGH — the constructor split matches the in-process vs attach flow boundary.
  - Blind spot: any future field added to Dashboard for the in-process TUI must be re-evaluated for the attach path.
- **Fix B**: Make NewDashboard accept nil reg/dispatcher everywhere; add `if d.dispatcher != nil` to all callsites. Risky — could mask bugs in the in-process path.
- **Fix C**: Pass stub reg/dispatcher from IPCClient that no-op. Adds boilerplate; cleaner is Fix A.

### F5 — Phase 4 §6 doesn't specify cleanup-arg wiring to waitForShutdown

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Blind Spots
- **Location**: plan.md:386-392 (Phase 4 §6 Wire IPC server into daemon mode)
- **Detail**: §6 says "IPC server goroutine starts after `waitForBind`. On shutdown, IPC server shuts down alongside HTTP server." But "alongside" is hand-wavy. The chain requires:
  - IPCServer.Shutdown(ctx) removes the socket file (per §3 contract — but §3 contract doesn't actually say this).
  - The cleanup func passed to waitForShutdown IS `ipcServer.Shutdown`.
  - The plan says all three things in different places (F7 amendment, §6, §4.5) but doesn't tie them into one explicit wiring diagram.
- **Fix**: Add to plan §6: "Wire `ipcServer.Shutdown` as the `cleanup` arg to `waitForShutdown` (see Phase 2 §3). On daemon child startup: `cleanup := ipcServer.Shutdown; waitForShutdown(server, cleanup)`. This guarantees the socket file is removed on graceful SIGTERM-driven shutdown (per F7)."

### F6 — SnapshotSince edge cases unspecified (parallel to F6 EventBus Since gap)

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Blind Spots
- **Location**: plan.md:328 (Phase 4 §2 Event replay — LogSink)
- **Detail**: After F5 amendment, plan says add `SnapshotSince(seq int64) []LogEntry` but doesn't specify behavior for seq<=0, seq>currentSeq, seq==currentSeq, seq<oldest_in_ring. The F6 EventBus contract was specified inline for `Since`; `SnapshotSince` is a parallel method on LogSink and needs the same spec.
- **Fix**: Mirror the F6 contract spec inline for SnapshotSince:
  - `seq <= 0`: return entire ring.
  - `seq > currentSeq`: return nil (nothing yet, switch to live).
  - `seq == currentSeq`: return nil (caught up).
  - `seq < oldest_in_ring`: return what's left, evicted=true.
  Change return signature to `(entries []LogEntry, currentSeq int64, evicted bool)` for parallelism with EventBus.Since.

### F7 — Critical Implementation Details section still describes the OLD PID race / stale socket approach

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:63-64 (Critical Implementation Details)
- **Detail**: Lines 63-64 still describe:
  - "PID file race: Two `freedius --daemon` invocations could race. Check PID file existence + `process.Signal(0)` probe before writing."
  - "Socket cleanup: On daemon crash, the Unix socket file may be stale. On startup, attempt `net.Dial` to the socket — if connection fails, remove and re-listen."
  Both contradict the amended Phase 3 §4 contract (now uses flock + syscall.Kill(0) + PID/start_time + /proc check) and the Phase 4 §5 socket lifecycle (now driven by IPCServer.Shutdown).
- **Fix**: Replace lines 63-64 with the post-amendment summaries: "PID file race closed via `syscall.Flock` on sidecar `freedius.lock` + `syscall.Kill(pid, 0)` liveness + `<pid>\t<start_time_unix_nano>` file format (see Phase 3 §4)." and "Socket cleanup driven by `IPCServer.Shutdown` which removes `<runtimeDir>/freedius.sock`; called via `waitForShutdown(server, ipcServer.Shutdown)` cleanup arg (see Phase 4 §6)."

### F8 — Phase 4 §5 socket path fallback still says `$TMPDIR` instead of `os.TempDir()`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:384 (Phase 4 §5 Socket path and lifecycle)
- **Detail**: After F11 finding (prior review), Phase 3 §4 was amended to use `os.TempDir()`. Phase 4 §5 (line 384) still says "fallback: `$TMPDIR/freedius.sock`". The two files will use different path resolution unless both go through `runtimeDir()` (which doesn't exist yet — see F1).
- **Fix**: Replace "$TMPDIR/freedius.sock" with "use `runtimeDir()` (defined in Phase 3 §7 per F1) and append `freedius.sock`".

### F9 — `stopDaemon` and `daemonStatus` contract thin; PID file read + SIGTERM send not specified

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Blind Spots
- **Location**: plan.md:226-228 (Phase 3 §2 daemon_unix.go contract); plan.md:263-272 (Phase 3 §5 subcommand dispatch)
- **Detail**: Plan §2 lists `stopDaemon` and `daemonStatus` as exported functions on daemon_unix.go, and §5 says `handleStop()` / `handleStatus()` dispatch to them. But neither function's contract is specified:
  - `stopDaemon()`: read PID file via `readPIDFile()` (which returns PID + start_time per §4) → `syscall.Kill(pid, SIGTERM)` → wait briefly for process exit (e.g., 200ms poll loop) → on timeout, return error "daemon did not exit in time". Also: should `stopDaemon` remove the PID file? Plan §3.4 §4 says PID file removal happens at SIGTERM receipt (Phase 2 waitForShutdown cleanup path) — so `stopDaemon` does NOT need to remove it.
  - `daemonStatus()`: read PID file → `syscall.Kill(pid, 0)` → return `(running bool, pid int, err error)` where running is true if Kill returns nil or EPERM, false on ESRCH.
  Without explicit contracts, the implementer has to guess.
- **Fix**: Replace the bare signature list at plan.md:219-228 with a full contract spec for each of the three functions. Update Phase 3 §5 to reference the explicit contracts.

### F10 — `views.go:560` reference is wrong; Log tab refresh is at model.go:560

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:328 (Phase 4 §2 Event replay — LogSink)
- **Detail**: Plan says "the TUI Log tab refresh at views.go:560". `proxy/tui/views.go` is only 411 lines. The Log tab refresh is at `proxy/tui/model.go:560`: `content = renderLogTab(d.logBuffer.all(), ...)`. Note: this reads from `d.logBuffer.all()` (the in-process TUI's local ring buffer), NOT from `sink.Snapshot()`. So the original F5 premise (that production code depends on destructive Snapshot) was incorrect — only tests at logtee_test.go:45,75,101,131 call `sink.Snapshot()`, and they test overflow behavior, not destructive semantics. The F5 fix (keep destructive Snapshot + add SnapshotSince) is still safe; the rationale just needs slight rewording.
- **Fix**: Update plan.md:328 to: "logtee_test.go:45,75,101,131 assert Snapshot overflow behavior; in production, the TUI Log tab reads from its own `d.logBuffer` (model.go:560), not from `sink.Snapshot()`, so Snapshot destructiveness is internal-only."

### F11 — Progress section convention drift: SHA appended to automated rows but not manual rows

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: plan.md:478-489 (Progress §Phase 1)
- **Detail**: §Progress convention (line 496) says "Append ` — <commit sha>` when a step lands. Do not rename step titles." Phase 1 automated rows (1.1, 1.2, 1.3) carry SHA suffixes ` — e08a497`. Phase 1 manual row (1.4) does not — but it's still pending. This is correct per the convention (only flip-to-done rows get SHA). However, the convention is silent on whether one SHA per phase is fine or whether different SHAs per row are allowed. The current state shows three rows with the same SHA (all Phase 1 work landed in one commit). If Phase 2 work lands in three separate commits (one per automated row), each row would carry a different SHA. The convention doesn't forbid this but doesn't explicitly bless it either.
- **Fix**: No change needed. The current state is correct. Document the per-row-SHA allowance in the convention line for clarity: "Append ` — <commit sha>` when a step lands (each row may carry a different SHA if the phase is split across commits)."

## Notes

The plan amendments (8 plan.md commits since the original draft) addressed most of the prior impl-review's high-impact findings (F2 PID race, F3 os.Args[0], F4 attach reuse, F5 Snapshot, F6 Since edges, F7 shutdown cleanup, F8 SSE lessons). This fresh plan-review focuses on what the amendments DID NOT touch: the architectural seams where multiple amendments create new inter-section contracts that weren't explicitly wired (F2, F3, F4, F5, F9), and consistency gaps between the Critical Implementation Details narrative and the amended Phase contracts (F7, F8, F10).

F1 is critical because the implementer cannot proceed past Phase 3 §4 without `runtimeDir()` — the plan tells them to use a function that doesn't exist. This is a one-line fix (define the helper in a new section).

F4 is high-impact because the attach-mode panic would surface only at runtime, not at compile time, and could ship as a regression. The implementer should resolve this before Phase 4 implementation begins.
