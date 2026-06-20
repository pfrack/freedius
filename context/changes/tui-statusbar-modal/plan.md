# TUI Statusbar on Top + `?` Keyboard Shortcuts Modal — Implementation Plan

## Overview

Two related, low-risk changes to the freedius Bubble Tea dashboard:

1. **Move the stats bar to the top of the dashboard** so uptime, request count, error count, error rate, and the transient status message are always visible. The tab switcher stays a single row directly below the stats bar.
2. **Add a keyboard shortcuts help modal** opened with `?`, listing every current keybinding in a flat two-column layout, dismissed with `?` or `Esc`.

The two changes are independent and touch disjoint code paths, but both land in the same review because they jointly improve the dashboard chrome.

## Current State Analysis

The TUI is implemented in `proxy/tui/` using Bubble Tea v2 (`charm.land/bubbletea/v2 v2.0.7`) and lipgloss v2 (`charm.land/lipgloss/v2 v2.0.4`). The dashboard chrome is composed in `Dashboard.View()` at `proxy/tui/model.go:457-497`:

- **Stats bar** is rendered by `renderStatsBar` (`proxy/tui/views.go:219-241`) and is the **last** line of the `View()` output. `statsBarStyle` at `proxy/tui/styles.go:26-28` has no border, so the output is exactly 1 line.
- **Tab switcher** is rendered by `renderTabs` (`proxy/tui/views.go:16-32`) and is the **first** segment. `tabBarStyle` at `proxy/tui/styles.go:30-32` has a `Border(false, false, true, false)` — a bottom border only — which contributes exactly 1 extra line of output, so `renderTabs` produces 2 lines (label row + border row).
- **Body** is the active tab's content (or the form, if a form is open), with `bodyHeight := height - 3` (`proxy/tui/model.go:471`) reserved for the chrome.

**No overlay mechanism exists at the screen level.** The only overlay today is the `providerPicker` (`proxy/tui/picker.go:23-99`), which is *body-scoped* — it replaces one field's view inside the form, not the whole screen. The new modal will be the first screen-scoped overlay.

**No keyboard shortcut inventory or help system exists.** All bindings are scattered across `handleTabModeKeyPress` (`proxy/tui/model.go:248-311`), `handleFormKeyPress` (`proxy/tui/model.go:365-422`), `handleDeleteConfirmKeyPress` (`proxy/tui/model.go:424-454`), the global `esc` check in `Update` (`proxy/tui/model.go:187`), and the picker's `Update` (`proxy/tui/picker.go:80-95`). A user learning the TUI has to read the source to find every key.

**Latent bug in the form footer:** `proxy/tui/views.go:370` says `Enter=Save  Esc=Cancel  Tab=Next Field  Ctrl+D=Delete`, but `ctrl+d` is not bound anywhere in the package — the only mention of "Ctrl+D" in the repository is this footer string. The actual delete flow is two steps: `d` on the Config tab, then `y`/`n` in the confirm prompt.

## Desired End State

**Layout:** the dashboard renders as a vertical stack — stats bar (1 row) → tab bar (label + bottom border = 2 rows) → body content. The stats bar is always visible regardless of `formMode` and active tab. The `bodyHeight := height - 3` math is unchanged (still 1 + 2 = 3 reserved rows). The form footer at `proxy/tui/views.go:370` no longer mentions `Ctrl+D=Delete`.

**Modal:** pressing `?` while no form is open renders a centered dialog titled "Keyboard Shortcuts" listing every current binding as `key — description` rows in a flat list. Pressing `?` or `Esc` while the modal is open closes it (toggle for `?`). While the modal is open, no other key propagates to the dashboard — `1`/`2`/`3` do not switch tabs, `q`/`Ctrl+C` do not quit, `e`/`a`/`p`/`d` do not open forms.

The change is verified by:
- `go test ./...` passes (existing 934 lines of tests + 1 layout regression test + 6 modal tests).
- `go vet ./...` clean.
- Manual launch shows stats bar at top, tabs below, content below.
- Manual: `?` opens the modal, modal shows every binding from the research inventory, `?` and `Esc` both close it, all dashboard state is preserved across close.

### Key Discoveries

- The chrome row count is symmetric across the old and new layouts (1 stats + 2 tab rows = 3 reserved rows either way), so `bodyHeight := height - 3` at `proxy/tui/model.go:471` stays correct. The layout reorder is purely a Sprintf argument order change at `proxy/tui/model.go:493`.
- `?` is unbound across all 17 `case` labels in the package (verified by Grep across `proxy/tui/`). No key collision to resolve.
- The existing `d.showPicker bool` at `proxy/tui/model.go:101` is the design template for the new `d.showHelp bool` — same shape, same dispatch pattern.
- `lipgloss.Place(width, height, hPos, vPos, str, opts...)` at `charm.land/lipgloss/v2@v2.0.4/position.go:36` is the idiomatic Charm overlay. v2's `WithWhitespaceStyle(s lipgloss.Style)` (`whitespace.go:65`) replaced v1's separate background/foreground options.
- All 4 existing `d.View()` test call sites in `proxy/tui/model_test.go` use `strings.Contains` against body content, never asserting chrome position. The layout reorder breaks zero existing tests.
- The form footer `Ctrl+D=Delete` bug is one line of misleading text — fix as part of this change to keep the chrome review cohesive.

## What We're NOT Doing

- **No new dependencies.** lipgloss v2.0.4 already provides `Place` and `WithWhitespaceStyle`.
- **No picker `q`/`Ctrl+C` leak fix.** The providerPicker currently lets `q`/`Ctrl+C` fall through to bubble list's default Quit keymap, which quits the TUI from inside the picker. This is pre-existing and unrelated to this change — noted for a follow-up.
- **No scrollable modal.** With ~18 rows of content the modal fits on a 24-line terminal. On smaller terminals the body may clip — flag for a follow-up if the user requests it.
- **No visual group headers in the modal.** The shortcut data is "grouped by context" logically (data order follows the grouping), but rendered as a flat list. Adding visual section dividers is a small follow-up if requested.
- **No `Ctrl+D` binding.** The misleading footer text is dropped; we do not add a real `Ctrl+D` binding. In-form delete is a separate product decision.
- **No change to `bodyHeight := height - 3` math.** It already accommodates both layouts.
- **No change to `main.go`, `proxy/`, `config/`, or any non-TUI package.**

## Implementation Approach

**Two phases, hand-rolled with tests-after, no TDD** (per user decision). Each phase is a reviewable diff with a clear manual-confirmation gate.

- **Phase 1 (small, mechanical)** — touch `proxy/tui/model.go:493` (Sprintf reorder) and `proxy/tui/model.go:471` (clarifying comment), touch `proxy/tui/views.go:370` (drop `Ctrl+D=Delete`), add 1 regression test in `proxy/tui/model_test.go`. ~6 LOC changes + ~10 LOC test.
- **Phase 2 (additive, no state-machine change outside the new flag)** — add 1 file (`proxy/tui/help.go`), extend `proxy/tui/styles.go` (5 new style vars), extend `proxy/tui/views.go` (`modalWidthFor`, `renderHelpModal`, `overlayModal`), extend `proxy/tui/model.go` (1 field, 1 capture block in `Update`, 1 case in `handleTabModeKeyPress`, 1 overlay composition in `View`), add 6 tests. ~150 LOC.

State ordering inside `Update`'s `KeyPressMsg` case matters: the help-capture block must run *before* the `esc`-quits-when-no-form check at `proxy/tui/model.go:187`, because while help is open `Esc` should close the modal, not quit the TUI.

## Critical Implementation Details

- **`?` opener is scoped to tab mode on purpose.** The `case "?"` lives inside `handleTabModeKeyPress` (`proxy/tui/model.go:248-311`), not at the top of `Update`. This is required so that `?` remains a typeable character in form fields (URLs, model names) and is a no-op while the picker is open (form mode intercepts first). A naive top-of-`Update` placement would break form text input.
- **Help-capture block must be the first branch in `KeyPressMsg` dispatch.** It returns `d, nil` early on every key, swallowing all input. This guarantees no other key propagates to the form/tab dispatchers while help is open, which is what prevents accidental tab-switching, quit, and form-opening.
- **Bodyheight math is symmetric but undocumented.** Add a comment at `proxy/tui/model.go:471` explaining that the `-3` is `1 stats + 2 tab rows`. Without this, a future change that adds a row to either bar will silently overflow.
- **Modal width clamps to `[40, 60]`.** On a 60-col terminal the modal is 36 cols; on 100+ it caps at 60. Smaller terminals are still readable, larger terminals don't waste space.
- **lipgloss.Place is non-trivial in v2.** v1's `WithWhitespaceBackground` and `WithWhitespaceForeground` are gone — there is one `WithWhitespaceStyle(s lipgloss.Style)` (`charm.land/lipgloss/v2@v2.0.4/whitespace.go:65`, upgrade notes in `UPGRADE_GUIDE_V2.md:339-353`). The overlay helper uses `Background("0")` so the modal sits in a black-bordered "frame" on top of the dashboard.

---

## Phase 1: Layout reorder + footer fix + regression test

### Overview

Three coordinated touches to the dashboard chrome plus one regression test:
- Move stats to the top of `View()`.
- Drop the misleading `Ctrl+D=Delete` substring from the form footer.
- Add a comment explaining the `bodyHeight` math.
- Add a layout-order regression test that locks in the new position.

No state changes, no new behavior, no new tests for the modal.

### Changes Required:

#### 1. Reorder Sprintf in `Dashboard.View`

**File**: `proxy/tui/model.go`

**Intent**: The current View() composes `tabs → body → stats`; the new layout is `stats → tabs → body`. This is the only View()-level change needed — `bodyHeight := height - 3` at line 471 stays correct because the reserved row count is symmetric (1 + 2 = 3 in both layouts).

**Contract**: At `proxy/tui/model.go:489-496`, swap the order of the three variables (assign in the new order: stats, tabs, body) AND swap the order of the three `%s` placeholders in the Sprintf at line 493 so the segments compose as `stats\ntabs\nbody`. No other lines change.

#### 2. Document the bodyHeight math

**File**: `proxy/tui/model.go`

**Intent**: Today `bodyHeight := height - 3` has no comment. A future change that adds a row to either the stats bar or the tab bar would silently overflow the body. A short comment names the three reserved rows and why the math is symmetric.

**Contract**: Insert a 4-line comment immediately above the `bodyHeight := height - 3` assignment at `proxy/tui/model.go:471`. The comment must mention: 1 row for the stats bar, 1 row for the tab labels, 1 row for the tab bar's bottom border.

#### 3. Drop misleading `Ctrl+D=Delete` from the form footer

**File**: `proxy/tui/views.go`

**Intent**: The footer text at line 370 advertises a key that is not bound. Drop the trailing `  Ctrl+D=Delete` substring so the remaining labels (`Enter=Save`, `Esc=Cancel`, `Tab=Next Field`) are accurate.

**Contract**: At `proxy/tui/views.go:370`, change the string literal so it ends after `Tab=Next Field`. No other change to the footer (still rendered via `statusClientErrStyle.Render`).

#### 4. Add layout-order regression test

**File**: `proxy/tui/model_test.go`

**Intent**: Lock in the new chrome order so future refactors don't silently regress it. No existing test in the suite asserts chrome position; this fills that gap.

**Contract**: Add a new test `TestDashboard_Layout_StatsAboveTabs` to `proxy/tui/model_test.go`. The test:
1. Calls `newTestDashboard(nil, "", 0, false)`.
2. Sets `d.width = 80` and `d.height = 24` (so `bodyHeight` is non-zero).
3. Renders via `viewContent(d.View())` (helper at line 807).
4. Strips ANSI with `stripANSI` (helper at line 691).
5. Asserts `strings.Index(out, "uptime:") < strings.Index(out, "[1] Log")`.
6. Asserts `strings.Index(out, "[1] Log") < strings.Index(out, "No requests")` (proving tabs precede body content).

### Success Criteria:

#### Automated Verification:

- 1.1 `go build -o /tmp/freedius-build .` completes without errors.
- 1.2 `go test ./...` passes (existing 934-line suite + the new `TestDashboard_Layout_StatsAboveTabs`).
- 1.3 `go vet ./...` clean.
- 1.4 `TestDashboard_Layout_StatsAboveTabs` is present in `model_test.go` and runs green.

#### Manual Verification:

- 1.5 Launch freedius (`go run .`); verify the stats bar (showing `uptime:`, `requests:`, `errors:`, `error rate:`) appears at the very top of the TUI, the tab switcher (`[1] Log [2] Providers [3] Config`) is directly below it, and the active tab's content fills the rest of the screen.
- 1.6 Press `1`/`2`/`3` to switch tabs; verify the stats bar stays pinned at the top.
- 1.7 On the Config tab, press `e` to open an edit form; verify the stats bar is still at the top and the form footer no longer mentions `Ctrl+D=Delete`.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to Phase 2.

---

## Phase 2: `?` keyboard shortcuts modal

### Overview

Add a centered help dialog that lists every current keyboard shortcut. New file for shortcut data, new styles, new renderer, new dispatch logic, six new tests. No existing behavior changes outside the new flag and its open/close transitions.

### Changes Required:

#### 1. Create `proxy/tui/help.go` with shortcut data

**File**: `proxy/tui/help.go` (new)

**Intent**: A flat list of all current keybindings, ordered by context: Global → Tab switching → Log/Providers/Config (sharing the same `j`/`k` row) → Form mode → Delete confirm → Picker. The renderer consumes this list verbatim.

**Contract**: File contains:
- `type shortcut struct { key, desc string }` (lowercase, unexported).
- `var helpShortcuts = []shortcut{...}` populated in the exact order from the research inventory in `context/changes/tui-statusbar-modal/research.md` §5 (the row list is reproduced verbatim in the file; do not re-derive it from the source).
- Package declaration `package tui` matching the existing files.
- No imports required (the data is plain strings).

Row content (in order, copy verbatim from research §5):
```
{"q / Ctrl+C",        "Quit"},
{"?",                 "Show this help"},
{"1 / 2 / 3",         "Switch to Log / Providers / Config tab"},
{"Tab / Shift+Tab",   "Cycle tabs (or fields in a form)"},
{"↑ / k",             "Scroll up"},
{"↓ / j",             "Scroll down"},
{"e / Enter",         "Edit config entry (Config tab)"},
{"a",                 "Add new mapping (Config tab)"},
{"p",                 "Add new provider (Config tab)"},
{"d",                 "Delete entry under cursor (Config tab)"},
{"Ctrl+E",            "Toggle verbose errors"},
{"Ctrl+S",            "Install shell RC (Config tab)"},
{"Tab",               "Next form field"},
{"Shift+Tab",         "Previous form field"},
{"Enter",             "Save / open picker"},
{"Esc",               "Cancel form"},
{"y / n",             "Confirm / cancel delete"},
{"Enter / Esc",       "Select / cancel (picker)"},
```

#### 2. Add modal styles

**File**: `proxy/tui/styles.go`

**Intent**: Five new lipgloss styles in the existing `var ( ... )` block — one for the modal box, one for the title, one for the footer hint, one for shortcut keys (bold/cyan), one for shortcut descriptions (gray). Colors reuse the palette already in use (`"4"`, `"6"`, `"7"`, `"0"`).

**Contract**: Append to the `var ( ... )` block in `proxy/tui/styles.go` (currently ends at line 49). Add:
- `modalStyle` — `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("4")).Padding(1, 2)`.
- `modalTitleStyle` — `Bold(true).Foreground(lipgloss.Color("4")).Padding(0, 1)`.
- `modalFooterStyle` — `Faint(true).Italic(true)`.
- `shortcutKeyStyle` — `Bold(true).Foreground(lipgloss.Color("6"))`.
- `shortcutDescStyle` — `Foreground(lipgloss.Color("7"))`.

#### 3. Add modal renderers to `proxy/tui/views.go`

**File**: `proxy/tui/views.go`

**Intent**: Three new functions in views.go:
- `modalWidthFor(terminalWidth int) int` — returns `min(max(terminalWidth * 60 / 100, 40), 60)`.
- `renderHelpModal(terminalWidth int) string` — composes the title, a `lipgloss.JoinHorizontal` two-column body from `helpShortcuts` (key column width 14), a blank line, a footer line ("Press ? or Esc to close"), all wrapped in `modalStyle`.
- `overlayModal(_, modal string, width, height int) string` — wraps `lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal, lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("0"))))`. The first `base` parameter is unused (the dashboard is overwritten by each frame; the Place output alone produces the final screen) — keep the parameter so future enhancements can dim the background without changing the call site.

**Contract**: 
- `modalWidthFor` is a pure function; takes `int`, returns `int`.
- `renderHelpModal` is a pure function; takes `int` (terminal width), returns `string`. Uses `lipgloss.JoinHorizontal`, `lipgloss.JoinVertical`, `modalStyle`, `modalTitleStyle`, `modalFooterStyle`, `shortcutKeyStyle`, `shortcutDescStyle`, `separatorStyle`, and the `helpShortcuts` data from `help.go`.
- `overlayModal` is a pure function; takes `(base, modal string, width, height int)`, returns `string`. Uses `lipgloss.Place` and `WithWhitespaceStyle` from `charm.land/lipgloss/v2`.
- All three go in `proxy/tui/views.go`; place after `renderDeleteConfirm` (currently ends at line 380).

#### 4. Add `showHelp` field to `Dashboard` struct

**File**: `proxy/tui/model.go`

**Intent**: New boolean field, mirroring the existing `showPicker` (`proxy/tui/model.go:101`).

**Contract**: Add `showHelp bool` to the `Dashboard` struct. Place it immediately after `showPicker` and `picker` at `proxy/tui/model.go:101-102` so the two overlay flags are grouped. No other struct field changes.

#### 5. Add help-capture block at the top of `KeyPressMsg` dispatch

**File**: `proxy/tui/model.go`

**Intent**: While the modal is open, every keystroke must be swallowed except `?` and `esc` (which close it). Inserting at the top of the `case tea.KeyPressMsg:` in `Update` ensures nothing propagates to the form/tab dispatchers.

**Contract**: At `proxy/tui/model.go:185-203` (the `case tea.KeyPressMsg:` branch in `Update`), insert a new block as the first statement, BEFORE the existing `if d.formMode == formNone && msg.String() == "esc"` line at 187:

```go
// Help modal: capture every key while open.
if d.showHelp {
    switch msg.String() {
    case "?", "esc":
        d.showHelp = false
    }
    return d, nil
}
```

The block MUST end with `return d, nil` so no key propagates. It MUST be placed before the `esc` quit check (otherwise `esc` would quit instead of closing the modal).

#### 6. Add `?` case in `handleTabModeKeyPress`

**File**: `proxy/tui/model.go`

**Intent**: The `?` keypress opens the help modal. Lives in tab mode (no form, no picker) so `?` remains typeable in form fields and is a no-op while the picker is open.

**Contract**: Add a new case to the `switch msg.String()` block in `handleTabModeKeyPress` at `proxy/tui/model.go:248-311`. The new case:

```go
case "?":
    d.showHelp = true
    return d, nil
```

Placement within the switch is free (any case is reached only when `d.showHelp == false` and `d.formMode == formNone`, both already enforced by the existing dispatch). Grouping near the quit cases (`q`, `ctrl+c`) at line 250 is fine.

#### 7. Add overlay composition at the end of `View()`

**File**: `proxy/tui/model.go`

**Intent**: When `d.showHelp` is true, the dashboard output should be the overlay-modal output, not the base layout.

**Contract**: At the end of `Dashboard.View` (`proxy/tui/model.go:457-497`), after the existing `result := fmt.Sprintf("%s\n%s\n%s", stats, tabs, body)` line (which now produces the new top-pinned layout from Phase 1), and before the `v := tea.NewView(result)` line, add:

```go
if d.showHelp {
    modal := renderHelpModal(width)
    result = overlayModal(result, modal, width, height)
}
```

The width/height variables are already in scope (computed earlier in `View` at lines 463-470).

#### 8. Add six tests for the modal

**File**: `proxy/tui/model_test.go`

**Intent**: Cover the open/close/toggle/capture/view behavior. Tests use the existing helpers `newTestDashboard` (line 25), `tea.KeyPressMsg` constructors (lines 39-44), `stripANSI` (line 691), and `viewContent` (line 807).

**Contract**: Add the following tests, each in the style of existing `TestDashboard_*` tests:

- `TestDashboard_HelpModal_OpensWithQuestionMark` — `d.Update(tea.KeyPressMsg{Text: "?"})`; assert `d.showHelp == true`.
- `TestDashboard_HelpModal_EscCloses` — set `d.showHelp = true`; `d.Update(tea.KeyPressMsg{Code: tea.KeyEsc})`; assert `d.showHelp == false`.
- `TestDashboard_HelpModal_QuestionMarkToggles` — press `?` (assert open), press `?` (assert closed).
- `TestDashboard_HelpModal_CapturesTabSwitchKey` — set `d.showHelp = true`, `d.activeTab = tabLog`; `d.Update(tea.KeyPressMsg{Code: '2'})`; assert `d.activeTab == tabLog` (unchanged). Also asserts that `q` (the quit key) does not flip `d.quitting` while help is open.
- `TestDashboard_HelpModal_ViewContainsTitle` — set `d.width=80, d.height=24, d.showHelp = true`; assert `strings.Contains(stripANSI(viewContent(d.View())), "Keyboard Shortcuts")`.
- `TestDashboard_HelpModal_NotOpenedInForm` — set `d.formMode = formEditProvider` (or any non-zero mode) and add one form field; `d.Update(tea.KeyPressMsg{Text: "?"})`; assert `d.showHelp == false` (because the tab-mode handler is not reached while a form is open). Also assert the character was written into the form field's text input — the form's text input still receives the keystroke.

### Success Criteria:

#### Automated Verification:

- 2.1 `go build -o /tmp/freedius-build .` completes without errors.
- 2.2 `go test ./...` passes (existing 934-line suite + the 1 test from Phase 1 + 6 new modal tests).
- 2.3 `go vet ./...` clean.
- 2.4 All 6 new modal tests are present in `model_test.go` and run green.
- 2.5 `proxy/tui/help.go` exists and contains the exact 18-row `helpShortcuts` data from Phase 2 step 1.
- 2.6 The 5 new styles are appended to the `var ( ... )` block in `proxy/tui/styles.go`.

#### Manual Verification:

- 2.7 Launch freedius; press `?` on the Log tab; verify the modal appears centered, titled "Keyboard Shortcuts", listing every binding from the research inventory, with `?` listed as "Show this help".
- 2.8 Press `?` again; verify the modal closes.
- 2.9 Reopen with `?`; press `Esc`; verify the modal closes.
- 2.10 With the modal open, press `1`/`2`/`3`; verify the active tab does NOT change.
- 2.11 With the modal open, press `q`; verify the TUI does NOT quit.
- 2.12 On the Config tab, press `e` to open an edit form; press `?` while typing in a name field; verify (a) the character `?` is typed into the field and (b) the help modal does NOT open.
- 2.13 On the Config tab, press `p` to open an add-provider form, then press Enter on the behavior field to open the picker; press `?` while the picker is open; verify the help modal does NOT open (no-op).
- 2.14 Resize the terminal to a small width (e.g. 50 cols) and a small height (e.g. 18 rows); open the modal; verify it is still readable and does not corrupt the surrounding layout.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before closing the change.

---

## Testing Strategy

### Unit Tests

**Phase 1:**
- `TestDashboard_Layout_StatsAboveTabs` — locks in the chrome position. Asserts `strings.Index(out, "uptime:") < strings.Index(out, "[1] Log") < strings.Index(out, "No requests")`.

**Phase 2 (all in `proxy/tui/model_test.go`):**
- `TestDashboard_HelpModal_OpensWithQuestionMark` — `?` sets `showHelp = true`.
- `TestDashboard_HelpModal_EscCloses` — `Esc` while open sets `showHelp = false`.
- `TestDashboard_HelpModal_QuestionMarkToggles` — `?` then `?` flips the flag.
- `TestDashboard_HelpModal_CapturesTabSwitchKey` — modal swallows `2` (tab unchanged) and `q` (quit not triggered).
- `TestDashboard_HelpModal_ViewContainsTitle` — `View()` output contains "Keyboard Shortcuts".
- `TestDashboard_HelpModal_NotOpenedInForm` — `?` in a form field is typed into the text input, does not open the modal.

**Existing tests that continue to pass unchanged:** all 934 lines of `proxy/tui/model_test.go`. The four `d.View()` call sites in the existing suite (lines 863, 896, 916, 928) use `strings.Contains` against body content (provider names, entry names, "No requests") and do not depend on chrome position.

### Integration Tests

None beyond the existing `go test ./...` suite. The TUI has no separate integration tier in the project.

### Manual Testing Steps

1. Build: `go build -o freedius .`
2. Run: `./freedius` (or `go run .`)
3. Verify stats bar appears at top with running uptime, request count, error count, error rate.
4. Press `1`, `2`, `3` to switch tabs; verify stats bar stays at top.
5. On the Config tab, press `e` to open an edit form; verify stats bar still at top, form footer no longer says `Ctrl+D=Delete`.
6. Close the form (Esc). Press `?` to open the help modal. Verify it shows every binding.
7. Press `?` again to close. Reopen, press `Esc` to close.
8. With modal open, press `1`/`2`/`3`/`q` — verify no effect on dashboard.
9. Open a form. Press `?` in a name field — verify `?` is typed, modal does not open.
10. Resize terminal to a small size; reopen modal; verify it remains readable.

## Performance Considerations

- The `lipgloss.Place` call in `overlayModal` measures the modal's content height via `strings.Count(str, "\n") + 1` (`charm.land/lipgloss/v2@v2.0.4/position.go:91`) on every frame. For an 18-row modal this is a single string scan of ~500 bytes — negligible. No caching needed.
- `renderHelpModal` allocates 18 row strings, joins them, then renders. The whole sequence runs in O(N) where N is the number of shortcuts (constant 18). The Bubble Tea program redraws at the terminal's refresh rate (typically 60Hz), but since `View()` is only called when the model changes, the modal is rendered only when the user opens/closes it or a request event arrives. Negligible CPU impact.
- No new allocations on the hot path (request event handler at `proxy/tui/model.go:212-223`).

## Migration Notes

None. The change is purely additive and does not modify any persisted data, on-disk config files, the HTTP API, or the proxy event schema. The single string edit at `proxy/tui/views.go:370` is a user-facing label change with no behavioral impact (the key was not bound).

## References

- Research: `context/changes/tui-statusbar-modal/research.md`
- Change: `context/changes/tui-statusbar-modal/change.md`
- Lessons: `context/foundation/lessons.md` (no rules in tension; closest lesson is the SSE-related ones in `proxy/translate/`, unrelated)
- Similar patterns: `proxy/tui/picker.go:23-99` (`providerPicker` — template for the new showHelp flag), `proxy/tui/styles.go:7-49` (style declarations to extend)
- Library APIs: `charm.land/lipgloss/v2@v2.0.4/position.go:26-36` (`Place`), `charm.land/lipgloss/v2@v2.0.4/borders.go:413-419` (border row cost), `charm.land/lipgloss/v2@v2.0.4/whitespace.go:65` (`WithWhitespaceStyle`)
- Prior TUI changes: `context/changes/tui-dashboard/`, `context/changes/tui-config-setup/`, `context/changes/unified-server-logs-tab/`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Layout reorder + footer fix + regression test

#### Automated

- [x] 1.1 `go build -o /tmp/freedius-build .` completes without errors
- [x] 1.2 `go test ./...` passes (existing 934-line suite + the new `TestDashboard_Layout_StatsAboveTabs`)
- [x] 1.3 `go vet ./...` clean
- [x] 1.4 `TestDashboard_Layout_StatsAboveTabs` is present in `model_test.go` and runs green

#### Manual

- [x] 1.5 Launch freedius; verify stats bar at top, tabs below, content below
- [x] 1.6 Press `1`/`2`/`3`; verify stats bar stays pinned at the top
- [x] 1.7 Open edit form on Config tab; verify stats bar still at top, form footer no longer mentions `Ctrl+D=Delete`

### Phase 2: `?` keyboard shortcuts modal

#### Automated

- [ ] 2.1 `go build -o /tmp/freedius-build .` completes without errors
- [ ] 2.2 `go test ./...` passes (existing suite + 1 from Phase 1 + 6 new modal tests)
- [ ] 2.3 `go vet ./...` clean
- [ ] 2.4 All 6 new modal tests are present in `model_test.go` and run green
- [ ] 2.5 `proxy/tui/help.go` exists and contains the 18-row `helpShortcuts` data
- [ ] 2.6 The 5 new styles are appended to the `var ( ... )` block in `proxy/tui/styles.go`

#### Manual

- [ ] 2.7 Press `?` on each tab; verify modal renders centered with title and all bindings
- [ ] 2.8 Press `?` again; verify modal closes
- [ ] 2.9 Reopen with `?`; press `Esc`; verify modal closes
- [ ] 2.10 With modal open, press `1`/`2`/`3`; verify active tab does not change
- [ ] 2.11 With modal open, press `q`; verify TUI does not quit
- [ ] 2.12 Open an edit form; press `?` in a name field; verify `?` is typed, modal does not open
- [ ] 2.13 Open the provider picker; press `?`; verify modal does not open (no-op)
- [ ] 2.14 Resize terminal small; reopen modal; verify layout remains readable
