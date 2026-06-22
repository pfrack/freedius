# Split TUI Config Tab into Providers + Mappings Tabs — Implementation Plan

## Overview

Decompose the current `tabConfig` (which conflates providers and mappings in a single list) into two distinct tab surfaces: the **Providers tab** becomes editable via a cursor-based selection model with an overlay modal for editing (reusing the existing Help modal pattern), and the **Mappings tab** replaces Config with mapping-only content using the existing inline form system. The 6-field provider modal includes the `protocol` field (`openai`/`anthropic`) — currently YAML-only in `Provider.Protocol` — so users can configure the wire protocol for `mix` providers without hand-editing config files.

## Current State Analysis

The TUI has 3 tabs: **Log** (0), **Providers** (1, read-only table), and **Config** (2, mixed providers+mappings list). All editing — add, edit, delete — happens on the Config tab via `formMode` inline body swap and a single `configCursor` that indexes into the merged `collectAllEntries` list.

Key constraints:
- `providerScroll` (scroll-offset) is the Providers tab's only state; it has no cursor.
- The Help overlay pattern (`overlayModal` + `renderHelpModal` at `proxy/tui/views.go:385-401`) is the direct template for the provider edit modal.
- The picker system works through `formMode` and `fieldLabelsForMode`, so it works inside a modal as long as `formMode` is set correctly.
- Tab count stays at 3, so the `% 3` modulo in tab cycling (`proxy/tui/model.go:335,338`) is preserved.
- The `Provider.Protocol` field (`config/config.go:48`) exists on the struct but has no UI surface; the current provider form has only 5 fields.

## Desired End State

After this plan is complete, the TUI has 3 tabs with clear separation:
- **Providers tab** (F1): cursor-based list of providers. `p` adds a provider, `Enter`/`e` opens an overlay modal for editing, `d` deletes, `j`/`k` or scroll wheel scrolls. Click on a row opens the edit modal.
- **Mappings tab** (F2, replaces Config): cursor-based list of mappings. `a` adds a mapping, `Enter`/`e` opens inline edit form, `d` deletes, `j`/`k` scrolls. Click on a row opens the inline edit form. `Ctrl+S` installs shell RC.
- **Log tab** (Esc or cycle): unchanged.

The provider edit modal renders 6 fields (`name`, `behavior`, `base_url`, `api_key_env`, `anthropic_version`, `protocol`) in a 40-60 column overlay bordered modal, with behavior and protocol pickers available via Enter. The behavior picker fires for both add and edit provider forms. The protocol picker fires for the protocol field (valid values: `""`, `"openai"`, `"anthropic"`).

### Key Discoveries:

- `overlayModal` ignores its first argument (`_`) and uses only the `modal` string, placed centered with a whitespace background (`proxy/tui/views.go:403-409`)
- `configVisibleWindow` uses `approxEntryLines = 6` (`proxy/tui/views.go:126`) — providers render at 1 line each in the table; mappings render at 4 lines each. A per-tab entry-line constant is needed for correct click-to-cursor math.
- `fieldLabelsForMode` returns `nil` for `formDeleteConfirm` — the delete confirmation uses a separate render path (`renderDeleteConfirm`, `proxy/tui/views.go:374`) not inline form
- `detachOnQuit` guards block all form-opening paths (`proxy/tui/model.go:719,758,780`) — this applies to the modal path too
- The `protocol` field on `Provider` (`config/config.go:48`) is consumed by `MixAdapter` (`proxy/mix.go`) and normalized by `normalizeBaseURL` — the form field should mirror the same validation (empty, `"openai"`, `"anthropic"`)
- There are currently 5 provider fields in the form; adding `protocol` makes 6 total. The modal body height fits in ~14 rows (6 fields × 2 rows + error space + title + separator + footer)

## What We're NOT Doing

- **Backend/config schema changes** — `config.Provider` and `config.Mapping` structs stay exactly as-is. `config.Config`, `Load`, `Save`, `applyDefaults`, `validate` are not modified.
- **Generated file changes** — `providers_gen.go`, `adapters_gen.go` are unaffected.
- **Log tab changes** — Log tab is completely untouched.
- **New theme/style support** — No new styles needed; existing `ModalStyle`/`ModalTitleStyle`/`ModalFooterStyle`/`OverlayBgStyle` from `proxy/tui/styles.go` are reused.
- **Config hot-reload** — The TUI writes config on save but never re-reads the file. This is unchanged.
- **Protocol-first routing in MixAdapter** — The existing `Protocol` field and `MixAdapter.Handle` routing logic are already implemented; this plan only adds UI surface for the field.
- **New `configEntry` types** — `configEntry` struct (`proxy/tui/views.go:262-267`) stays; it works for both kinds.

## Implementation Approach

The change is decomposed into 5 phases, each independently testable:

1. **Foundation** — Rename `tabConfig` → `tabMappings`, add new cursor fields, update F-key mappings and help text. No behavioral change yet.
2. **Mappings tab** — Refactor `renderConfigTab` → `renderMappingsTab` (remove provider rendering branch). Swap `collectAllEntries` for `collectMappingEntries`. Update cursor/scroll wiring. The Mappings tab now shows only mappings.
3. **Providers tab** — Add `providerCursor` cursor-based navigation with active-row highlight. Wire `p`/`e`/`Enter`/`d` keybindings on Providers tab. Add `handleProvidersClick` for mouse support. The Providers tab now supports keyboard editing (opening the edit form inline, like Config did — modal overlay is Phase 4).
4. **Overlay modal** — Add `showProviderModal` state field. Write `renderProviderEditModal` (modeled on `renderHelpModal`). Wire overlay into `View()`. Add `Esc`-closes-modal key path. Migrate provider editing from inline body swap to modal. Add `protocol` field to the form (now 6 fields). Add protocol picker. Picker-in-modal works via existing `handleFormKeyPress` (delegates to picker when `showPicker` is true).
5. **Tests** — Update ~25 existing tests (constant renames + tab reassignment for provider ops). Rewrite 4 tests that depended on `collectAllEntries`. Add 5-7 new tests for modal flow and per-tab cursor navigation. Update help text tests.

## Critical Implementation Details

**Detach mode applies to modal too**: `openEditForm` and `openAddProviderForm`/`openAddMappingForm` all guard on `d.detachOnQuit` (`proxy/tui/model.go:719,758,780`). The new `openEditProviderFormModal()` must also guard on `d.detachOnQuit` — the IPC attach dashboard must not show editable forms.

**Modal must set `formMode` for picker to work**: The `handleFormKeyPress` dispatcher (`proxy/tui/model.go:528-585`) uses `d.formMode` to decide whether to show the behavior picker or the provider picker. The modal sets `formMode = formEditProvider` (or `formAddProvider`), so the picker path fires correctly. The `providerSelectedMsg` handler writes into the focused form field via `fieldLabelsForMode(d.formMode)` — this also works inside the modal because `formMode` is set.

**`approxEntryLines` per-tab constant**: The Mappings tab needs `approxEntryLines = 4` (header + provider_name + model_string + blank). The Providers tab renders 1-line rows (table format), so `approxEntryLines = 1`. Add `mappingEntryLines` and `providerEntryLines` constants and use them in `handleMappingsClick` and `handleProvidersClick` respectively. Do NOT reuse the existing `configVisibleWindow` with hardcoded `6` — instead parameterize it or create per-tab click handlers.

**Picker key dispatch order**: When `showPicker = true`, `handleFormKeyPress` delegates to `picker.Update(msg)` BEFORE processing Tab/Enter/Esc for the form itself (`proxy/tui/model.go:529-537`). This means the picker inside the modal works correctly — no special modal-key handling is needed for picker interactions. The modal's `Esc` path calls `d.resetForm()` which sets `showPicker = false`.

**protocol picker values**: The protocol picker should offer `""` (empty — auto-detect from URL), `"openai"`, and `"anthropic"`. The empty value means "fall back to URL path sniffing" (matches `Provider.Protocol` semantics). A new `newProtocolPicker(styles Styles)` function in `proxy/tui/picker.go` is needed, mirroring `newBehaviorPicker`.

**renderProviderEditModal field order**: `name`, `behavior`, `base_url`, `api_key_env`, `anthropic_version`, `protocol`. The `protocol` field is last because it's the least common edit — most users will never touch it. The field appears for all provider modes (not just mix) for simplicity, matching how `anthropic_version` appears even for non-anthropic providers.

## Phase 1: Foundation — Constants, Cursor State, Help Text

### Overview

Rename `tabConfig` → `tabMappings`, add `providerCursor` and `mappingsCursor` fields to `Dashboard`, update F2 key mapping, and refresh help text descriptions. No tab navigation or editing behavior changes — only renaming constants, adding cursor state, and refreshing help text + tab labels.

### Changes Required:

#### 1. Tab constants rename

**File**: `proxy/tui/styles.go:199-203`

**Intent**: Rename `tabConfig` to `tabMappings` and its value stays `2`. This is a pure rename — all references to `tabConfig` across the codebase must be updated. Do NOT change the numeric values (tabLog=0, tabProviders=1, tabMappings=2).

#### 2. Dashboard struct — add cursor fields

**File**: `proxy/tui/model.go:97-117`

**Intent**: Replace `configCursor int` (line 107) with `providerCursor int` and `mappingsCursor int`. The old `configCursor` is removed. The Mappings tab uses `mappingsCursor` as its cursor into the mappings-only list; the Providers tab uses `providerCursor` as its cursor into the providers table. Both use cursor-based navigation with `configVisibleWindow` (updated per-tab entry-lines constant in Phase 2/3).

#### 3. F2 key mapping

**File**: `proxy/tui/model.go:326-328`

**Intent**: Change `d.activeTab = tabConfig` to `d.activeTab = tabMappings`.

#### 4. Help text update

**File**: `proxy/tui/help.go:8-35`

**Intent**: Update 7 lines referencing "Config tab":
- Line 11: `"Switch to Providers / Mappings tab"`
- Line 16: `"Edit entry (Providers / Mappings tab)"`
- Line 17: `"Add new mapping (Mappings tab)"`
- Line 18: `"Add new provider (Providers tab)"`
- Line 19: `"Delete entry under cursor"`
- Line 23: `"Install shell RC (Mappings tab)"`
- Line 33: `"Click entry to edit"`

#### 5. Tab label update

**File**: `proxy/tui/views.go:16-32`

**Intent**: Update the `renderTabs` labels:
- Providers tab: `"[2] Providers (Enter=edit p=+prov d=del)"`
- Mappings tab (was Config): `"[3] Mappings (j/k=scroll Enter=edit a=+map d=del)"`

### Success Criteria:

#### Automated Verification:

- `go build ./cmd/freedius` passes
- `go test ./proxy/tui/...` passes (tests still reference old constant names — will break until Phase 5; for now, treat Phase 1 tests as temporarily expected failures — OR apply the constant rename first and tests compile with the new name, which is the preferred approach)
- `go vet ./...` passes

#### Manual Verification:

- TUI launches with 3 tabs; F1/F2/tab cycling work as before
- Help modal shows updated descriptions
- Providers tab is still read-only (no cursor, no edit — that's Phase 3)
- Mappings tab still renders mixed content (Phase 2 cleans that up)

**Implementation Note**: Apply constant rename across ALL files in a single commit to avoid split-brain compilation. The tab numeric values stay the same, so runtime behavior is identical.

**Implementation Note (Ctrl+S implicit carry-over)**: The `tabConfig` → `tabMappings` rename in Step 1 also updates the Ctrl+S handler's guard at `proxy/tui/model.go:378-383` (`if d.activeTab != tabConfig { return d, nil }`). With the rename, Ctrl+S works on the new Mappings tab automatically — no Phase 2 code change is needed. Phase 2 manual verification "Ctrl+S installs shell RC from Mappings tab" is satisfied entirely by this Phase 1 rename.

---

## Phase 2: Mappings Tab — Refactor renderConfigTab to Mappings-Only

### Overview

Rename `renderConfigTab` → `renderMappingsTab`, remove the provider rendering branch, swap `collectAllEntries` for `collectMappingEntries` (or rename to `collectMappingEntries`), update `configCursor` → `mappingsCursor` for this tab. The Mappings tab now shows only mapping entries.

### Changes Required:

#### 1. Rename and refactor renderConfigTab

**File**: `proxy/tui/views.go:153-201`

**Intent**: Rename `renderConfigTab` → `renderMappingsTab`. Remove the `entry.kind == "provider"` branch (lines 169-192) — only render mapping entries. Change title from "Configuration" to "Mappings". Use a new `collectMappingEntries` data source.

#### 2. collectMappingEntries function

**File**: `proxy/tui/views.go:269-288`

**Intent**: Add `collectMappingEntries(cfg *config.Config) []configEntry` that returns only mapping entries (populates `kind: "mapping"`, leaves `provider` zero-valued). Keep `collectAllEntries` in place for now — it's still used by `findEntryIndex` in tests and by `handleConfigClick` during the transition (will be removed in Phase 3). Alternatively, if `collectAllEntries` is no longer called from production code after this refactor, remove it and leave `collectMappingEntries`.

#### 3. findEntryIndex — update for mappings-only

**File**: `proxy/tui/views.go:290-300`

**Intent**: Update `findEntryIndex` to iterate `collectMappingEntries` instead of `collectAllEntries`. Since the function is only called from tests (`proxy/tui/model_test.go:519`), the signature stays: `findEntryIndex(cfg *config.Config, name, kind string) int`. With `kind` always being `"mapping"` in the new test context, the `kind` parameter becomes redundant but harmless — keep for backward compatibility or simplify to just name.

#### 4. configVisibleWindow — per-tab entry-lines constant

**File**: `proxy/tui/views.go:125-151`

**Intent**: Parameterize the `approxEntryLines = 6` constant. Add a parameter `entryLines int` to `configVisibleWindow` (or create `mappingVisibleWindow` and `providerVisibleWindow` helpers). The Mappings tab calls it with `mappingEntryLines = 4`; the Providers tab (Phase 3) calls it with `providerEntryLines = 1`. The existing `configVisibleWindow` signature becomes `configVisibleWindow(entries []configEntry, cursor, available, entryLines int) (start, end int)`.

#### 5. Scroll wiring — Mappings tab

**File**: `proxy/tui/model.go:394-427`

**Intent**: In `scrollUp`/`scrollDown`, the `case tabMappings` branch now uses `d.mappingsCursor` instead of `d.configCursor`. `scrollDown` ceiling uses `len(collectMappingEntries(d.config))` instead of `len(collectAllEntries(d.config))`.

#### 6. View() render dispatch

**File**: `proxy/tui/model.go:642-652`

**Intent**: Change `case tabConfig` to `case tabMappings` and call `renderMappingsTab(d.config, d.mappingsCursor, width, bodyHeight, d.styles)` instead of `renderConfigTab(...)`.

### Success Criteria:

#### Automated Verification:

- `go build ./cmd/freedius` passes
- `go test ./proxy/tui/...` passes (if constant rename was applied cleanly in Phase 1, tests referencing `tabMappings` compile)
- `go vet ./...` passes

#### Manual Verification:

- Mappings tab shows only mapping entries (no providers)
- `j`/`k` cursor navigation works on Mappings tab
- `a` adds a mapping; `Enter`/`e` edits focused mapping; `d` deletes
- `Ctrl+S` installs shell RC from Mappings tab
- Providers tab is still read-only table (Phase 3 makes it editable)

---

## Phase 3: Providers Tab — Cursor Navigation and Keyboard Editing

### Overview

Convert the Providers tab from read-only scroll-offset to cursor-based with a highlighted active row. Wire `p`/`e`/`Enter`/`d` keybindings for provider operations on this tab. Add `handleProvidersClick` for mouse support. Provider editing still opens inline via `formMode` body swap at this point — the overlay modal migration happens in Phase 4.

### Changes Required:

#### 1. Providers tab cursor and rendering

**File**: `proxy/tui/views.go:77-123`

**Intent**: Update `renderProvidersTab` to accept a `cursor int` parameter (replacing or supplementing `scroll int`). Render the active row with `ActiveTabStyle` highlight (matching the current `renderConfigTab` cursor highlight pattern). Add `providerEntryLines = 1` constant for the table rows. Replace the scroll-offset rendering logic with cursor-based `configVisibleWindow` using `providerEntryLines = 1`. The `providerScroll` field becomes unused and can be removed or left as dead code (to be cleaned up later).

#### 2. Providers tab keybindings

**File**: `proxy/tui/model.go:346-371`

**Intent**: Split the existing `tabConfig` guards into `tabProviders` and `tabMappings`:
- `e`/`Enter`: `tabProviders` → `d.openEditForm()` (opens inline form at this stage; will become modal in Phase 4); `tabMappings` → `d.openEditForm()` (opens inline mapping form)
- `a`: `tabMappings` → `d.openAddMappingForm()`
- `p`: `tabProviders` → `d.openAddProviderForm()`
- `d`: `tabProviders` → delete provider via `collectProvidersFromConfig` + `providerCursor`; `tabMappings` → delete mapping via `collectMappingEntries` + `mappingsCursor`

For the `d` key on Providers tab, use `collectProvidersFromConfig` (`proxy/tui/views.go:235-258`) to get the provider at `providerCursor`, then set `formKind = "provider"`, `formEntryName = entry.name`, `formMode = formDeleteConfirm`.

#### 3. openEditForm — per-tab routing

**File**: `proxy/tui/model.go:718-755`

**Intent**: Split `openEditForm` into `openEditProviderForm` and `openEditMappingForm`, each using their own cursor field and data source:
- `openEditProviderForm`: uses `collectProvidersFromConfig`, `providerCursor`, creates 6-field provider form (name, behavior, base_url, api_key_env, anthropic_version, protocol), sets `formMode = formEditProvider`
- `openEditMappingForm`: uses `collectMappingEntries`, `mappingsCursor`, creates 3-field mapping form, sets `formMode = formEditMapping`

The existing `openEditForm` becomes a thin dispatcher that checks `d.activeTab` and calls the appropriate sub-function.

#### 4. openAddProviderForm — protocol field

**File**: `proxy/tui/model.go:757-777`

**Intent**: Add the `protocol` field to `openAddProviderForm`. Currently creates 5 fields; must now create 6: `name`, `behavior`, `base_url`, `api_key_env`, `anthropic_version`, `protocol`. The protocol field placeholder is `"protocol (openai/anthropic)"`.

#### 5. collectProviderFromForm — protocol field

**File**: `proxy/tui/model.go:930-937`

**Intent**: Add `Protocol` to the returned `config.Provider`: `Protocol: strings.TrimSpace(d.formFields[5].Value())`. Index 5 corresponds to the 6th field (protocol).

#### 6. fieldLabelsForMode — add protocol label

**File**: `proxy/tui/views.go:314-333`

**Intent**: Add `"protocol"` as the 6th label in the `formEditProvider`/`formAddProvider` case: `return []string{"name", "behavior", "base_url", "api_key_env", "anthropic_version", "protocol"}`.

#### 7. validateForm — protocol validation

**File**: `proxy/tui/model.go:811-857`

**Intent**: Add validation for the protocol field in the `formEditProvider`/`formAddProvider` case: `protocol` must be one of `""`, `"openai"`, `"anthropic"`. If not, set `errs[5] = "protocol must be one of: openai, anthropic, or empty"`.

#### 8. handleProvidersClick

**File**: `proxy/tui/model.go:456-460`

**Intent**: Add a click handler for the Providers tab. When `d.activeTab == tabProviders`, compute the clicked row index using `configVisibleWindow` with `providerEntryLines = 1`, set `d.providerCursor = idx`, and call `d.openEditProviderForm()`. Mirror the `handleMappingsClick` pattern.

#### 9. handleMappingsClick (renamed from handleConfigClick)

**File**: `proxy/tui/model.go:462-480`

**Intent**: Rename `handleConfigClick` → `handleMappingsClick`. Update it to use `collectMappingEntries` and `d.mappingsCursor`. Use `mappingEntryLines = 4` for the click-to-cursor math. Update the call site in `handleMouseClick` (`proxy/tui/model.go:456`).

#### 10. scrollUp/scrollDown — Providers cursor

**File**: `proxy/tui/model.go:394-427`

**Intent**: Change `case tabProviders` from `providerScroll++`/`providerScroll--` to `providerCursor--`/`providerCursor++` with bounds checking against `len(collectProvidersFromConfig(d.config))`, mirroring the current `tabConfig` cursor pattern.

#### 11. View() Providers tab render

**File**: `proxy/tui/model.go:645-646`

**Intent**: Pass `d.providerCursor` to `renderProvidersTab` (add `cursor int` parameter). The function uses cursor to render the active row highlight.

### Success Criteria:

#### Automated Verification:

- `go build ./cmd/freedius` passes
- `go test ./proxy/tui/...` passes
- `go vet ./...` passes

#### Manual Verification:

- Providers tab shows a highlighted row that moves with `j`/`k`
- `p` adds a new provider (inline body swap form with 6 fields including protocol)
- `Enter`/`e` on a provider row opens inline edit form (same 6 fields)
- `d` on a provider row shows delete confirmation
- Click on a provider row opens inline edit form
- Protocol field accepts empty, `"openai"`, or `"anthropic"` and validates
- Mappings tab still works as before (Phase 2)

---

## Phase 4: Overlay Modal for Provider Editing

### Overview

Migrate provider editing from inline body swap (`formMode != formNone` → `renderForm`) to an overlay modal (like `renderHelpModal`). Add `showProviderModal` state. Write `renderProviderEditModal`. Wire the modal into `View()`. The modal renders 6 fields, supports behavior and protocol pickers, and closes on `Esc`. The Mappings tab continues using the existing inline form system.

### Changes Required:

#### 1. showProviderModal state field

**File**: `proxy/tui/model.go:97-117`

**Intent**: Add `showProviderModal bool` to the `Dashboard` struct. This field is set to `true` when the provider edit/add modal should be visible. It is independent from `formMode` — `formMode` can be `formEditProvider` or `formAddProvider` while `showProviderModal` is true.

#### 2. renderProviderEditModal function

**File**: `proxy/tui/views.go` (new function)

**Intent**: Write `renderProviderEditModal(terminalWidth int, d *Dashboard) string` that renders a bordered, centered modal containing the provider form fields. Model it on `renderHelpModal`:
- Title: `"Edit Provider: <name>"` (or `"Add New Provider"` for add mode)
- Body: iterate `fieldLabelsForMode(formMode)` and render each `d.formFields[i].View()` with `ConfigKeyStyle` labels
- If `showPicker && picker != nil` and the focused field label is `"behavior"` or `"protocol"`, replace that field's view with `picker.View()`
- Footer: `"Enter=Save  Esc=Cancel  Tab=Next Field"`
- Use `modalWidthFor(terminalWidth)` for width (same 40-60 column range as help modal)
- Return the rendered string

#### 3. Protocol picker

**File**: `proxy/tui/picker.go` (new function)

**Intent**: Add `newProtocolPicker(styles Styles) *providerPicker` that builds a fixed list of 3 items: `""` (empty), `"openai"`, `"anthropic"`. Follow the same pattern as `newBehaviorPicker`. The selected value is returned via `providerSelectedMsg` as usual.

#### 4. handleFormKeyPress — protocol picker trigger

**File**: `proxy/tui/model.go:528-585`

**Intent**: Add a new case in the Enter-key handler for the protocol field. When `fieldName == "protocol"` and `(formAddProvider || formEditProvider)`, open `newProtocolPicker(d.styles)`. This mirrors the behavior picker trigger pattern at lines 561-565.

#### 5. View() modal overlay

**File**: `proxy/tui/model.go:619-673`

**Intent**: In the `View()` method, gate `renderForm` on `!d.showProviderModal` so the inline form does not run when the modal is active (renderForm handles Mappings inline flow only; renderProviderEditModal handles Providers modal flow):
```
if d.formMode != formNone && !d.showProviderModal {
    content = renderForm(...)
}
```
Then, after the form-mode check and before the help overlay, add:
```
if d.showProviderModal {
    modal := renderProviderEditModal(width, d)
    result = overlayModal(result, modal, width, height, d.styles.OverlayBgStyle)
}
```
This renders the Providers tab body normally, then overlays the provider edit modal on top (the body content is ignored by `overlayModal`). With the gate above, `renderForm` does not produce wasted work for the discarded inline body.

#### 6. Modal lifecycle — open/close

**File**: `proxy/tui/model.go` (openEditProviderFormModal, closeProviderModal)

**Intent**: Write `openEditProviderFormModal()` that:
1. Guards on `d.detachOnQuit` (same as existing form-open functions)
2. Calls the existing `openEditProviderForm` logic (Phase 3) to populate `formFields`, `formMode`, `formKind`, `formEntryName`
3. Sets `d.showProviderModal = true`

Write `closeProviderModal()` that:
1. Calls `d.resetForm()` to clear all form state
2. Sets `d.showProviderModal = false`

Update the Providers tab `Enter`/`e` keybinding to call `openEditProviderFormModal()` instead of `openEditForm()`.

Update the Providers tab `p` keybinding to call `openAddProviderFormModal()` (a new function mirroring `openAddProviderForm` + `showProviderModal = true`).

#### 7. Modal Esc key handling

**File**: `proxy/tui/model.go:234-255`

**Intent**: In `Update`, when `d.showProviderModal` is true, the key dispatch order is:
1. If picker is active (`d.showPicker`), delegate to `handleFormKeyPress` (picker captures keys first)
2. If `esc` is pressed, call `closeProviderModal()` and return
3. If `tab`/`shift+tab` is pressed, delegate to `handleFormKeyPress` (field navigation inside modal)
4. If `enter` is pressed, delegate to `handleFormKeyPress` (submit or open picker)
5. All other keys are routed through `handleFormKeyPress` (which types into the focused form field) — preventing background tab content from scrolling while still allowing text entry

This mirrors the `showHelp` key-capture pattern (`proxy/tui/model.go:236-242`) but allows Tab/Enter/Esc through for form interaction.

#### 8. Delete confirmation in modal context

**File**: `proxy/tui/model.go`

**Intent**: When `showProviderModal` is true and the user presses `d` on the Providers tab, the modal must handle the delete confirmation path. Two approaches:
- **Option A (recommended)**: When `d` is pressed while `showProviderModal` is true, do nothing — `d` works only in tab mode, not modal mode. The user must close the modal first (Esc), then press `d` in tab mode. This is simpler and consistent with how `showHelp` captures keys.
- **Option B**: Allow `d` inside the modal to trigger delete confirmation on the currently edited provider. More complex, adds modal-specific delete handling.

**Recommendation**: Option A — keep `d` outside the modal. This is consistent with the help modal pattern.

#### 9. Form submit from modal

**File**: `proxy/tui/model.go:230-232`

**Intent**: `formSubmittedMsg` triggers `submitForm()` as before (`proxy/tui/model.go:860-928`). `submitForm` already handles `formEditProvider` and `formAddProvider` cases. After `resetForm()` is called inside `submitForm`, `showProviderModal` must also be set to `false`.

**Contract**: `resetForm` should also set `d.showProviderModal = false` unconditionally — if `resetForm` is called, any modal should close. (Unconditional is safe: `showProviderModal` is only ever true on the Providers tab; the Mappings form cancel path never sets it to true.)

#### 10. Mouse click — Providers tab in modal state

**File**: `proxy/tui/model.go:442-460`

**Intent**: When `d.showProviderModal` is true and a mouse click occurs, do nothing (same as `formMode != formNone` guard at line 450-451). Clicks are consumed when a modal/form is active.

#### 11. scrollUp/scrollDown — Providers tab in modal state

**File**: `proxy/tui/model.go:429-439`

**Intent**: When `d.showProviderModal` is true, scroll events in `handleMouseWheel` should be consumed (no-op), same as when `d.formMode != formNone` (line 430-431). Add `d.showProviderModal` check to the guard.

### Success Criteria:

#### Automated Verification:

- `go build ./cmd/freedius` passes
- `go test ./proxy/tui/...` passes
- `go vet ./...` passes

#### Manual Verification:

- `Enter`/`e` on Providers tab opens a centered, bordered modal overlay (not an inline body swap)
- Modal shows 6 fields: name, behavior, base_url, api_key_env, anthropic_version, protocol
- `Tab`/`Shift+Tab` navigates fields inside the modal
- Enter on "behavior" field opens behavior picker; Enter on "protocol" field opens protocol picker
- Esc closes the modal and returns to Providers tab view
- Submitting the modal (Enter with no errors) saves to config and closes modal
- Providers tab body is visible behind the modal (dimmed via OverlayBgStyle)
- `d` while modal is open does nothing (delete only works in tab mode)
- Mappings tab still uses inline form editing (not modal)
- Ctrl+S on Mappings tab still installs shell RC
- Detach mode (`NewAttachDashboard`) blocks modal from opening

---

## Phase 5: Tests — Update Existing and Add New

### Overview

Update ~25 existing test functions that reference `tabConfig` (now `tabMappings`) and the old `configCursor` field. Rewrite 4 tests that depend on `collectAllEntries` (now `collectMappingEntries`). Add 5-7 new tests for the overlay modal flow, per-tab cursor navigation, and the protocol field. Update help text tests.

### Changes Required:

#### 1. Constant rename in tests

**File**: `proxy/tui/model_test.go` (32 lines referencing `tabConfig`)

**Intent**: Replace all `tabConfig` references with `tabMappings`. For tests that exercise provider operations (edit provider, add provider, delete provider), change the active tab from `tabMappings` to `tabProviders`. Specifically:
- Tests testing mapping operations (edit mapping, add mapping, delete mapping, save config): `tabConfig` → `tabMappings`
- Tests testing provider operations (edit provider, add provider, delete provider): `tabConfig` → `tabProviders`
- Tab cycle tests: `tabConfig` → `tabMappings` in expected values

#### 2. Cursor field rename in tests

**File**: `proxy/tui/model_test.go`

**Intent**: Replace `d.configCursor = N` with `d.mappingsCursor = N` for mapping tests, and `d.providerCursor = N` for provider tests. The cursor index values may change because `collectMappingEntries` returns only mappings (different indices than the merged `collectAllEntries` list).

#### 3. findEntryIndex migration

**File**: `proxy/tui/model_test.go:519`

**Intent**: `findEntryIndex(cfg, "opus", "mapping")` now iterates `collectMappingEntries` (mappings-only list). The returned index is guaranteed to be correct for `mappingsCursor`. Verify this test still passes after the refactor.

#### 4. Tests needing significant rewrite

| Test | Current Line | Problem | Fix |
|------|-------------|---------|-----|
| `TestDashboard_ConfigTabEnterEdits` | 942 | Cursor 0 is a provider; after split, Mappings tab has no providers. | Rewrite as `TestDashboard_ProvidersTabEnterOpensModal` (Phase 4) |
| `TestDashboard_ConfigTabScrollsToCursor` | 964 | Uses `collectAllEntries` (merged list). | Rewrite to test Mappings tab with `collectMappingEntries` and `mappingsCursor` |
| `TestDashboard_MouseClickConfigEntry` | 1304 | Clicks on mixed entries. | Split into `TestDashboard_MouseClickProvidersTabEntry` and `TestDashboard_MouseClickMappingsTabEntry` |
| `TestDashboard_MouseWheelScroll` config subtest | 1245 | Sets `tabConfig`. | Change to `tabMappings`, update cursor field |
| `TestDashboard_ProvidersTabScroll` | 1001 | Asserts `providerScroll` increments on `k` (lines 1044-1056); Phase 3 removes `providerScroll`. | Rewrite to use `providerCursor` against `len(collectProvidersFromConfig)` |
| `TestDashboard_MouseWheelScroll` providers subtest | 1219 | Asserts `providerScroll` on wheel events (lines 1223-1234); Phase 3 removes `providerScroll`. | Rewrite to use `providerCursor` against `len(collectProvidersFromConfig)` |

#### 5. New tests — Modal flow

**File**: `proxy/tui/model_test.go` (new test functions)

**Intent**: Add the following tests:
- `TestDashboard_ProvidersTabEnterOpensModal`: `d.activeTab = tabProviders`, `d.providerCursor = 0`, press `Enter`, verify `d.showProviderModal == true`, `d.formMode == formEditProvider`
- `TestDashboard_ProvidersModal_EscCloses`: open modal, press `Esc`, verify `d.showProviderModal == false`, `d.formMode == formNone`
- `TestDashboard_ProvidersModal_SubmitSaves`: open modal, fill fields (including protocol), press `Enter`, verify `config.Providers[name]` updated and `showProviderModal == false`
- `TestDashboard_ProvidersModal_BehaviorPicker`: open modal, Tab to behavior field, press Enter, verify `d.showPicker == true`, select "mix", verify behavior field updated
- `TestDashboard_ProvidersModal_ProtocolPicker`: open modal, Tab to protocol field, press Enter, verify `d.showPicker == true`, select "anthropic", verify protocol field updated
- `TestDashboard_ProvidersModal_DetachModeBlocked`: set `detachOnQuit = true`, press `Enter` on Providers tab, verify `showProviderModal == false`
- `TestDashboard_MappingsTabScrollsCorrectly`: `d.activeTab = tabMappings`, set mappings config, verify cursor navigation with `mappingsCursor`

#### 6. Help text test updates

**File**: `proxy/tui/model_test.go` (around line 1367, `TestHelpShortcuts_ContainsMouse`)

**Intent**: Update any assertion checking for "Config tab" in help shortcuts to check for "Mappings tab" or the new descriptions.

### Success Criteria:

#### Automated Verification:

- `go build ./cmd/freedius` passes
- `go test ./proxy/tui/...` -v passes with 0 failures
- `go vet ./...` passes
- `go test -cover ./proxy/tui/...` shows no regression in coverage

#### Manual Verification:

- All existing TUI workflows still work (no regression)
- New modal tests validate the overlay lifecycle
- Protocol field can be set and persists through config save/load

---

## Testing Strategy

### Unit Tests:

- Per-tab cursor navigation (providers and mappings independently)
- Overlay modal lifecycle: open, Esc closes, submit saves, picker works inside modal
- Protocol field validation: empty accepted, "openai"/"anthropic" accepted, invalid rejected
- formDeleteConfirm still works on both tabs (Providers and Mappings)
- Detach mode blocks modal opening
- Mouse click on Providers tab opens modal; click on Mappings tab opens inline form

### Integration Tests:

- Config round-trip: add provider with protocol field via modal, save, reload, verify protocol persists
- Full edit cycle: add provider (modal) → add mapping (inline form) → edit mapping (inline) → delete provider (modal) → verify mapping references survive

### Manual Testing Steps:

1. Launch TUI, verify 3 tabs (Log, Providers, Mappings)
2. On Providers tab: j/k moves cursor, row highlight follows
3. Press `p` → modal opens with 6 fields → fill all → Enter → verify provider appears in list
4. Press Enter on a provider row → modal opens with prefilled values → change protocol to "anthropic" → Enter → verify change persists
5. Press `d` on Providers tab → delete confirmation → `y` → verify provider removed
6. Switch to Mappings tab: `a` → inline form → Enter → add mapping → verify appears in list
7. Press Enter on a mapping → inline edit form → change model → Enter → verify persists
8. Press Ctrl+S on Mappings tab → verify shell RC installed
9. Verify help modal (`?`) still works and shows updated descriptions
10. Verify detach mode: start daemon, attach, verify provider edit is blocked (modal doesn't open)

## Performance Considerations

No performance implications. This is a pure UI reorg with no changes to data loading, request handling, or rendering performance characteristics.

## Migration Notes

No data migration. Config YAML schema is unchanged. Existing configs continue to work. The `protocol` field is optional (empty = URL-sniffing fallback).

## References

- Research: `context/changes/tui-providers-mappings-split/research.md`
- Related research: `context/changes/improve-mixed-providers-config/research.md` — Protocol field gaps, TUI UX notes
- Related research: `context/changes/missing-providers-tui/research.md` — Why popular providers were missing from TUI
- Provider struct: `config/config.go:37-54`
- MixAdapter protocol routing: `proxy/mix.go:51-63`
- Help modal pattern: `proxy/tui/views.go:385-401`
- Modal styles: `proxy/tui/styles.go:173-191`
- Picker pattern: `proxy/tui/picker.go:60-80`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Foundation

#### Automated

- [x] 1.1 `go build ./cmd/freedius` passes after renaming `tabConfig` → `tabMappings` — 3314901
- [x] 1.2 `go test ./proxy/tui/...` passes (or compiles with expected failures noted) — 3314901
- [x] 1.3 `go vet ./...` passes — 3314901

#### Manual

- [x] 1.4 TUI launches with updated help text and tab labels — 3314901

### Phase 2: Mappings Tab

#### Automated

- [x] 2.1 `go build ./cmd/freedius` passes — 3314901
- [x] 2.2 `go test ./proxy/tui/...` passes — 3314901
- [x] 2.3 `go vet ./...` passes — 3314901

#### Manual

- [x] 2.4 Mappings tab shows only mapping entries (no providers) — 3314901
- [x] 2.5 j/k cursor navigation works on Mappings tab — 3314901

### Phase 3: Providers Tab

#### Automated

- [x] 3.1 `go build ./cmd/freedius` passes — 3314901
- [x] 3.2 `go test ./proxy/tui/...` passes — 3314901
- [x] 3.3 `go vet ./...` passes — 3314901

#### Manual

- [x] 3.4 Providers tab shows cursor highlight, j/k navigation works — 3314901
- [x] 3.5 p opens provider form (inline, 6 fields including protocol) — 3314901
- [x] 3.6 Enter/e opens inline edit form; d triggers delete confirmation — 3314901
- [x] 3.7 Click on provider row opens inline edit form — 3314901

### Phase 4: Overlay Modal

#### Automated

- [x] 4.1 `go build ./cmd/freedius` passes — 3314901
- [x] 4.2 `go test ./proxy/tui/...` passes — 3314901
- [x] 4.3 `go vet ./...` passes — 3314901

#### Manual

- [x] 4.4 Enter on Providers tab opens centered overlay modal — 3314901
- [x] 4.5 Modal shows 6 fields; Tab/Shift+Tab navigate; Esc closes — 3314901
- [x] 4.6 Behavior picker works inside modal; protocol picker works inside modal — 3314901
- [x] 4.7 Submit saves and closes modal; d in tab mode deletes provider — 3314901
- [x] 4.8 Mappings tab still uses inline form editing (not modal) — 3314901
- [x] 4.9 Detach mode blocks modal from opening — 3314901

### Phase 5: Tests

#### Automated

- [x] 5.1 `go build ./cmd/freedius` passes — 3314901
- [x] 5.2 `go test ./proxy/tui/...` -v passes with 0 failures — 3314901
- [x] 5.3 `go vet ./...` passes — 3314901
- [x] 5.4 `go test -cover ./proxy/tui/...` shows no coverage regression — 3314901

#### Manual

- [x] 5.5 All new modal tests pass; protocol field round-trips through save/load — 3314901
