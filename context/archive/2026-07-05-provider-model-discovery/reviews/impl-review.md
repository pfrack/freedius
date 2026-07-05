<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider Model Discovery UI

- **Plan**: context/changes/provider-model-discovery/plan.md
- **Scope**: Phase 1–3 of 3 (full review)
- **Date**: 2026-07-05
- **Verdict**: APPROVED
- **Findings**: 0 critical (in scope) 2 warnings 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Dead code in error handling branch

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:682
- **Detail**: `if len(models) > 0` in the error branch can never be true. `FetchModels` returns `(nil, error)` on all error paths — it never returns both non-nil models and a non-nil error. The `"Refresh failed: %v"` formatting branch is unreachable.
- **Fix**: Remove the `if len(models) > 0` branch; simplify to `data.Error = fetchErr.Error()`. Or document the defensive guard if retained for future-proofing.
- **Decision**: FIXED — removed unreachable branch

### F2 — Loose test assertion in TestRefreshModels_UpstreamError

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/web/handlers_models_test.go:180-183
- **Detail**: Test checks `!strings.Contains(body, "fetch models") && !strings.Contains(body, "error") && !strings.Contains(body, "Error")` — this would pass for nearly any response body containing the word "error" in any context.
- **Fix**: Assert more specifically on the expected error content, e.g., check for the actual upstream error message or the `form-error` CSS class.
- **Decision**: FIXED — assert on form-error class and connection refused

### F3 — Template duplication between page and fragment

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: templates/providers-table.html vs templates/providers.html:9-49
- **Detail**: The table fragment templates duplicate the `<table>` markup from the full page templates. Any future table structure change must be updated in both files.
- **Fix**: Accept as conscious trade-off for HTMX fragment independence (the existing codebase already has this pattern for logs).
- **Decision**: SKIPPED — accepted as trade-off for HTMX fragment independence

### F4 — CSS `.text-muted` color hard-coded

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.css:225
- **Detail**: `.text-muted` uses `color: #888` which doesn't adapt to the dark theme. Other elements use CSS variables.
- **Fix**: Use `color: var(--text)` with reduced opacity, or define a CSS variable for muted text color.
- **Decision**: FIXED — use var(--text) with opacity

### F5 — Pre-existing: handleCreateProvider renders wrong table (not in scope)

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — pre-existing, not introduced by this change
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:383
- **Detail**: `handleCreateProvider` renders `renderMappingsTable` for HTMX responses, but should render `renderProvidersTable`. This bug existed before this change (verified at commit 09819d0). Not a regression.
- **Fix**: Out of scope — pre-existing bug. Change `renderMappingsTable` to `renderProvidersTable` on line 383 in a separate fix.
- **Decision**: FIXED — changed to renderProvidersTable

### F6 — Pre-existing: Recursive RLock risk (not in scope)

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — pre-existing, not introduced by this change
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:381-383, 487-490, 539-542, 590-593, 634-637
- **Detail**: HTMX branches acquire `cfg.RLock()` then call `render*Table` which calls `Snapshot()` which acquires another `RLock`. Go's `sync.RWMutex` prohibits recursive read-locking. This pattern existed before this change. Not a regression.
- **Fix**: Out of scope — pre-existing pattern. Remove outer RLock in HTMX branches in a separate fix.
- **Decision**: FIXED — removed outer RLock from all HTMX branches

### F7 — No rate limiting on model refresh

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — acceptable for a local tool
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:646-694
- **Detail**: The POST refresh endpoint makes a synchronous upstream HTTP request with no rate limiting. Rapid clicks could flood the upstream.
- **Fix**: Acceptable for a local tool. Could add a per-provider cooldown in ModelsCache if needed later.
- **Decision**: SKIPPED — acceptable for a local tool
