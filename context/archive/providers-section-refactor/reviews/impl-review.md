<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider/Mapping Structural Refactor

- **Plan**: context/changes/providers-section-refactor/plan.md
- **Scope**: All Phases (1–4) of 4
- **Date**: 2026-06-20
- **Verdict**: NEEDS ATTENTION → RESOLVED (all findings fixed)
- **Findings**: 0 critical  3 warnings  5 observations

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

### F1 — Unbounded body read in EventBusMiddleware

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/proxy.go:514
- **Detail**: `extractModelFromBody` called `io.ReadAll(r.Body)` with no size limit, bypassing dispatcher's MaxBytesReader check.
- **Fix**: Wrapped in `io.LimitReader(r.Body, MaxBodyBytes)`.
- **Decision**: FIXED

### F2 — In-memory config mutated before Save

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality (Data Safety)
- **Location**: proxy/tui/model.go:311-319, 537-569
- **Detail**: `handleDeleteConfirmKeyPress` and `submitForm` mutated config maps before calling Save. If Save failed, in-memory and disk state diverged.
- **Fix A ⭐**: Roll back in-memory changes on Save failure.
- **Decision**: FIXED (via Fix A)

### F3 — Unplanned local token counting feature

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Scope Discipline
- **Location**: proxy/count_tokens_local.go
- **Detail**: Local BPE-based token counting via `translate.CountInputTokens` for providers without upstream support. Not in any plan phase.
- **Fix A ⭐**: Documented as addendum in plan.
- **Decision**: FIXED (via Fix A — addendum)

### F4 — Empty testhelpers_test.go

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: config/testhelpers_test.go
- **Detail**: File contained only `package config`, all helpers stripped, not deleted.
- **Fix**: Deleted the file.
- **Decision**: FIXED

### F5 — Dead supportsCountTokens function

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/capabilities.go:19
- **Detail**: One-line wrapper reading `provider.SupportsCountTokens`, only called from its own test. Dispatcher reads the field directly.
- **Fix**: Removed function and its tests.
- **Decision**: FIXED

### F6 — Dead AnthropicVersion populate logic

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: config/defaults.go:29-31
- **Detail**: `applyDefaults` checked if `p.AnthropicVersion == ""` and copied from generated defaults, but the generator never populated it — dead no-op.
- **Fix**: Removed the dead block.
- **Decision**: FIXED

### F7 — SupportsCountTokens static vs dynamic URL routing mismatch

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: config/defaults.go:30
- **Detail**: Static compile-time flag vs dynamic runtime URL sniffing diverge when user overrides a mix provider's base_url.
- **Fix**: Added comment documenting the constraint.
- **Decision**: FIXED

### F8 — Duplicate response header sets

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: proxy/proxy.go:206-214
- **Detail**: `X-Freedius-Matched-*` headers set twice for count-tokens requests passing through to upstream.
- **Fix**: Hoisted header sets before the conditional, removed duplicates.
- **Decision**: FIXED
