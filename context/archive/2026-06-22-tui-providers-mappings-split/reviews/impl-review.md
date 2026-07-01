<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Split TUI Config Tab into Providers + Mappings Tabs

- **Plan**: context/changes/tui-providers-mappings-split/plan.md
- **Scope**: Phases 1-5 (full plan)
- **Date**: 2026-06-22
- **Verdict**: NEEDS ATTENTION
- **Findings**: 1 critical, 2 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Click handler uses inline form instead of modal

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/model.go:523
- **Detail**: `handleProvidersClick` calls `d.openEditProviderForm()` (inline body swap) instead of `d.openEditProviderFormModal()` (overlay modal). Since all phases were implemented in one pass, the click handler should use the modal path — Enter/e already do. This also means clicking a provider row shows an inline form rather than the modal, inconsistent with keyboard-triggered editing.
- **Fix**: Change `d.openEditProviderForm()` to `d.openEditProviderFormModal()` at model.go:523.
  - Strength: One-line change; makes click behavior match Enter/e behavior.
  - Tradeoff: None — modal path handles the same 6 fields with better UX.
  - Confidence: HIGH — identical pattern used at line 366 for Enter key.
  - Blind spot: None significant.
- **Decision**: FIXED

### F2 — Missing `TestDashboard_MouseClickProvidersTabEntry`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/model_test.go
- **Detail**: Plan specifies splitting `TestDashboard_MouseClickConfigEntry` into providers and mappings click tests. `TestDashboard_MouseClickMappingsTabEntry` exists but `TestDashboard_MouseClickProvidersTabEntry` was not added.
- **Fix**: Add a test that clicks on a provider row on the Providers tab and verifies the modal opens.
  - Strength: Completes the planned test coverage.
  - Tradeoff: Minor — a few lines of test code.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### F3 — Missing `TestDashboard_ProvidersModal_BehaviorPicker`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/model_test.go
- **Detail**: Plan lists `TestDashboard_ProvidersModal_BehaviorPicker` as a new test but it was not implemented.
- **Fix**: Add a test that opens the provider modal, tabs to the behavior field, presses Enter to open the picker, selects "mix", and verifies the field updates.
  - Strength: Validates picker-in-modal integration.
  - Tradeoff: Minor test code addition.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### F4 — Missing `TestDashboard_ProvidersModal_ProtocolPicker`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/model_test.go
- **Detail**: Plan lists `TestDashboard_ProvidersModal_ProtocolPicker` as a new test but it was not implemented.
- **Fix**: Add a test that opens the provider modal, tabs to the protocol field, presses Enter to open the picker, selects "anthropic", and verifies the field updates.
  - Strength: Validates protocol picker-in-modal integration.
  - Tradeoff: Minor test code addition.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### F5 — Nil config dereference in attach mode

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:384,391,444,451,491,498; proxy/tui/views.go:77,161
- **Detail**: `NewAttachDashboard` (line 182) leaves `d.config` as nil. Every code path that calls `collectProvidersFromConfig(d.config)` or `collectMappingEntries(d.config)` — delete, scroll-down, mouse-click, and render — will panic with nil pointer dereference in `ProvidersSnapshot()` or `MappingsSnapshot()`. The edit/add form paths are guarded by `detachOnQuit` checks, but the delete, scroll, mouse-click, and render paths are not. This is a pre-existing gap widened by this refactor (adding two new render tabs with config-dependent rendering).
- **Fix A ⭐ Recommended**: Supply a non-nil empty config to `NewAttachDashboard`:
  ```go
  d.config = &config.Config{}
  ```
  at the end of `NewAttachDashboard()`.
  - Strength: One-line fix; eliminates the entire class of crashes.
  - Tradeoff: Minimal — attach mode doesn't write config, so an empty config is harmless.
  - Confidence: HIGH — attach mode is read-only, empty config is safe.
  - Blind spot: Verify that no attach-mode path writes to config (it shouldn't — `detachOnQuit` blocks writes).
- **Fix B**: Guard every config-accessing path with `if d.config == nil { return }`.
  - Strength: Doesn't change initialization pattern.
  - Tradeoff: Many guard points to add; easy to miss one in future changes.
  - Confidence: MEDIUM — high touch surface, regression risk.
  - Blind spot: New config-accessing code in future changes still needs guards.
- **Decision**: FIXED

### F6 — Write lock held during file I/O

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:987,661
- **Detail**: `submitForm` and `handleDeleteConfirmKeyPress` acquire `d.config.Lock()` (exclusive write lock) and then call `d.config.Save()` which performs `os.Rename`, `os.WriteFile`, `os.MkdirAll`. Any concurrent read (dispatcher, render cycle, Snapshot call) blocks for the entire file I/O duration. Pre-existing issue, not introduced by this refactor.
- **Fix**: Copy relevant data under the lock, release the lock, then call Save outside the lock.
  - Strength: Eliminates TUI freeze during slow file I/O.
  - Tradeoff: Non-trivial refactor; the Save currently reads `d.config.Providers`/`d.config.Mappings` which are safe to snapshot.
  - Confidence: MEDIUM — the `ProvidersSnapshot()`/`MappingsSnapshot()` pattern already exists.
  - Blind spot: Ensures no concurrent modifications between release and Save.
- **Decision**: FIXED

### F7 — No-op division by 1 in index calculation

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/model.go:520
- **Detail**: `idx := entryOffset/1 + start` — the `/1` is always a no-op, likely a leftover from copying the `handleMappingsClick` pattern where `/4` maps to 4-line entries. Providers render at 1 line per entry, so `/1` is mathematically correct but stylistically a no-op.
- **Fix**: Simplify to `idx := entryOffset + start`.
  - Strength: Cleaner code.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### F8 — Stale comment references `collectAllEntries`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/views.go:275
- **Detail**: `findEntryIndex`'s doc comment says "survives changes to the sort order in `collectAllEntries`", but the function now calls `collectMappingEntries`.
- **Fix**: Update comment to reference `collectMappingEntries`.
  - Strength: Accurate documentation.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED
