<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Web UI Modernization

- **Plan**: context/changes/web-ui-redesign/plan.md
- **Scope**: Phase 1 of 3, Phase 2 of 3, Phase 3 of 3 (full plan review)
- **Date**: 2026-07-05
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 3 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — TOCTOU race in delete-provider handler

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:454-490
- **Detail**: `handleDeleteProvider` reads under RLock to check if any mapping references the provider (lines 454-468), releases the read lock, then re-acquires a write lock to delete (line 471). Between the two lock acquisitions, a concurrent request could create a new mapping referencing the same provider, which would be orphaned by the delete. This is a pre-existing issue (not introduced by this change), but the file is in scope.
- **Fix**: Move the in-use check inside the write lock scope. Acquire `cfg.Lock()` at the top, check for in-use mappings, and only then proceed with delete (or unlock and return error). This matches the pattern already used in `handleDeleteMapping` (lines 607-614) which does the existence check and delete under one `cfg.Lock()`.
  - Strength: Eliminates the race window entirely; consistent with the sibling handler pattern.
  - Tradeoff: Holds the write lock longer during the in-use check — negligible for a local proxy with few concurrent users.
  - Confidence: HIGH — the pattern already exists in `handleDeleteMapping`.
  - Blind spot: None significant.
- **Decision**: FIXED — moved in-use check inside write lock scope

### F2 — Duplicate `levelLabel` function across packages

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/handlers.go:252-264 and internal/eventstream/handlers.go:222-233
- **Detail**: `levelLabel` is an identical function defined in both `proxy/web` and `internal/eventstream` packages. The copy in `proxy/web` was added by this change (used for server-side log rendering), while the one in `internal/eventstream` was pre-existing. This violates DRY and risks divergence if one copy is updated but not the other.
- **Fix**: Extract `levelLabel` into a shared location (e.g., `proxy/` package or a new file in `internal/`) and import it from both callers.
  - Strength: Single source of truth; eliminates divergence risk.
  - Tradeoff: Minor refactor — one new file or one existing file to share from.
  - Confidence: HIGH — straightforward extraction.
  - Blind spot: Need to check if `internal/eventstream` can import from `proxy/` (dependency direction).
- **Decision**: FIXED — exported LevelLabel from internal/eventstream, removed duplicate from proxy/web

### F3 — Inefficient log snapshot retrieval

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:99-114
- **Detail**: `logSink.SnapshotSince(0)` retrieves ALL entries from the ring buffer, then filters by level in Go, then caps to 200. For a large buffer (e.g., 10,000 entries), this loads everything into memory unnecessarily. The `?min=` filter should ideally be pushed into the snapshot call.
- **Fix**: Consider adding a `SnapshotFilteredSince(sinceSeq, minLevel, cap)` method to `LogSink`, or apply the cap before the level filter to reduce allocations. At minimum, this is acceptable for a local proxy with limited concurrent users.
  - Strength: Reduces memory allocation for large log buffers.
  - Tradeoff: Requires modifying the `LogSink` interface — slightly more invasive.
  - Confidence: MEDIUM — depends on whether `LogSink` API is in scope.
  - Blind spot: The ring buffer size is fixed, so the worst case is bounded.
- **Decision**: FIXED — iterate from end, cap at 200 before building slice

### F4 — Double-flush in SSE event emission

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: internal/eventstream/handlers.go:106-113, 153-165, 217-218
- **Detail**: `writeSSE()` flushes internally at line 218, but callers (`handleEvents`, `handleLogs`) also call `flusher.Flush()` after each `writeSSE` call. This causes two syscalls per SSE event instead of one. Not a correctness bug, but unnecessary overhead under load.
- **Fix**: Remove the flush from `writeSSE()` and let callers control flush timing, OR remove the explicit `flusher.Flush()` calls in `handleEvents`/`handleLogs`.
- **Decision**: FIXED — removed redundant flush from writeSSE, callers control flush timing

### F5 — No rate limiting on model fetch endpoint

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:643-687
- **Detail**: Each "Fetch models" button click triggers `proxy.FetchModels()` which makes an HTTP request to an upstream provider. There is no debounce, throttle, or in-flight deduplication. Rapid clicking could fire many concurrent upstream requests.
- **Fix**: Add a per-provider in-flight lock to prevent concurrent fetches for the same provider. Low priority for a local proxy.
- **Decision**: FIXED — added per-provider in-flight dedup with sync.Map + TryLock

### F6 — Legacy CSS button classes coexist with BEM

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.css:474-534
- **Detail**: The CSS has both new BEM-modified `.btn--primary`, `.btn--danger`, `.btn--ghost`, `.btn--sm` and legacy flat classes `.btn-primary`, `.btn-danger`, `.btn-cancel`, `.btn-sm`. The legacy classes duplicate properties from the BEM versions and are not used in the new templates (templates use `btn--primary`, `btn--danger`, etc.). The comment says "legacy classes (backward compat during transition)" — transition appears complete.
- **Fix**: Confirm no remaining callers of legacy classes, then remove them to avoid confusion.
- **Decision**: FIXED — removed unused legacy btn-primary, btn-danger, btn-sm classes; kept btn-cancel

### F7 — Sidebar header SVG icon (unplanned)

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: proxy/web/templates/layout.html (commit 1c5f3ff)
- **Detail**: A sidebar header SVG logo icon was added in a follow-up commit (1c5f3ff) that is not described in the plan. The icon is visually consistent with the nav-link SVGs and purely cosmetic.
- **Fix**: Document in the plan as an addendum, or accept as harmless scope addition.
- **Decision**: ACCEPTED — harmless cosmetic addition, will document as addendum
