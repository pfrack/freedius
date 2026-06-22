<!-- PLAN-REVIEW-REPORT -->
# Plan Review: Split TUI Config Tab into Providers + Mappings Tabs

- **Plan**: context/changes/tui-providers-mappings-split/plan.md
- **Mode**: Deep
- **Date**: 2026-06-22
- **Verdict**: SOUND
- **Findings**: 0 critical, 4 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| End-State Alignment | PASS |
| Lean Execution | PASS |
| Architectural Fitness | PASS |
| Blind Spots | PASS |
| Plan Completeness | PASS |

## Grounding

8/10 paths verified (tabConfig@styles.go:199-203, Dashboard@model.go:97-117, configVisibleWindow@views.go:125-151, renderProvidersTab@views.go:77-123, overlayModal@views.go:403-410, handleFormKeyPress@model.go:528-585, fieldLabelsForMode@views.go:314-333, renderForm@views.go:335-372); 1 path off-by-7-lines (renderHelpModal at 385-401, not 385-410); 1 detail off (collectProviderFromForm reads 4 fields not 5/6 — plan correctly addresses this with index 5 for protocol). All symbols confirmed. brief↔plan ✓ (6 decisions, scope, 5 phases, success criteria all aligned).

## Findings

### F1 — Modal layering: renderForm runs in modal state, duplicating picker rendering

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Architectural Fitness
- **Location**: Phase 4 — Step 5 (renderForm) and Step 6 (modal overlay)
- **Detail**: Phase 4 sets formMode = formEditProvider/formAddProvider when opening the modal. View() at model.go:638-639 then runs `if formMode != formNone { content = renderForm(...) }` — producing inline form fields and the picker. Step 6 then overlays `renderProviderEditModal`, which renders its own field list + picker on top. `overlayModal` (verified at views.go:403-410) discards the first argument via `_`, so visually only the modal is seen — but renderForm's work is wasted per frame, and Step 5's protocol picker addition to renderForm only matters if renderForm is allowed to run. The plan does not specify the relationship between renderForm and renderProviderEditModal in modal state.
- **Fix A ⭐ Recommended**: Skip renderForm when showProviderModal is true.
  - Strength: Clean separation — renderForm handles Mappings inline form only; renderProviderEditModal handles Providers modal only. Step 5 becomes dead code (can be removed). Mirrors how showHelp works in View() (model.go:643).
  - Tradeoff: One extra View() guard. Step 5 work is discarded.
  - Confidence: HIGH — verified showHelp pattern at model.go:643 already does similar conditional rendering.
  - Blind spot: None significant.
- **Fix B**: Have renderProviderEditModal delegate to renderForm for field iteration.
  - Strength: No duplicated field-iteration logic. Step 5 keeps renderForm up-to-date.
  - Tradeoff: Couples modal renderer to inline renderer; modal loses independent layout control. Diverges from how the existing help modal is structured.
  - Confidence: MEDIUM — works but establishes an inconsistent pattern.
  - Blind spot: Future modal changes need to keep two callers in sync.
- **Decision**: FIXED via Fix A — Step 5 (renderForm protocol picker rendering) deleted from Phase 4; Step 6 updated to gate `renderForm` on `!d.showProviderModal`. Steps 7-12 renumbered to 6-11.

### F2 — Phase 4 Step 10 contradiction: conditional vs unconditional reset

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 4 — Step 10 (Form submit from modal)
- **Detail**: Step 10 says "add this to `resetForm()` (conditionally, only if `showProviderModal` is true, to avoid side effects when `resetForm` is called from other contexts like Mappings form cancel)" then immediately states "**Contract**: `resetForm` should also set `d.showProviderModal = false` unconditionally — if `resetForm` is called, any modal should close." The two statements disagree. Unconditional is correct: showProviderModal is only ever true on Providers tab, and Mappings form cancel never sets it.
- **Fix**: Replace Step 10 with the unconditional contract and remove the "conditionally" wording.
- **Decision**: FIXED via F1 edit — "conditionally" wording removed; only the unconditional Contract remains. (Step 10 is now Step 9 after renumbering.)

### F3 — providerScroll test references not enumerated in Phase 5

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Completeness
- **Location**: Phase 5 — Tests
- **Detail**: providerScroll is referenced in 8 test lines: model_test.go:1044, 1045, 1056 (TestDashboard_ProvidersTabScroll, lines 1001-1063) and 1223, 1224, 1231, 1233, 1234 (providers subtests of TestDashboard_MouseWheelScroll, lines 1219-1236). These tests explicitly assert that providerScroll increments on k and decrements on j — behavior Phase 3 removes entirely. Phase 5 says "Update ~25 existing tests" generically but does not name these. An implementer running Phase 3 first will see 8 specific test failures and have to discover which tests to rewrite.
- **Fix**: Add to Phase 5 step 4 (Tests needing significant rewrite):
  - TestDashboard_ProvidersTabScroll (line 1001) — rewrite to use providerCursor instead of providerScroll
  - TestDashboard_MouseWheelScroll providers subtest (line 1219) — same rewrite
- **Decision**: FIXED — two rows added to Phase 5 step 4 table naming both tests with line ranges and the providerScroll → providerCursor rewrite.

### F4 — Phase 1 Overview contradicts Changes Required (no behavioral change vs help text/labels)

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 1 — Overview
- **Detail**: Phase 1 Overview says "No behavioral change to any tab yet — just renaming constants and adding state." But Phase 1 Changes Required #4 (Help text update) and #5 (Tab label update) change user-visible text. Help modal descriptions and tab labels are observable behavior.
- **Fix**: Rephrase Overview to "No tab navigation or editing behavior changes — only renaming constants, adding cursor state, and refreshing help text + tab labels."
- **Decision**: FIXED — Overview rephrased.

### F5 — renderHelpModal line range wrong

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Critical Implementation Details (intro section)
- **Detail**: Plan says "Help overlay pattern (`overlayModal` + `renderHelpModal` at `proxy/tui/views.go:385-410`)" but renderHelpModal is actually at lines 385-401. The 402-410 range is the overlayModal function. Could mislead the implementer when sizing the template.
- **Fix**: Change to `proxy/tui/views.go:385-401`.
- **Decision**: FIXED — line 13 (Key constraints) and line 563 (References) both updated to 385-401.

### F6 — Ctrl+S preservation mechanism not made explicit

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Blind Spots
- **Location**: Phase 2 — Manual Verification
- **Detail**: Phase 2 manual verification says "Ctrl+S installs shell RC from Mappings tab" but no Phase 2 change item touches Ctrl+S. The actual mechanism is in Phase 1's constant rename: the Ctrl+S handler at model.go:378-383 guards on `d.activeTab != tabConfig`, and Phase 1 renames that to tabMappings — which automatically makes Ctrl+S work on the new tab. Plan's framing of Phase 2 as "preserving Ctrl+S" is misleading because the work happens in Phase 1.
- **Fix**: Add an Implementation Note to Phase 1's Step 3 (or to Phase 2's overview) clarifying that the Phase 1 rename at model.go:379 is what enables Ctrl+S on Mappings tab.
- **Decision**: FIXED — Implementation Note added to Phase 1 explaining the Ctrl+S implicit carry-over via the rename.

### F7 — Phase 4 Step 8 step 5 wording is misleading

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 4 — Step 8 (Modal Esc key handling)
- **Detail**: Step 8 says "All other keys are consumed (no-op) — prevents background tab content from scrolling." This is wrong: in the Update() flow, non-special keys are routed through handleFormKeyPress (model.go:579-583) which types them into the focused form field. The "no-op" framing could mislead the implementer into writing a key sink that breaks text input.
- **Fix**: Rephrase: "All other keys are routed through handleFormKeyPress (which types into the focused form field) — preventing background tab content from scrolling while still allowing text entry."
- **Decision**: FIXED via F1 edit — Step 7 item 5 rephrased to clarify keys route through handleFormKeyPress (typing into focused field), not a no-op. (Step 8 is now Step 7 after renumbering.)