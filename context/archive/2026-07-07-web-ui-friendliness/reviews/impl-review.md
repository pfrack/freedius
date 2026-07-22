<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Web UI Friendliness Improvements

- **Plan**: context/changes/web-ui-friendliness/plan.md
- **Scope**: Phase 1, 2, 3 (all completed phases)
- **Date**: 2026-07-07
- **Verdict**: APPROVED
- **Findings**: 0 critical, 0 warnings, 5 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — LastResponder uses sync.Mutex+map instead of sync.Map

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/lastresponder.go
- **Detail**: Plan contract specified `sync.Map` for thread-safe provider→timestamp storage. Implementation uses `sync.Mutex` + `map[string]lastResponderEntry`. The `Snapshot()` method needs to iterate the full map, requiring a lock anyway — `sync.Map` would have been premature. Functionally equivalent.
- **Fix**: Accept as-is. The implementation is correct and the choice is well-reasoned.
- **Decision**: ACCEPTED-AS-RULE: sync.Mutex+map over sync.Map When Iteration Is Needed

### F2 — renderMappingsTable accepts Handlers struct instead of map parameter

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go
- **Detail**: Plan said "renderMappingsTable accepts optional lastResponders map". Actual: passes the full `*eventstream.Handlers` struct, which bundles both `Cfg` and `LastResponder`. Cleaner than adding a separate parameter.
- **Fix**: Accept as-is. The interface evolved in a reasonable direction.
- **Decision**: ACCEPTED-AS-RULE: Prefer Bundled Structs Over Scalar Parameters

### F3 — Nil-receiver guards on LastResponder not idiomatic

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/lastresponder.go:42-102
- **Detail**: All methods on `LastResponder` have nil-receiver guards (`if l == nil { return }`). No other type in the codebase (`EventBus`, `LogSink`, `Dispatcher`) uses this pattern. Can mask call-site bugs where a nil pointer is silently consumed instead of failing loudly.
- **Fix**: Remove nil-receiver guards and ensure callers always pass a valid pointer, consistent with codebase convention.
- **Decision**: FIXED (removed nil-receiver guards from all 4 methods)

### F4 — Fragile type assertion without comma-ok pattern

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:751
- **Detail**: Type assertion `mu.(*sync.Mutex)` uses the non-comma-ok form, which panics if the stored type is not `*sync.Mutex`. Only `&sync.Mutex{}` is ever stored, but the comma-ok form is the safer convention used elsewhere.
- **Fix**: Use the comma-ok form: `mu, ok := h.mu.(*sync.Mutex)` with a defensive check.
- **Decision**: FIXED (comma-ok form with typed `mtx` variable)

### F5 — Misleading comment on Responder field

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/types.go:68
- **Detail**: Comment says `Responder int // -1 when no recent responder known` but the zero value of `int` is `0`, not `-1`. The `HasResponder bool` field correctly distinguishes the case, so logic is correct — only the comment is misleading.
- **Fix**: Update comment to `// Responder index (check HasResponder for validity)`.
- **Decision**: FIXED (updated comment)
