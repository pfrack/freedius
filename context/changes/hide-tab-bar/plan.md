# Compact TUI: Merge Tabs into Topbar — Implementation Plan

## Overview

Remove the separate tab bar row (`[1] Log [all]  [2] Providers  [3] Config`) and merge tab indicators into the stats bar (topline). Change tab switching shortcuts from `1`/`2`/`3` to `F1`/`F2`/`F3` to avoid conflicts with form text input. The layout becomes a single topbar + body, gaining 2 extra rows for content.

## Current State Analysis

The TUI renders three chrome rows: stats bar (1 row) + tab labels (1 row) + tab bar border (1 row). `bodyHeight` is calculated as `height - 3` (`model.go:551`). The `renderTabs` function (`views.go:16`) builds the separate tab bar; it's called from `Dashboard.View()` (`model.go:571`). The `renderStatsBar` function (`views.go:205`) renders the stats bar independently.

Tab switching currently uses `1`/`2`/`3` keys in `handleTabModeKeyPress` (`model.go:290-298`). These bare number keys cannot be used as shortcuts when a form is open (form mode intercepts all key input), but the deeper issue is they reserve single-character keys that could conflict with future text-input features.

The help modal (`help.go:11`) documents `1 / 2 / 3` for tab switching.

### Key Discoveries

- `bodyHeight = height - 3` reserves 3 rows: 1 stats + 1 tab label + 1 tab border (`model.go:546-551`)
- `renderStatsBar` (`views.go:205`) currently builds the stats line with stats message appended; tab indicators need to be appended on the right side
- `renderTabs` is only called from `Dashboard.View()` (`model.go:571`) — no other callers
- The `renderTabs` function uses `ActiveTabStyle` (bold+underline) and `InactiveTabStyle` (faint) — these same styles can be reused in the stats bar for tab indicators
- `TabBarStyle` (`styles.go:153-155`) adds a bottom border; this style is no longer needed once the separate row is removed
- Test `TestDashboard_Layout_StatsAboveTabs` (`model_test.go:825`) asserts the tab bar appears between stats and body — needs rewrite
- Test `TestRenderTabs_LabelIsLog` (`model_test.go:698`) tests `renderTabs()` directly — can be kept or removed
- Bubble Tea's `tea.KeyPressMsg` has a `Code` field for special keys; `tea.KeyF1`, `tea.KeyF2`, `tea.KeyF3` are the constants for function keys

## Desired End State

The TUI shows a single topbar line with stats on the left and tab indicators on the right, followed by the active tab's body content. Example:

```
 uptime: 5s │ requests: 10 │ errors: 0 │ error rate: 0.0%        F1:Log F2:Providers F3:Config
```

Tab switching via `F1`/`F2`/`F3` and `Tab`/`Shift+Tab` works. The `?` help modal documents the new shortcuts. Body area is 2 rows taller.

### Verification

- `freedius` starts with a single topbar showing stats + tab indicators
- Active tab indicator is visually distinct (bold+underline), inactive tabs are faint
- `F1` switches to Log tab, `F2` to Providers, `F3` to Config
- `Tab`/`Shift+Tab` cycles tabs
- Typing `1`/`2`/`3` in a form inserts the character (does not switch tabs)
- `?` help modal shows `F1 / F2 / F3` for tab switching
- Body area is 2 rows taller than before

## What We're NOT Doing

- No changes to tab switching logic (only the key bindings change)
- No changes to form mode or picker behavior
- No changes to the help modal layout/content beyond the shortcut key label update
- No changes to scroll behavior or tab content rendering

## Implementation Approach

Three coordinated changes: (1) modify `renderStatsBar` to accept the active tab and log level, appending tab indicators on the right side of the stats line, (2) remove the `renderTabs` call from `Dashboard.View()` and adjust `bodyHeight`, (3) change tab key bindings from `1`/`2`/`3` to `F1`/`F2`/`F3` in `handleTabModeKeyPress`. Update the help shortcuts list and relevant tests.

## Critical Implementation Details

- **Stats bar right-alignment**: Tab indicators must be right-aligned in the stats bar. The `renderStatsBar` function currently pads the stats line to `width`. With tab indicators on the right, the approach is: render stats text, calculate remaining space, render tab indicators right-aligned. This requires knowing the active tab and log level (same args as `renderTabs`).
- **Bubble Tea F-key constants**: `tea.KeyF1`, `tea.KeyF2`, `tea.KeyF3` — these are the `Code` values. The `msg.String()` representation is `"f1"`, `"f2"`, `"f3"`. The switch in `handleTabModeKeyPress` uses `msg.String()`, so the cases should be `"f1"`, `"f2"`, `"f3"`.

## Phase 1: Merge tab indicators into stats bar

### Overview

Modify `renderStatsBar` to accept the active tab index and log level, and render compact tab indicators right-aligned on the same line as the stats. Remove the separate `renderTabs` call from `Dashboard.View()` and reclaim 2 rows for body height.

### Changes Required:

#### 1. renderStatsBar — add tab indicators

**File**: `proxy/tui/views.go`

**Intent**: Extend `renderStatsBar` to accept the active tab index and log level, and append right-aligned tab indicators to the stats line. The indicators use the same `ActiveTabStyle`/`InactiveTabStyle` as the old tab bar.

**Contract**: Change signature to `renderStatsBar(stats statsData, width int, activeTab int, level LogFilter, styles Styles) string`. After building the stats text (left side), build compact tab labels (`F1:Log`, `F2:Providers`, `F3:Config`) styled with `ActiveTabStyle`/`InactiveTabStyle`. Place the tab indicators at the right end of the line, padded to `width`. The active tab gets `ActiveTabStyle`, others get `InactiveTabStyle`. The log level filter label appears after `F1:Log` (e.g., `F1:Log[all]`).

#### 2. Dashboard.View — remove tab bar, adjust body height

**File**: `proxy/tui/model.go`

**Intent**: Remove the `renderTabs` call and its composition into the output. Change `bodyHeight` from `height - 3` to `height - 1` (only the topbar takes chrome space). Update the `renderStatsBar` call to pass `activeTab` and `currentLogLevel`.

**Contract**: In `Dashboard.View()`:
- Remove `tabs := renderTabs(d.activeTab, width, d.currentLogLevel, d.styles)` (line 571)
- Change `result := fmt.Sprintf("%s\n%s\n%s", stats, tabs, body)` to `result := fmt.Sprintf("%s\n%s", stats, body)`
- Change `bodyHeight := height - 3` to `bodyHeight := height - 1`
- Update `renderStatsBar(d.stats, width, d.styles)` to `renderStatsBar(d.stats, width, d.activeTab, d.currentLogLevel, d.styles)`
- Update the comment at lines 546-550 to reflect the 1-row chrome budget

#### 3. Tab key bindings — change to F1/F2/F3

**File**: `proxy/tui/model.go`

**Intent**: Change tab switching from bare `1`/`2`/`3` keys to `F1`/`F2`/`F3` to avoid conflicts with form text input.

**Contract**: In `handleTabModeKeyPress` (`model.go:280`), replace:
- `case "1":` → `case "f1":`
- `case "2":` → `case "f2":`
- `case "3":` → `case "f3":`

#### 4. Help shortcuts — update key labels

**File**: `proxy/tui/help.go`

**Intent**: Update the tab switching shortcut entry to reflect the new `F1`/`F2`/`F3` keys.

**Contract**: Change line 11 from `{"1 / 2 / 3", "Switch to Log / Providers / Config tab"}` to `{"F1 / F2 / F3", "Switch to Log / Providers / Config tab"}`.

### Success Criteria:

#### Automated Verification:

- Build passes: `go build ./...`
- All tests pass: `go test ./...`
- `go vet ./...` clean

#### Manual Verification:

- `freedius` starts with a single topbar showing stats on left and tab indicators on right
- Active tab indicator is bold+underline; inactive tabs are faint
- `F1` switches to Log, `F2` to Providers, `F3` to Config
- `Tab`/`Shift+Tab` cycles tabs
- Typing `1` in a form field inserts the character (does not switch tabs)
- `?` help modal shows `F1 / F2 / F3` for tab switching
- Body area is 2 rows taller

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful.

---

## Phase 2: Test updates

### Overview

Update tests that reference the old tab bar rendering or the old `1`/`2`/`3` key bindings.

### Changes Required:

#### 1. Layout test — adapt to merged topbar

**File**: `proxy/tui/model_test.go`

**Intent**: `TestDashboard_Layout_StatsAboveTabs` currently asserts the tab bar appears as a separate element between stats and body. With tabs merged into the stats bar, rewrite to verify: (a) stats bar appears, (b) tab indicators appear within the stats bar line, (c) body content appears after the stats bar, (d) no separate `[1] Log` tab bar row exists.

**Contract**: Rename to `TestDashboard_Layout_TopbarContainsTabs`. Keep the stats-before-body ordering check. Add assertion that tab indicators (`F1:Log`) appear in the output. Remove assertion about a separate `[1] Log` tab bar element.

#### 2. Tab key tests — update to F1/F2/F3

**File**: `proxy/tui/model_test.go`

**Intent**: Tests `TestDashboard_Update_KeyPress` and `TestDashboard_Update_TabCycle` use `tea.KeyPressMsg{Code: '1'}` etc. to test tab switching. Update these to use `tea.KeyPressMsg{Code: tea.KeyF1}` etc.

**Contract**: In `TestDashboard_Update_KeyPress`:
- `{Code: '1'}` → `{Code: tea.KeyF1}` (and update `wantTab: tabLog`)
- `{Code: '2'}` → `{Code: tea.KeyF2}` (and update `wantTab: tabProviders`)
- `{Code: '3'}` → `{Code: tea.KeyF3}` (and update `wantTab: tabConfig`)

In `TestDashboard_Update_TabCycle`:
- All `tea.KeyPressMsg{Code: tea.KeyTab}` stay unchanged (Tab cycling is unaffected)
- If any test uses `'1'`/`'2'`/`'3'` for tab init, keep using `d.activeTab = tabLog` directly (init, not key press)

#### 3. RenderTabs test — keep or remove

**File**: `proxy/tui/model_test.go`

**Intent**: `TestRenderTabs_LabelIsLog` tests the `renderTabs` function directly. The function still exists (kept for potential re-enablement), so the test is still valid. Keep it as-is.

**Contract**: No changes needed — the test calls `renderTabs()` directly, not through the View.

### Success Criteria:

#### Automated Verification:

- Build passes: `go build ./...`
- All tests pass: `go test ./...`
- `go vet ./...` clean

#### Manual Verification:

- N/A — automated test updates only

---

## Testing Strategy

### Unit Tests:

- `TestDashboard_Layout_TopbarContainsTabs` — adapted from `StatsAboveTabs`, verifies merged topbar layout
- `TestDashboard_Update_KeyPress` — updated key codes to `tea.KeyF1`/`F2`/`F3`
- `TestDashboard_Update_TabCycle` — unchanged (Tab/Shift+Tab unaffected)
- `TestRenderTabs_LabelIsLog` — kept as-is (tests function directly)
- `TestDashboard_LKeyInTabMode` — unchanged (tests `l` key for log level cycling)

### Manual Testing Steps:

1. Start `freedius` — verify single topbar with stats + tab indicators
2. Verify active tab is visually distinct (bold+underline)
3. Press `F2` — verify Providers tab content appears
4. Press `F3` — verify Config tab content appears
5. Press `F1` — verify Log tab content appears
6. Press `Tab` — verify tab cycling works
7. Open a form (press `e` on Config tab), type `1` — verify `1` is inserted, not a tab switch
8. Press `?` — verify help modal shows `F1 / F2 / F3`
9. Verify body area is 2 rows taller

## References

- Stats bar rendering: `proxy/tui/views.go:205-227`
- Tab bar rendering: `proxy/tui/views.go:16-32`
- View composition: `proxy/tui/model.go:570-579`
- Body height calculation: `proxy/tui/model.go:546-551`
- Tab key handling: `proxy/tui/model.go:290-298`
- Help shortcuts: `proxy/tui/help.go:8-29`
- Layout test: `proxy/tui/model_test.go:825-853`
- Key press test: `proxy/tui/model_test.go:33-67`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Merge tab indicators into stats bar

#### Automated

- [x] 1.1 Build passes: `go build ./...`
- [x] 1.2 All tests pass: `go test ./...`
- [x] 1.3 `go vet ./...` clean

#### Manual

- [ ] 1.4 `freedius` starts with single topbar (stats + tab indicators)
- [ ] 1.5 `F1`/`F2`/`F3` switch tabs correctly
- [ ] 1.6 `Tab`/`Shift+Tab` cycles tabs
- [ ] 1.7 Typing `1` in a form inserts the character
- [ ] 1.8 `?` help modal shows `F1 / F2 / F3`
- [ ] 1.9 Body area is 2 rows taller

### Phase 2: Test updates

#### Automated

- [x] 2.1 Build passes: `go build ./...`
- [x] 2.2 All tests pass: `go test ./...`
- [x] 2.3 `go vet ./...` clean

#### Manual

- [ ] 2.4 N/A — automated test updates only
