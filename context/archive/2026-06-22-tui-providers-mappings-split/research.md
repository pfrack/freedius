---
date: 2026-06-22T12:00:00+02:00
researcher: opencode
git_commit: c6f94fde7aec9597c5995ae305af1043956e5ece
branch: daemon-mode
repository: freedius
topic: "Split TUI Config tab into separate Providers and Mappings tabs"
tags: [research, codebase, tui, providers, mappings, modal, keybinding, test]
status: complete
last_updated: 2026-06-22
last_updated_by: opencode
---

# Research: Split TUI Config tab into Providers + Mappings tabs

**Date**: 2026-06-22T12:00:00+02:00
**Researcher**: opencode
**Git Commit**: c6f94fde7aec9597c5995ae305af1043956e5ece
**Branch**: daemon-mode
**Repository**: freedius

## Research Question

Configuration of providers should be in modal for providers and configuration of mappings should be only in mappings. Scope: UI reorg only — no backend/config schema changes.

## Summary

The current TUI has 3 tabs: **Log**, **Providers** (read-only table), and **Config** (mixed list of providers + mappings, supports inline-body-swap form editing). The Config tab conflates both data types — edit, add, and delete for both providers and mappings share a single `configCursor`, a single `formMode` dispatch, and a single `renderConfigTab` view.

The desired state is:
- **Providers tab** becomes editable: pressing Enter opens the existing provider form as an **overlay modal** (like the help modal pattern already established in `overlayModal`/`renderHelpModal`)
- **Mappings tab** replaces the old Config tab: shows only mappings, editing stays as inline body swap (the current form system, but with providers removed)
- 3 tabs total: Log, Providers (with modal), Mappings (with inline forms)
- No backend changes to `config.Config`, `config.Provider`, or `config.Mapping` structs

The refactor touches ~80+ lines across 4 files (`model.go`, `views.go`, `styles.go`, `help.go`) and requires modification or rewriting of ~25 test functions in `model_test.go`. The overlay modal pattern already exists for Help and can be reused. The form system can stay with only the mapping modes; provider forms move into the modal path.

## Detailed Findings

### 1. Current Tab Architecture

**Tab constants** (`proxy/tui/styles.go:199-203`):
```go
const (
    tabLog       = 0
    tabProviders = 1  // read-only provider table
    tabConfig    = 2  // mixed providers + mappings list
)
```

**Tab roles today**:
| Tab | Shortcut | Edit? | Add? | Delete? | Render function |
|-----|----------|-------|------|---------|----------------|
| Log (0) | F1→Providers, F2→Config | No | No | No | `renderLogTab` |
| Providers (1) | F1 | No | No | No | `renderProvidersTab` (table view) |
| Config (2) | F2 | `e`/Enter | `p`=prov, `a`=map | `d` | `renderConfigTab` (mixed list) |

All edit/add/delete keys are gated on `d.activeTab == tabConfig` at `proxy/tui/model.go:346-371`.

### 2. The Overlay Modal Pattern (Existing Infrastructure)

The help screen already uses a true overlay modal that can be reused as the provider editing surface:

- **State field**: `d.showHelp bool` (`proxy/tui/model.go:105`)
- **Key capture**: When `showHelp==true`, ONLY `?` and `esc` are processed (`proxy/tui/model.go:236-242`); all other keys consumed
- **Rendering**: `renderHelpModal` builds a bordered box; `overlayModal` centers it via `lipgloss.Place` (`proxy/tui/views.go:403-409`)
- **Core render flow** in `View()` (`proxy/tui/model.go:664-667`):
  ```go
  if d.showHelp {
      modal := renderHelpModal(width, d.styles)
      result = overlayModal(result, modal, width, height, d.styles.OverlayBgStyle)
  }
  ```
- **Styles already exist**: `ModalStyle` (rounded border, accent color), `ModalTitleStyle` (bold), `ModalFooterStyle` (faint italic), `OverlayBgStyle` (`proxy/tui/styles.go:173-191`)

To add a provider editing modal, add a new state field (e.g., `showProviderModal bool`), reuse the existing `formFields`, `formFocus`, `fieldErrors`, `showPicker`, and `picker` state, and write a `renderProviderEditModal` function modeled on `renderHelpModal` but embedding the form fields. The `esc` key in the modal would call `d.resetForm()` and set `showProviderModal = false`.

### 3. Form System Coupling

**Form modes** (`proxy/tui/styles.go:205-211`):
```go
const (
    formNone          = 0
    formEditProvider  = 1
    formAddProvider   = 2
    formEditMapping   = 3
    formAddMapping    = 4
    formDeleteConfirm = 5
)
```

The form system is deeply coupled to the shared Config tab. Here's the full lifecycle:

#### 3a. Opening forms — `openEditForm()` (`proxy/tui/model.go:718-755`)

Uses `collectAllEntries(d.config)` to get a merged providers+mappings list, then uses `d.configCursor` (a single cursor shared across both types) to identify the target entry. Branches on `entry.kind` to create 5-field provider forms or 3-field mapping forms. After the split, `openEditForm` would need to be split into two:
- `openEditMappingForm()` — cursor into mappings-only list (on Mappings tab)
- `openEditProviderFormModal()` — looks up provider by name from Providers tab cursor, opens as modal

#### 3b. Validation — `validateForm()` (`proxy/tui/model.go:811-857`)

Common validation (name non-empty, no CR/LF/colon) applies to all modes. Provider-specific: behavior enum check, URL format check, api_key_env character check. Mapping-specific: provider existence cross-reference check, model non-empty. After split, the provider validations would run in the modal path; mapping validations stay in the inline form.

#### 3c. Submission — `submitForm()` (`proxy/tui/model.go:860-928`)

All 4 form modes are dispatched in a single switch. Provider modes write to `d.config.Providers[name]`; mapping modes write to `d.config.Mappings[name]`. Save-to-disk with rollback on failure. Both use `d.config.Lock()`/`Unlock()`. The submit function can remain unified as long as `formMode` is set correctly (modal path would set `formEditProvider`/`formAddProvider`).

#### 3d. Form field labels — `fieldLabelsForMode()` (`proxy/tui/views.go:314-333`)

Returns 5 labels for provider modes, 3 for mapping modes. Used by `providerSelectedMsg` handler and `renderForm`. Must stay but provider modes would only be entered from the modal path.

### 4. Picker Integration

The picker system has two picker types:

| Picker | Built by | Triggered when | Used for |
|--------|----------|----------------|----------|
| Provider picker | `newProviderPicker()` | Enter on "provider" field during `formAddMapping` | Selecting a configured provider for a new mapping |
| Behavior picker | `newBehaviorPicker()` | Enter on "behavior" field during `formAddProvider`/`formEditProvider` | Selecting openai/anthropic/mix |

The picker renders as an overlay **within the form view** (not a separate modal). The `providerSelectedMsg` handler (`proxy/tui/model.go:214-228`) writes the selected value into the focused form field. This flow works regardless of whether the form is inline or modal, as long as `formMode` and `showPicker` are properly set.

**Key observation**: The behavior picker must still work inside the modal. Since the modal reuses the same `formFields`, `formFocus`, and `showPicker` state, the `handleFormKeyPress` path for Enter-on-behavior-field would fire correctly if the modal key handler delegates to the existing form key handler.

### 5. Keybinding Redistribution

Current tab-conditional keys at `proxy/tui/model.go:313-389`:

| Key | Current guard | After change |
|-----|--------------|--------------|
| `e`/Enter | `tabConfig` | `tabProviders` → open edit modal; `tabMappings` → open edit form |
| `a` | `tabConfig` | `tabMappings` → `openAddMappingForm()` (unchanged) |
| `p` | `tabConfig` | `tabProviders` → `openAddProviderFormModal()` |
| `d` | `tabConfig` | `tabProviders` → delete provider; `tabMappings` → delete mapping |
| `Ctrl+S` | `tabConfig` only | Move to `tabMappings` or keep on both |

Tab cycling uses `% 3` (`proxy/tui/model.go:335, 338`). Since the total stays at 3 tabs, modulo stays unchanged.

### 6. Scroll and Cursor Changes

**Current scroll model per tab**:
| Tab | Field | Type | Bound |
|-----|-------|------|-------|
| Log | `logScroll` | scroll-offset | `len(logBuffer.all())` |
| Providers | `providerScroll` | scroll-offset | `len(collectProvidersFromConfig(...))` |
| Config | `configCursor` | cursor | `len(collectAllEntries(...))` |

After the split:
- **Mappings tab**: needs a new cursor field (e.g., `mappingsCursor int`) for cursor-based navigation, bounded by `len(d.config.MappingsSnapshot())`
- **Providers tab**: the existing `providerScroll` scroll-offset is adequate for a table view. If the providers tab gains keyboard navigation (Enter to edit, d to delete), a `providerCursor int` field would be needed for selection

### 7. Mouse Click Handling

`handleConfigClick()` (`proxy/tui/model.go:462-480`) uses `collectAllEntries` and a hardcoded `entryOffset / 6` (6 lines per entry — the worst-case provider). After split, the Mappings tab always has 4 lines per entry, so the constant should change. A separate `handleProvidersClick()` would compute from the providers list instead.

The `approxEntryLines = 6` constant in `configVisibleWindow()` (`proxy/tui/views.go:126`) would become per-tab or be removed when each tab has its own scroll model.

### 8. Help Shortcut Text Changes

`proxy/tui/help.go` has 7 lines referencing "Config tab" that need updating:

| Line | Current | After |
|------|---------|-------|
| 11 | `Switch to Providers / Config tab` | `Switch to Providers / Mappings tab` |
| 16 | `Edit config entry (Config tab)` | Split descriptions per tab |
| 17 | `Add new mapping (Config tab)` | `Add new mapping (Mappings tab)` |
| 18 | `Add new provider (Config tab)` | `Add new provider (Providers tab)` |
| 19 | `Delete entry under cursor (Config tab)` | Split descriptions |
| 23 | `Install shell RC (Config tab)` | `Install shell RC (Mappings tab)` |
| 33 | `Edit config entry (Config tab)` | Split descriptions |

Also the tab label in `renderTabs()` (`proxy/tui/views.go:20`) must change from `"[3] Config (...)"` to `"[3] Mappings (j/k=scroll Enter=edit a=+map d=del)"`.

### 9. Test Impact Analysis

**`proxy/tui/model_test.go`** (1385 lines) has **32 lines** referencing `tabConfig` and **7 lines** referencing `tabProviders`.

#### Tests that survive a constant rename (mapping-only, semantics unchanged):

| Test | Line | Why |
|------|------|-----|
| `TestDashboard_OpenEditMappingForm` | 219 | Only tests mapping edit |
| `TestDashboard_OpenAddMappingForm` | 272 | Only tests mapping add |
| `TestDashboard_FormSubmitMappingUnknownProvider` | 399 | Only tests mapping validation |
| `TestDashboard_DeleteMapping` | 447 | Only tests mapping delete |
| `TestDashboard_SaveConfig` | 492 | Edits a mapping, verifies disk write |

#### Tests that need active-tab change (provider operations move to new tab):

| Test | Line | Change needed |
|------|------|---------------|
| `TestDashboard_OpenEditProviderForm` | 192 | `d.activeTab = tabProviders` instead of `tabConfig` |
| `TestDashboard_OpenAddProviderForm` | 253 | Same — `p` key now works on Providers tab |
| `TestDashboard_DeleteProvider` | 424 | `d` key now on Providers tab |
| `TestDashboard_FormSubmitProviderInvalidBehavior` | 373 | Opens provider form from Providers tab |
| `TestDashboard_AddProviderInsert` | 540 | Add provider from Providers tab |

#### Tests needing significant rewrite:

| Test | Line | Problem |
|------|------|---------|
| `TestDashboard_ConfigTabEnterEdits` | 942 | Cursor 0 is a provider; after split, Mappings tab has no providers. Must test Providers tab instead. |
| `TestDashboard_ConfigTabScrollsToCursor` | 964 | Uses `collectAllEntries` (merged list). Must test Mappings tab with only mappings. |
| `TestDashboard_MouseClickConfigEntry` | 1304 | Clicks on mixed entries. Split into mapping-click and provider-click tests. |
| Tab cycle tests | 67-115 | Hardcoded `% 3` and specific initial/want values. Survive if total stays 3. |

#### Tests that survive unchanged:

- All help modal tests (~15 lines)
- All log-tab tests (~20 lines)
- `TestDashboard_CtrlSOutsideConfigNoOp`
- `TestDashboard_LKeyInTabMode`, `TestDashboard_CycleLogLevel`, `TestDashboard_CycleTheme`
- All `picker_test.go` tests (124 lines, no tab references at all)
- Log rendering tests

#### No tests exist for:

- Picker integration: no test opens a picker and selects from it
- `renderForm`: no direct rendering test for form views
- `renderProvidersTab`: only tested indirectly via scroll test
- `collectAllEntries`: no direct unit test (used indirectly in `ConfigTabScrollsToCursor`)

**New tests recommended:**
1. `TestDashboard_ProvidersTabEnterOpensModal` — Enter on Providers tab opens provider edit modal
2. `TestDashboard_MappingsTabEnterEditsMapping` — Enter on Mappings tab opens mapping edit form
3. `TestDashboard_ProvidersModal_EscCloses` — Esc closes the provider edit modal
4. `TestDashboard_ProvidersModal_SubmitSaves` — Submitting the provider modal saves to config
5. `TestDashboard_ProvidersModal_DeleteConfirms` — d→y on Providers tab deletes provider
6. `TestRenderMappingsTab_OnlyMappings` — Rendering shows only mappings, no providers
7. `TestDashboard_MouseClickMappingsTabEntry` — Click on Mappings tab opens edit form for the clicked mapping

### 10. Files Unaffected

These files need **no changes**:
- `config/config.go` — Provider and Mapping structs stay the same
- `config/defaults.go` — no config logic changes
- `proxy/tui/picker.go` / `picker_test.go` — no tab references, no form mode references
- `proxy/tui/loglevel.go` — 0 tab references
- `cmd/freedius/main.go:229` — `NewDashboard()` call unchanged
- `cmd/freedius/attach.go:26` — `NewAttachDashboard()` call unchanged
- `proxy/mix.go`, `proxy/openai_compat.go`, `proxy/anthropic_compat.go` — no TUI integration

## Code References

### Current architecture (what exists):
- `proxy/tui/styles.go:199-203` — Tab constants (tabLog=0, tabProviders=1, tabConfig=2)
- `proxy/tui/styles.go:205-211` — Form mode constants
- `proxy/tui/styles.go:173-191` — Modal styles (already defined, reusable)
- `proxy/tui/model.go:97-116` — Dashboard struct (state fields)
- `proxy/tui/model.go:313-389` — `handleTabModeKeyPress` (key dispatch with tab guards)
- `proxy/tui/model.go:394-427` — `scrollUp`/`scrollDown` (per-tab scroll/cursor logic)
- `proxy/tui/model.go:456-480` — `handleMouseClick`/`handleConfigClick` (mixed-list click)
- `proxy/tui/model.go:619-673` — `View()` (render dispatch + help modal overlay)
- `proxy/tui/model.go:718-755` — `openEditForm()` (uses `collectAllEntries` + shared `configCursor`)
- `proxy/tui/model.go:757-797` — `openAddProviderForm()`, `openAddMappingForm()`
- `proxy/tui/model.go:811-857` — `validateForm()` (switches on all 4 form modes)
- `proxy/tui/model.go:860-928` — `submitForm()` (switches on all 4 form modes + delete)
- `proxy/tui/model.go:930-943` — `collectProviderFromForm()`, `collectMappingFromForm()`
- `proxy/tui/model.go:528-585` — `handleFormKeyPress` (picker activation on Enter)
- `proxy/tui/model.go:214-228` — `providerSelectedMsg` handler
- `proxy/tui/views.go:16-32` — `renderTabs()` (tab labels)
- `proxy/tui/views.go:77-123` — `renderProvidersTab()` (read-only table)
- `proxy/tui/views.go:153-201` — `renderConfigTab()` (mixed providers+mappings)
- `proxy/tui/views.go:260-300` — `configEntry`, `collectAllEntries`, `findEntryIndex`
- `proxy/tui/views.go:335-372` — `renderForm()` (inline body swap for all forms)
- `proxy/tui/views.go:385-410` — `renderHelpModal()` + `overlayModal()` (overlay modal pattern to reuse)
- `proxy/tui/help.go:8-35` — Help shortcut descriptions (7 lines mention "Config tab")
- `proxy/tui/picker.go:31-56` — `newProviderPicker()` (picker for mapping form provider field)
- `proxy/tui/picker.go:60-80` — `newBehaviorPicker()` (picker for provider behavior field)

### Test impact:
- `proxy/tui/model_test.go:192-590` — ~20 form tests (15 set `d.activeTab = tabConfig`)
- `proxy/tui/model_test.go:942-962` — `TestDashboard_ConfigTabEnterEdits` (needs rewrite)
- `proxy/tui/model_test.go:964-998` — `TestDashboard_ConfigTabScrollsToCursor` (needs rewrite)
- `proxy/tui/model_test.go:1304-1355` — Mouse click tests referencing `tabConfig` (4 tests)
- `proxy/tui/model_test.go:67-115` — Tab cycle tests (survive if 3 tabs)
- `proxy/tui/model_test.go:772-811` — Ctrl+S tests (vetting tab guard)
- `proxy/tui/picker_test.go` — No changes needed (0 tab/form references)

## Architecture Insights

1. **Inline body swap vs overlay modal**: Currently, `formMode != formNone` unconditionally swaps the entire body to `renderForm()` at `proxy/tui/model.go:638-640`. The overlay modal pattern for help (line 664-667) renders the tab body normally and centers the modal on top. The provider edit modal should follow the help pattern: render the Providers tab body, then overlay the form modal.

2. **Picker system is form-mode dependent**: The behavior picker fires when Enter is pressed on the "behavior" field, and the selected value is written back via `providerSelectedMsg`. This dependency on `formMode` and `fieldLabelsForMode` means the modal must still set `formMode` correctly (e.g., `formEditProvider`) for the picker to work inside the modal.

3. **Shared configCursor is the primary coupling**: The single `configCursor` indexes the merged providers+mappings list from `collectAllEntries`. Splitting tabs requires either a per-tab cursor or deriving cursor position from the tab's own data source. The simplest approach is adding a `mappingsCursor int` field for the Mappings tab and a `providerCursor int` for the Providers tab.

4. **No new `configEntry` types needed**: The existing `configEntry` struct (`proxy/tui/views.go:262-267`) works for both kinds. The Mappings tab would populate it with only mapping entries. The Providers tab doesn't need `configEntry` at all — it can use the existing `providerInfo` struct (`proxy/tui/views.go:228-233`).

5. **Form constants can stay**: `formEditProvider`, `formAddProvider`, `formEditMapping`, `formAddMapping` can all remain — the modal path sets provider modes, the Mappings tab inline path sets mapping modes. Only `formDeleteConfirm` potentially becomes redundant (deletion can happen directly in the key handler without entering form mode, since the tab context uniquely identifies the type).

6. **Tab count stays at 3**: Log=0, Providers=1, Mappings=2. This preserves the existing `% 3` modulo wrapping in tab cycling, simplifying the test migration.

## Historical Context

- `context/changes/add-popular-providers/` — Added 9 providers; TUI in that change already shows mixed Config tab
- `context/changes/improve-mixed-providers-config/research.md:80-86` — Noted that "TUI has no mix-specific UX" and "TUI Has No Mix-Specific UX (LOW priority)"
- `context/changes/improve-mixed-providers-config/research.md:164-170` — Recommendation 3: "Add protocol-aware TUI hints" (post-requisite to splitting provider UX surface)
- `context/foundation/lessons.md:21-31` — "Embrace Extra Tests": extra tests beyond plan are acceptable

## Related Research

- `context/changes/improve-mixed-providers-config/research.md` — Research into mix provider config gaps; noted TUI UX gaps in section 4
- `context/changes/missing-providers-tui/research.md` — Why popular providers were missing from TUI

## Open Questions

1. **Should `a` (add mapping) and `p` (add provider) share the same keybinding, or be differentiated by tab?** Current proposal: `p` on Providers tab adds a provider, `a` on Mappings tab adds a mapping. Alternative: `n` = "new" on both tabs (consistent but removes the mnemonic).

2. **Should Ctrl+S (shell RC install) stay on Mappings tab or move to a broader scope?** Currently gated on `tabConfig`. Could stay on Mappings tab or become accessible from both non-Log tabs.

3. **Should the delete confirmation (`d` then `y/n`) remain a form mode, or become a simpler message?** The current `formDeleteConfirm` mode renders a single-line confirmation. It could be simplified to set a `d.stats.message` with a prompt instead of entering a form mode.

4. **Should the provider modal title be dynamic?** e.g., "Edit Provider: openai" vs "Add New Provider". The existing pattern in `renderHelpModal` uses a static title. A dynamic title is a minor enhancement.

5. **Should the Mappings tab scroll be cursor-based (like current Config) or scroll-offset-based (like Providers/Log)?** Cursor-based enables Enter-to-edit at the highlighted entry. Scroll-offset is simpler but requires managing which entry is selected. Cursor-based is recommended for consistency with editing affordances.
