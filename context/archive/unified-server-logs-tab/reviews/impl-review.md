<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Unified server-log tab + single binary entry point

- **Plan**: context/changes/unified-server-logs-tab/plan.md
- **Scope**: All phases (1–6) + post-implement fixes
- **Date**: 2026-06-20
- **Verdict**: NEEDS ATTENTION
- **Findings**: 3 critical 4 warnings 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️   (1 finding: -c shorthand missing) |
| Scope Discipline | PASS    ✅ |
| Safety & Quality | FAIL    ❌   (1 critical: config map race) |
| Architecture | PASS    ✅ |
| Pattern Consistency | WARNING ⚠️   (1 finding: Load path regression) |
| Success Criteria | PASS    ✅ |

## Findings

### F1 — Concurrent map read/write on config.Providers and config.Mappings

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:634-699, proxy/proxy.go:152,175

- **Detail**: The TUI mutates `d.config.Providers` and `d.config.Mappings` directly via `submitForm()` and `handleDeleteConfirmKeyPress()` (map `delete`, assignment, etc.) while `Dispatcher.ServeHTTP` reads the same maps on HTTP handler goroutines. Go maps are not safe for concurrent read+write — this will cause a `fatal error: concurrent map read and map write` at runtime. Note: this is pre-existing — the old runTUI/runServe split had the same race in the TUI path.

- **Fix A ⭐ Recommended**: Guard config maps with a `sync.RWMutex`. The dispatcher takes a read lock before map access; the TUI takes a write lock during form submission/deletion.
  - Strength: Language-standard, zero-allocation, proven pattern for shared mutable maps.
  - Tradeoff: Touches both `config.Config` (adding mutex) and `proxy.Dispatcher` (lock/unlock calls). The mutex must live in Config or be passed alongside it.
  - Confidence: HIGH — this is the canonical Go fix for concurrent map access.
  - Blind spot: Need to verify that no other code path reads/writes config concurrently.

- **Fix B**: Treat config as immutable and replace the `*config.Config` pointer atomically.
  - Strength: No locking at read time — reads are always lock-free.
  - Tradeoff: More invasive — every config mutation must build a new Config and atomically swap. Touches all save/edit/delete paths.
  - Confidence: MEDIUM — more lines changed, more surface for bugs.
  - Blind spot: Deep copy of config for each mutation is allocation-heavy.

### F2 — Config Save non-atomic write; mid-write corruption unrecoverable

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: config/config.go:255

- **Detail**: `Save()` does `os.WriteFile(path, data, 0o644)` directly. If the write fails mid-stream, the config file is truncated/partial. The backup restore at line 258 silently discards the rename error (`_ = os.Rename(...)`) and does nothing if the original was a new file (never backed up). Pre-existing issue, not introduced by this change.

- **Fix**: Write to `path + ".tmp"`, then `os.Rename(path+".tmp", path)`. If the original exists, the rename also serves as atomic replacement. Only keep the `.bak` for manual recovery.

### F3 — Server bind failure not surfaced to TUI

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: main.go:205-209

- **Detail**: `server.ListenAndServe()` runs in a goroutine. If it fails (port in use, permission denied), the error goes to `serverErr` but is only read AFTER the TUI exits. The TUI runs normally while the proxy is dead; the user gets no indication. Pre-existing — the old `tui.go` had the same pattern (lines 139-145).

- **Fix**: Observe the `serverErr` channel with a non-blocking `select` before starting the TUI program. On immediate bind failure, print the error and exit before the TUI starts. This handles the common case (port already in use) without changing the TUI protocol.

### F4 — YAML parse errors from file-based Load show `<bytes>` instead of file path

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: config/config.go:88-91

- **Detail**: `loadFromUnmarshaled` hardcodes `"<bytes>"` as the path argument to `yamlUnmarshalStrict`. Since `Load(path)` now delegates through this function, YAML syntax errors from file-based loads report `config: <bytes>: yaml: ...` rather than `config: /actual/path.yaml: yaml: ...`. This is new with the LoadFromBytes refactoring.

- **Fix**: Accept a `path` parameter in `loadFromUnmarshaled` and pass it through to `yamlUnmarshalStrict`. Minimal change — one extra parameter threaded through two call sites.

### F5 — Unsynchronized access to Dispatcher.VerboseErrors

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:331, proxy/proxy.go:309

- **Detail**: `toggleVerboseErrors()` writes `d.dispatcher.VerboseErrors` on the TUI goroutine while `writeErrorJSON` reads it on HTTP handler goroutines. No synchronization. While a `bool` write is typically atomic on x86-64, Go's race detector will flag it and the memory model provides no guarantee. New with Phase 5.

- **Fix**: Use `sync/atomic.Bool` for `Dispatcher.VerboseErrors`. This is Go 1.19+ compatible, zero-cost on modern hardware, and resolves the data race definitively. ~5 lines changed in proxy.go.

### F6 — ringBuffer.all() allocates full copy every TUI frame

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:53-63, proxy/tui/model.go:451

- **Detail**: `eventLog.all()` allocates a new `[]proxy.RequestEvent` slice and copies the entire ring buffer (up to 1000 entries) on every `View()` call (~60fps sustained). At 1000 events × ~200 bytes = 200KB/frame, ~12MB/s sustained allocation. Pre-existing — the ring buffer allocator was always present.

- **Fix**: Return a read-only window into the ring buffer directly: compute start/end indices, return a slice of the underlying array. Since `renderLogTab` is read-only, no copy is needed. Drop `ringBuffer.all()` in favor of a zero-alloc `ringBuffer.view()`.

### F7 — Ctrl+S "already installed" reported as failure

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:216-218

- **Detail**: `WriteShellRC` returns a non-nil error when the block is already installed (with `force=false`). `installShellRC` treats any non-nil error as failure and displays `"Shell install failed: already installed..."`. The actual condition is success/no-op. New with Phase 5.

- **Fix**: Check if the error message contains "already installed" and display `"Already installed ✓"` instead of `"Shell install failed: ..."`. Alternatively, refactor `WriteShellRC` to return a sentinel error that the caller can check with `errors.Is`.

### F8 — Missing `-c` shorthand for `--config`

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: main.go:84

- **Detail**: The plan specifies "Add `-c` shorthand for `--config` flag" (plan.md line 223). Implementation only registers `--config`. This is a plan deviation — the intent was to match the convenience flag pattern.

- **Fix**: `fs.String("c", "", "shorthand for --config")` alongside the existing `--config` registration. One extra line.

### F9 — NewDashboard accepts nil for all pointer params

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/model.go:106-131

- **Detail**: `NewDashboard` accepts nil for `cfg`, `reg`, and `dispatcher`. `NewDispatcher` and `NewRegistry` both panic on nil. Tests pass nil liberally. This masks bugs — a nil config will panic later in rendering. Inconsistent with codebase pattern.

- **Fix**: Add nil-guard panics for `cfg`, `reg`, and `dispatcher` (consistent with `NewDispatcher`/`NewRegistry`) or pass them explicitly non-nil in tests.

### F10 — extractModelFromBody silently swallows body on read error

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/proxy.go:515-519

- **Detail**: If body read fails (e.g., exceeds 10MB limit), the error is discarded and the dispatcher gets an empty body and `""` model. Returns misleading error message. Pre-existing — same code in original. Not touched by this change.

- **Fix**: Not in scope for this review — pre-existing and unchanged. Documented for awareness.
