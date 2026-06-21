# Mouse Support for TUI — Implementation Plan

## Overview

Add mouse click and scroll wheel support to the freedius Bubble Tea TUI dashboard. The TUI (`proxy/tui/`) is entirely keyboard-driven today. Bubble Tea v2 has first-class mouse support via typed mouse messages and a View-level mouse mode field. This plan enables `MouseModeCellMotion`, handles `tea.MouseClickMsg` and `tea.MouseWheelMsg` in `Update()`, and maps click coordinates to tab switches, config entry edits, and help modal dismissal.

## Current State Analysis

- **Zero mouse code**: The TUI handles input exclusively via `tea.KeyPressMsg` in `Update()` (`model.go:202-228`). No `tea.MouseClickMsg`, `tea.MouseWheelMsg`, or `tea.MouseMotionMsg` cases exist.
- **Bubble Tea v2 ready**: The TUI already uses the `tea.View` pattern with `v.AltScreen = true` (`model.go:577-579`). Adding `v.MouseMode = tea.MouseModeCellMotion` is a one-line change.
- **Fixed 3-zone layout**: Stats bar (row 0), tab bar (row 1, 2 rows with border), body (rows 3..height-1) (`model.go:542-543`).
- **Reusable scroll logic**: `scrollUp()` / `scrollDown()` (`model.go:355-388`) already handle all three tabs. Mouse wheel can delegate directly.
- **Tab labels are known constants**: `[1] Log [all]`, `[2] Providers`, `[3] Config (...)` rendered via `renderTabs()` (`views.go:16-32`). Widths computable from label strings + lipgloss padding.
- **Config entries use ~6 lines each** (`views.go:135`). Y-to-entry mapping is straightforward.

## Desired End State

After this plan, the TUI supports:
- **Scroll wheel** on all tabs (Log, Providers, Config) — delegates to existing `scrollUp()`/`scrollDown()`
- **Click on tab labels** — switches to the clicked tab
- **Click on config entries** — moves cursor to the clicked entry and opens the edit form
- **Click on help modal** — closes the modal (same as Esc)
- **Help modal** — lists mouse shortcuts in a "Mouse" section

Text selection remains possible via Shift+click (terminal-native bypass for `MouseModeCellMotion`).

### Key Discoveries:

- `tea.Mouse` provides zero-based `(X, Y)` cell coordinates — `(0,0)` is upper-left (`go doc charm.land/bubbletea/v2.Mouse`)
- `tea.MouseClickMsg.Button` identifies which button: `tea.MouseLeft`, `tea.MouseRight`, `tea.MouseMiddle`
- `tea.MouseWheelMsg.Button` identifies scroll direction: `tea.MouseWheelUp`, `tea.MouseWheelDown`
- `ActiveTabStyle` has `Padding(0, 1)` — adds 1 cell left + 1 cell right per tab label (`styles.go:141`)
- `renderTabs()` joins tabs with `lipgloss.JoinHorizontal` then wraps in `TabBarStyle` with `Width(width-2)` (`views.go:30-31`)
- The picker (`picker.go`) uses `list.Model` from `bubbles/v2` — may auto-handle mouse events once mouse mode is enabled; needs testing

## What We're NOT Doing

- **No hover effects** — `MouseModeAllMotion` is not enabled; no mouse movement tracking
- **No mouse-driven form interaction** — text input fields remain keyboard-only
- **No runtime mouse toggle** — mouse mode is always on; Shift+click bypasses for text selection
- **No provider tab click-to-select** — provider table rows remain scroll-only (no cursor concept on Providers tab)

## Implementation Approach

Single-phase implementation: all changes are localized to `proxy/tui/` (model.go, views.go, help.go) with no config or proxy-layer changes. The approach uses coordinate-based click mapping against the known fixed layout geometry.

## Phase 1: Mouse Event Handling

### Overview

Enable mouse mode, add mouse message handlers in `Update()`, implement coordinate-to-action mapping for tab clicks and config entry clicks, update help shortcuts.

### Changes Required:

#### 1. Enable mouse mode in `View()`

**File**: `proxy/tui/model.go`

**Intent**: Add `v.MouseMode = tea.MouseModeCellMotion` to `View()` so the terminal sends mouse click and scroll events to the TUI.

**Contract**: One line added after `v.AltScreen = true` (`model.go:578`):

```go
v.MouseMode = tea.MouseModeCellMotion
```

#### 2. Add mouse message cases in `Update()`

**File**: `proxy/tui/model.go`

**Intent**: Handle `tea.MouseClickMsg` and `tea.MouseWheelMsg` in the `Update()` message switch, dispatching to new handler methods.

**Contract**: Two new cases added after the `tea.KeyPressMsg` case (`model.go:202`). These cases must be placed *after* the `tea.KeyPressMsg` block but *before* the `tea.WindowSizeMsg` block:

```go
case tea.MouseClickMsg:
    return d.handleMouseClick(msg)
case tea.MouseWheelMsg:
    return d.handleMouseWheel(msg)
```

#### 3. Implement `handleMouseWheel`

**File**: `proxy/tui/model.go`

**Intent**: Delegate mouse wheel events to the existing `scrollUp()` / `scrollDown()` methods. `tea.MouseWheelUp` maps to `scrollUp()`, `tea.MouseWheelDown` maps to `scrollDown()`.

**Contract**: New method on `*Dashboard`:

```go
func (d *Dashboard) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd)
```

- If `d.showHelp` is true, ignore scroll (or optionally close help — TBD, but ignoring is simpler)
- If `d.formMode != formNone`, ignore scroll (forms are keyboard-only)
- Otherwise, switch on `msg.Button`: `tea.MouseWheelUp` → `d.scrollUp()`, `tea.MouseWheelDown` → `d.scrollDown()`

#### 4. Implement `handleMouseClick`

**File**: `proxy/tui/model.go`

**Intent**: Map click coordinates to actions based on the 3-zone layout. This is the core click routing logic.

**Contract**: New method on `*Dashboard`:

```go
func (d *Dashboard) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd)
```

Only handle `tea.MouseLeft` clicks. Route based on Y coordinate:

1. **Help modal open** (`d.showHelp`): Any click → close help (set `d.showHelp = false`)
2. **Form mode** (`d.formMode != formNone`): Ignore clicks (forms are keyboard-only)
3. **Stats bar** (Y == 0): No action
4. **Tab bar** (Y == 1): Compute which tab was clicked from X position (see tab width calculation below) → set `d.activeTab`
5. **Body region** (Y >= 3): If `d.activeTab == tabConfig`, compute which config entry is at that Y offset → set `d.configCursor` and call `d.openEditForm()`

#### 5. Tab width calculation for click mapping

**File**: `proxy/tui/views.go`

**Intent**: Export or compute the pixel width of each tab label so `handleMouseClick` can map X coordinates to tab indices. The tab labels are known strings; their rendered width = len(label) + 2 (for `Padding(0, 1)`).

**Contract**: Add a helper function `tabClickTargets(width int) []tabTarget` that returns the X start/end ranges for each tab. The function computes:
- Tab 0: `[1] Log [<level>]` — length varies with log level label
- Tab 1: `[2] Providers` — fixed length
- Tab 2: `[3] Config (...)` — fixed length

Each tab's rendered width = `len(rawLabel) + 2` (padding). Tabs are joined left-to-right via `lipgloss.JoinHorizontal`. The function returns ranges that `handleMouseClick` uses to find which tab a given X falls within.

Alternatively, the tab label strings can be accessed from `renderTabs()` — refactor to return widths alongside the rendered string, or compute widths independently using the same label format strings.

**Simplest approach**: Compute tab widths from the same format strings used in `renderTabs()`:

```go
func tabWidths(level LogFilter) []int {
    labels := []string{
        fmt.Sprintf("[1] Log [%s]", level.Label),
        "[2] Providers",
        "[3] Config (j/k=scroll Enter=edit a=+map p=+prov d=del)",
    }
    widths := make([]int, len(labels))
    for i, l := range labels {
        widths[i] = len(l) + 2 // +2 for Padding(0, 1)
    }
    return widths
}
```

Then in `handleMouseClick`, accumulate widths left-to-right to find which tab index the X coordinate falls within.

#### 6. Config entry click mapping

**File**: `proxy/tui/model.go` (in `handleMouseClick`)

**Intent**: When the user clicks in the body region while on the Config tab, map the Y coordinate to a config entry index.

**Contract**: The mapping logic:
- Body starts at row 3 (stats=1 + tab bar=2)
- Click Y offset within body = `msg.Y - 3`
- Config tab renders entries with `approxEntryLines = 6` lines each (`views.go:135`)
- Entry index = `clickYOffset / approxEntryLines` + `start` (the visible window start)
- The visible window start is computed the same way as in `renderConfigTab()` — center the cursor, so `start = cursor - visibleEntries/2`

However, the start offset depends on the current cursor position and visible window. The simplest approach: compute the visible window start from the current state (same logic as `renderConfigTab`), then add the click offset.

This requires extracting the visible-window computation into a shared helper, or duplicating the logic in `handleMouseClick`.

#### 7. Update help shortcuts

**File**: `proxy/tui/help.go`

**Intent**: Add mouse-specific shortcuts to `helpShortcuts` so users discover mouse support via the `?` help modal.

**Contract**: Append to `helpShortcuts` slice:

```go
{"", ""},  // separator
{"Mouse", ""},
{"Scroll wheel", "Scroll content"},
{"Click tab", "Switch tab"},
{"Click entry", "Edit config entry (Config tab)"},
```

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go test ./proxy/tui/...` passes (existing + new tests)
- `go build ./cmd/freedius` compiles

#### Manual Verification:

- Launch TUI: `go run ./cmd/freedius`
- Scroll wheel scrolls content on all three tabs
- Click on `[2] Providers` tab switches to Providers tab
- Click on `[3] Config` tab switches to Config tab
- Click on `[1] Log` tab switches back to Log tab
- On Config tab, click on an entry → cursor moves to that entry and edit form opens
- Press `?` → help modal shows mouse shortcuts
- Click on help modal → modal closes
- Shift+click still selects text in terminal (verify in a supporting terminal)

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful.

---

## Testing Strategy

### Unit Tests:

- `TestDashboard_MouseWheelScroll` — send `tea.MouseWheelMsg` with `tea.MouseWheelUp`/`Down` and verify `logScroll`/`providerScroll`/`configCursor` change
- `TestDashboard_MouseClickTabSwitch` — send `tea.MouseClickMsg` at Y=1 with computed X positions for each tab, verify `activeTab` changes
- `TestDashboard_MouseClickConfigEntry` — send `tea.MouseClickMsg` in body region on Config tab, verify `configCursor` moves and `formMode` opens
- `TestDashboard_MouseClickHelpCloses` — set `showHelp=true`, send click, verify `showHelp=false`
- `TestDashboard_MouseWheelInFormIgnored` — in form mode, send wheel event, verify no state change
- `TestDashboard_MouseClickInFormIgnored` — in form mode, send click, verify no state change
- `TestTabWidths` — verify `tabWidths()` returns correct widths for known log level labels

### Integration Tests:

- Manual testing of all mouse interactions in a real terminal (see Manual Verification above)

## References

- Research: `context/changes/mouse-support/research.md`
- `proxy/tui/model.go:178-257` — `Update()` message dispatch
- `proxy/tui/model.go:355-388` — `scrollUp()` / `scrollDown()`
- `proxy/tui/model.go:532-589` — `View()` with `tea.View` pattern
- `proxy/tui/views.go:16-32` — `renderTabs()` tab label rendering
- `proxy/tui/views.go:125-203` — `renderConfigTab()` with `approxEntryLines = 6`
- `proxy/tui/help.go:8-29` — `helpShortcuts` slice
- `proxy/tui/picker.go:82-97` — `providerPicker.Update()` — may need mouse event forwarding
- `charm.land/bubbletea/v2.Mouse` — `X, Y` zero-based cell coordinates
- `charm.land/bubbletea/v2.MouseClickMsg` — `.Button` identifies button type
- `charm.land/bubbletea/v2.MouseWheelMsg` — `.Button` identifies scroll direction

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Mouse Event Handling

#### Automated

- [x] 1.1 `go vet ./...` passes
- [x] 1.2 `go test ./proxy/tui/...` passes (existing + new mouse tests)
- [x] 1.3 `go build ./cmd/freedius` compiles

#### Manual

- [ ] 1.4 Scroll wheel scrolls content on all tabs
- [ ] 1.5 Click on tab labels switches tabs
- [ ] 1.6 Click on config entry opens edit form
- [ ] 1.7 Click on help modal closes it
- [ ] 1.8 Shift+click still selects text in terminal
- [ ] 1.9 Help modal shows mouse shortcuts
