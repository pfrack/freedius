<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Daemon Mode with Foreground Attach

- **Plan**: context/changes/daemon-mode/plan.md
- **Scope**: Full plan (4 of 4 phases)
- **Date**: 2026-06-21
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical | 8 warnings | 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | FAIL — Phases 2–4 entirely unimplemented |
| Scope Discipline | WARNING — Plan §Progress out of sync with reality |
| Safety & Quality | WARNING — 4 findings (F2 PID race, F5 Snapshot contract, F7 socket cleanup, F11 path resolution) |
| Architecture | WARNING — 3 findings (F4 DashboardIPC, F6 Since edge cases, F11 path helper) |
| Pattern Consistency | WARNING — 1 finding (F8 SSE lessons not echoed) + F3 os.Args[0] |
| Success Criteria | PASS — Phase 1 automated checks all pass on the uncommitted diff |

## Context

Review covers a plan-vs-reality check across all 4 phases. Phases 2–4
are entirely unimplemented (no files exist). Phase 1 code (7 lines,
ctrl+z + ResumeMsg handler) lives in the working tree, uncommitted.
Plan §Progress shows 1.1/1.2/1.3 as `- [x]` but no commit lands them.

HEAD = 0fc1afe (add-popular-providers W4). `git log --all --grep=
"daemon|ctrl+z|--fg"` returns no commits on any branch.

## Findings

### F1 — Phases 2–4 are entirely unimplemented

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: cmd/freedius/ (missing: signal_unix.go, signal_windows.go, daemon_unix.go, daemon_windows.go, ipc_unix.go, ipc_windows.go, attach.go, ipc_client.go); proxy/eventbus.go and proxy/logtee.go lack ring buffer + Since() + sequence numbers
- **Detail**: No commits land any Phase 2/3/4 work. The plan describes 8 new files plus modifications to EventBus/LogSink; none exist.
- **Fix A ⭐ Recommended**: Treat Phase 1 as committed (verify the working-tree diff is correct, commit it, append SHA to §Progress), then proceed to Phase 2.
  - Strength: Phase 1 diff is verified correct, so preserving the work is free.
  - Tradeoff: requires a Phase 1 commit before /10x-implement can resume cleanly.
  - Confidence: HIGH — 7-line diff matches plan §1.1 exactly.
  - Blind spot: still needs to clear §Progress of false-positive check marks.
- **Fix B**: Revert plan §Progress to all `- [ ]`, run /10x-implement from scratch.
  - Strength: clean slate.
  - Tradeoff: loses verified Phase 1 work.
  - Confidence: HIGH.
  - Blind spot: user has to redo work that's already done.
- **Decision**: PENDING

### F2 — PID file race is classic TOCTOU

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: Plan §3.4 (plan.md:243–247)
- **Detail**: "check PID file → signal(0) probe → write" has a window where two concurrent `freedius --daemon` invocations can both pass the probe and both write. PID reuse by kernel after fast crash/restart can point at wrong process.
- **Fix A ⭐ Recommended**: `syscall.Flock` on sidecar .lock file before probe; release in defer. Optionally store PID + start-time in PID file and validate via /proc/<pid> (Linux) or syscall.Kill(pid, 0) + ESRCH check (macOS).
  - Strength: portable on Linux + macOS, idiomatic Unix pattern.
  - Tradeoff: one extra file (.lock).
  - Confidence: HIGH — flock well-supported on both targets.
  - Blind spot: doesn't help if PID has been recycled; mitigated by start-time validation.
- **Fix B**: Atomic rename (write to .tmp, then os.Rename).
  - Strength: no new file.
  - Tradeoff: still has probe-then-rename window.
  - Confidence: MED.
  - Blind spot: doesn't address PID recycling.
- **Decision**: PENDING

### F3 — `os.Args[0]` re-exec is explicitly rejected in this codebase

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Pattern Consistency
- **Location**: Plan §3.2 (plan.md:209); research.md:117
- **Detail**: error-hardening research (context/archive/error-hardening/research.md:287) explicitly rejected `os.Args[0]` as "unreliable (can be relative)" and called out `go run` / `go install` / Homebrew as failure modes. Re-execing via `os.Args[0]` resurrects every one.
- **Fix**: Resolve binary path via `os.Executable()`; cache it. Refuse `--daemon` under `go run` with clear error.
  - Strength: matches established pattern; tests at main_test.go:454 already build real binary into t.TempDir.
  - Tradeoff: tests must build, not go run.
  - Confidence: HIGH.
  - Blind spot: user experience for `go install` users.
- **Decision**: PENDING

### F4 — `DashboardIPC` hand-wave + `submitForm` race in attach mode

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Architecture
- **Location**: Plan §4.7 (plan.md:380–386), §4 scope (plan.md:42); model.go:285–287, model.go:767–835
- **Detail**: Two coupled gaps: (a) "DashboardIPC adapter that implements the same interface" is hand-wavy — Dashboard is a concrete struct, not an interface; plan doesn't say whether to reuse Dashboard or parallel-struct. (b) Plan §4 marks config mutation as "out of scope" but existing `q` handler (model.go:285–287) returns tea.Quit which triggers server.Shutdown (main.go:228–233), so `q` in attached TUI would kill daemon — and `e`/`enter`/`a`/`p` would silently mutate daemon config via submitForm.
- **Fix A ⭐ Recommended**: Reuse Dashboard as-is. IPCClient.Events()/Logs() return `<-chan proxy.RequestEvent` / `<-chan proxy.LogEntry` driven by SSE — matches existing Dashboard fields (model.go:80–81). Add `detachOnQuit bool` field to Dashboard (default false in in-process TUI, true in attach). New `runAttach()` entry point runs `prog.Run()` without the Shutdown follow-up. Suppress form entry in attach mode by adding `if d.detachOnQuit { return d, nil }` at top of openEditForm/openAddProviderForm/openAddMappingForm.
  - Strength: zero duplication; smallest diff.
  - Tradeoff: adds one bool field to Dashboard.
  - Confidence: HIGH — channel signatures match exactly.
  - Blind spot: future contributors enabling "edit in attach" need to remember to reverse the guard.
- **Fix B**: Create AttachedDashboard wrapping *Dashboard that overrides quit handler.
  - Strength: clearer separation.
  - Tradeoff: two structs to keep in sync.
  - Confidence: MED.
  - Blind spot: introduces a parallel model that must mirror Dashboard.
- **Decision**: PENDING

### F5 — `LogSink.Snapshot()` behavior change breaks existing callers

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: Plan §4.2 (plan.md:314); proxy/logtee.go:46–59; proxy/logtee_test.go:101; proxy/tui/views.go:560
- **Detail**: Plan says "Make Snapshot() non-destructive (read from ring, not channel)". Snapshot() is currently destructive (drains channel). Existing tests at logtee_test.go:101 assert `len(entries) <= capacity`, and TUI Log tab refreshes via ring buffer at views.go:560. A contract change without an audit can silently break callers.
- **Fix A ⭐ Recommended**: Keep destructive Snapshot() as-is. Add separate `SnapshotSince(seq int64) []LogEntry` for IPC path that reads from new ring.
  - Strength: zero risk to existing behavior.
  - Tradeoff: two methods.
  - Confidence: HIGH.
  - Blind spot: none significant.
- **Fix B**: Rewrite Snapshot() as non-destructive + add Drain() for destructive callers. Audit views.go:560 first.
  - Strength: cleaner API.
  - Tradeoff: bigger blast radius; must find all destructive callers.
  - Confidence: MED.
  - Blind spot: untested callers.
- **Decision**: PENDING

### F6 — `Since(seq)` edge cases unspecified; replay-completeness undetectable

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Architecture
- **Location**: Plan §4.1, §4.2 (plan.md:302–316)
- **Detail**: Contract is `(events, currentSeq, err error)` but no spec for: (a) seq > currentSeq — empty? error? (b) seq < oldest-in-ring — silently return what's left? (c) seq == 0 — initial attach, return entire ring? Without contract, SSE endpoint can emit partial replays client cannot detect.
- **Fix**: Extend contract to `(events, currentSeq, evicted bool, err)`. When evicted=true, SSE envelope emits `replay_complete: false` so attached TUI can show "showing recent events, earlier history unavailable".
  - Strength: clients always know whether replay is whole or partial.
  - Tradeoff: tiny wire-format addition.
  - Confidence: HIGH.
  - Blind spot: clients on old protocol won't see the flag (acceptable for v1).
- **Decision**: PENDING

### F7 — `stop` doesn't clean up the socket; leaks on unclean shutdown

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: Plan §4.5 (plan.md:370), §3.6 (plan.md:286)
- **Detail**: Plan §4.5 says "On shutdown, defer os.Remove(socketPath)". Phase 2 only handles --fg; daemon child running --fg must trap SIGTERM and run socketCleanup() before exiting. If signal handler doesn't run cleanup (signal kills process directly without trapping), socket leaks.
- **Fix**: Make `waitForShutdown` contract explicit: "trap SIGTERM, run server.Shutdown, run ipcServer.Shutdown (which removes socket), exit 0". Add test asserting `freedius stop` removes both $XDG_RUNTIME_DIR/freedius.pid AND $XDG_RUNTIME_DIR/freedius.sock.
  - Strength: explicit contract prevents leak.
  - Tradeoff: more cleanup code in shutdown path.
  - Confidence: HIGH.
  - Blind spot: SIGKILL (-9) still bypasses cleanup — mitigated by stale socket detection (plan §4.5).
- **Decision**: PENDING

### F8 — SSE lessons.md rules not echoed; future regression risk

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Pattern Consistency
- **Location**: Plan §4.3, §4.9 (plan.md:336–345, 402–413)
- **Detail**: lessons.md items 1 & 2 require `json.Marshal` (no trailing newline) for SSE emission and `bufio.Reader.ReadBytes('\n')` (not Scanner) for SSE reading. IPC server and client are new SSE code; without naming these constraints in the plan, a future contributor reaching for json.NewEncoder or bufio.Scanner will reintroduce the bug classes that lessons.md was written to prevent.
- **Fix**: Add to Plan §4.3 and §4.9 contracts: "SSE emission MUST use json.Marshal (no trailing newline) per lessons.md §1; SSE reading MUST use bufio.Reader.ReadBytes per lessons.md §2." Add test assertions.
  - Strength: makes lessons-load-bearing on new code.
  - Tradeoff: more plan text.
  - Confidence: HIGH.
  - Blind spot: future lessons entries might be missed if not added similarly.
- **Decision**: PENDING

### F9 — Plan §Progress is out of sync with reality

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: plan.md:478–489 (Phase 1 rows)
- **Detail**: Boxes 1.1, 1.2, 1.3 are `- [x]` but Phase 1 code is uncommitted in working tree (model.go diff only). Convention at plan.md:476 says append ` — <sha>` when a step lands; no sha appears. This will confuse /10x-implement when picking up from this state.
- **Fix**: Either revert 1.1/1.2/1.3 to `- [ ]` (since uncommitted) or commit Phase 1 diff first, then run /10x-implement which will append the SHA per the convention.
- **Decision**: PENDING

### F10 — Multiple minor plan hygiene gaps

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: plan.md:135, plan.md:140, plan.md:251–264, plan.md:388–394
- **Detail** (4 items consolidated):
  - `os.Interrupt` + `syscall.SIGINT` is redundant (os.Interrupt IS SIGINT) — drop syscall.SIGINT.
  - `//go:build unix` is not a standard Go constraint — use `//go:build !windows` (matches bubbletea's tty_unix.go).
  - Subcommand dispatch for stop/status/attach bypasses `--help` short-circuit at main.go:74–84 — `freedius stop --help` won't show help.
  - Re-exec env propagation: must use `cmd.Env = os.Environ()` or leave default — FREEDIUS_PORT, FREEDIUS_LOG, provider API keys otherwise lost.
- **Fix**: Address all four as plan amendments before Phase 2 implementation. Each is a one-line edit.
- **Decision**: PENDING

### F11 — Signal/Socket/PID path resolution needs single helper

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Architecture
- **Location**: Plan §3.5 (plan.md:245), §4.5 (plan.md:370)
- **Detail**: §3.5 says PID path falls back to `$TMPDIR`; §4.5 says socket does the same. If two `pidFilePath()` / `socketPath()` functions diverge (one uses XDG, the other TMPDIR), `freedius attach` silently breaks (PID points to one place, socket to another).
- **Fix**: Extract one helper: `func runtimeDir() string { if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" { return d }; return os.TempDir() }`. Use in both files. Note: `os.TempDir()` returns `$TMPDIR` if set, `/tmp` otherwise — portable choice. research.md:128 fallback "/tmp/freedius.pid" is `os.TempDir()` on Linux; clarify.
  - Strength: single source of truth for path resolution.
  - Tradeoff: small refactor before implementing Phases 3/4.
  - Confidence: HIGH.
  - Blind spot: any test that hardcodes paths would need updating.
- **Decision**: PENDING

## Notes

Phase 1 7-line diff (uncommitted) was reviewed in detail and found to be correct and idiomatic. The bulk of the findings concern Phases 2–4 which are not yet implemented — these are plan-level risks that surface before code is written. Capping the warning count at 8 and folding F1/F7/F13/F17/F18 from the agent report into F10 keeps the surface focused on the highest-impact decisions.
