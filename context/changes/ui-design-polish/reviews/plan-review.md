<!-- PLAN-REVIEW-REPORT -->
# Plan Review: UI Design Polish

- **Plan**: `context/changes/ui-design-polish/plan.md`
- **Mode**: Deep
- **Date**: 2026-08-01
- **Verdict**: REVISE → SOUND (after triage fixes)
- **Findings**: 2 critical, 5 warnings, 3 observations (all triaged)

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| End-State Alignment | WARNING (FIXED via scope expansion) |
| Lean Execution | WARNING (FIXED — `--radius-card` dropped) |
| Architectural Fitness | PASS |
| Blind Spots | FAIL (FIXED via F1 + F2) |
| Plan Completeness | WARNING (FIXED via F3, F6, F7, F8, F9, F10) |

## Grounding

8/8 paths ✓, keyframe references ✓, `⟳` template locations ✓, drawer box-shadow ✓, `.route-step__label` bug ✓, model-filter-input rule ✓, brief↔plan phases consistent.

## Findings

### F1 — Log cap is targeted at the wrong file

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Blind Spots
- **Location**: Phase 4 §4 — Client-side log line cap
- **Detail**: Plan instructed editing `app.js`, but the SSE handler lives in `logs.html:69-89` (inside `{{define "scripts"}}`).
- **Fix A ⭐ Applied**: Move the contract to `logs.html`; cap inside both appendChild branches via shared trim. Explicit append → scroll → trim order.
- **Decision**: FIXED via Fix A

### F2 — Badge flag conversion targets dead CSS classes; live ones stay pills

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Blind Spots
- **Location**: Phase 5 §4 — Status badge style refinement
- **Detail**: `.badge--healthy` etc. are never used in templates; only `.badge--status-healthy` etc. appear (in `index.html`, `mapping-drawer.html`). After plan execution, dashboard status / activity-feed result / drawer status would remain pill-shaped.
- **Fix A ⭐ Applied**: Include `.badge--status-healthy/-degraded/-error/-unknown` in the same flag treatment (they share the base selector). Added manual verification step 5.7 covering dashboard routing-table, activity-feed, and drawer status.
- **Decision**: FIXED via Fix A

### F3 — `.table-wrap` inner `table` radius mismatch after Phase 3.1

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3 §1
- **Detail**: `app.css:652` uses `calc(var(--radius-2xl) - 0.25rem)`; would not track outer radius change.
- **Fix Applied**: Update contract to track the new outer radius.
- **Decision**: FIXED

### F4 — Plan promises "zero audit violations" but only closes some

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: End-State Alignment
- **Location**: Desired End State bullet 1
- **Detail**: Card hover translateY, remaining ALL-CAPS labels (`.section-count`, `.health-strip__label`, `.stats-strip__label`) not addressed.
- **Fix Applied**: Expanded scope — added card hover de-translate (Phase 3 §3) and extended label case reduction (Phase 2 §3) to cover remaining low-density labels.
- **Decision**: FIXED via scope expansion

### F5 — `--radius-card` token duplicates `--radius-xl`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Lean Execution
- **Location**: Phase 3 §1
- **Detail**: `--radius-card: 1.25rem` is identical to existing `--radius-xl: 1.25rem`.
- **Fix Applied**: Drop `--radius-card`. Reuse `--radius-xl` directly for `.card`, `.table-wrap`, `.log-container`. Use `--radius-md` for nested `.card--stat`.
- **Decision**: FIXED

### F6 — `.empty-state` radius not in Phase 3.1

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3 §1
- **Detail**: `.empty-state` uses `--radius-2xl` (page-level family), not in scope decision.
- **Fix Applied**: Added `.empty-state` to `--radius-2xl` group (with `.not-found`).
- **Decision**: FIXED (resolved by F5 fix)

### F7 — Phase 7 "Remaining audit items" vague

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Completeness
- **Location**: Phase 7 §1
- **Detail**: Three vague bullets with no pass/fail bar.
- **Fix Applied**: Replaced with four concrete pass/fail checks (token integrity grep, focus-visible spot-check, tr:hover spot-check, inline-style grep).
- **Decision**: FIXED via concrete checks

### F8 — `og:image` data URI needs percent-encoding + xmlns

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Plan Completeness
- **Location**: Phase 6 §2
- **Detail**: Raw `<`, `>`, `#`, `"` in `content` attribute truncates at first `"`; need percent-encoding and `xmlns`.
- **Fix Applied**: Added percent-encoding instruction and Twitter limitation note.
- **Decision**: FIXED

### F9 — Spinner `display: none` preservation not in contract

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Plan Completeness
- **Location**: Phase 4 §1
- **Detail**: Implementer could strip `display: none` thinking they're rewriting.
- **Fix Applied**: Added explicit "Preserve `display: none`" to contract.
- **Decision**: FIXED

### F10 — Log cap append/trim/scroll order unspecified

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Plan Completeness
- **Location**: Phase 4 §4
- **Detail**: Order between append, scroll, and trim not specified.
- **Fix Applied**: Specified append → scroll → trim order in contract.
- **Decision**: FIXED

## Triage Summary

```
═══════════════════════════════════════════════════════════
  TRIAGE COMPLETE
═══════════════════════════════════════════════════════════

  Fixed:     F1 (Fix A), F2 (Fix A), F3, F4, F5, F6, F7, F8, F9, F10   (10)
  Skipped:   —                                                            (0)
  Accepted:  —                                                            (0)
  Dismissed: —                                                            (0)

  ► Verdict after fixes: REVISE → SOUND
═══════════════════════════════════════════════════════════
```
