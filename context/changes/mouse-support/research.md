---
date: 2026-06-21T18:00:00+02:00
researcher: opencode
git_commit: 76806dc67bf4ecad65dd11f8ab46859dad5b2213
branch: providers
repository: freedius
topic: "Mouse support for the freedius TUI"
tags: [research, codebase, tui, bubbletea, mouse, input]
status: complete
last_updated: 2026-06-21
last_updated_by: opencode
---

# Research: Mouse Support for the freedius TUI

**Date**: 2026-06-21T18:00:00+02:00
**Researcher**: opencode
**Git Commit**: 76806dc67bf4ecad65dd11f8ab46859dad5b2213
**Branch**: providers
**Repository**: freedius

## Research Question

How can mouse support (click, scroll wheel) be added to the freedius Bubble Tea TUI?

## Summary

The freedius TUI (`proxy/tui/`) is entirely keyboard-driven — zero mouse-related code exists anywhere in the codebase. The TUI uses **Bubble Tea v2** (`charm.land/bubbletea/v2 v2.0.7`), which has first-class mouse support via typed mouse messages and a View-level mouse mode field. Adding mouse support requires: (1) enabling mouse mode in `View()`, (2) handling `tea.MouseClickMsg` / `tea.MouseWheelMsg` in `Update()`, and (3) tracking which screen region was clicked to map clicks to actions. The changes are localized to `proxy/tui/model.go` and `proxy/tui/views.go` with no config or proxy-layer changes needed.

## Detailed Findings

### 1. Current Input Model — Keyboard Only

The TUI handles input exclusively through `tea.KeyPressMsg` in the `Update()` method (`proxy/tui/model.go:202-228`). The message dispatch is:

```
tea.KeyPressMsg → (if showHelp) → close help
                → (if formMode != formNone) → handleFormKeyPress / handleDeleteConfirmKeyPress
                → handleTabModeKeyPress
```

There are no `tea.MouseClickMsg`, `tea.MouseWheelMsg`, `tea.MouseMotionMsg`, or `tea.MouseReleaseMsg` cases anywhere in the codebase.

### 2. Bubble Tea v2 Mouse API

Bubble Tea v2 provides four mouse message types, all implementing the `tea.MouseMsg` interface:

| Message Type | Trigger | Key Fields |
|---|---|---|
| `tea.MouseClickMsg` | Button pressed | `.Button` (tea.MouseLeft/MouseRight/MouseMiddle), `.Mouse().X`, `.Mouse().Y` |
| `tea.MouseReleaseMsg` | Button released | Same fields |
| `tea.MouseWheelMsg` | Scroll wheel | `.Button` (tea.MouseWheelUp/Down/Left/Right) |
| `tea.MouseMotionMsg` | Mouse moved (all-motion mode) | Same fields |

Mouse mode is enabled in `View()` via a field on `tea.View`:

```go
func (m model) View() tea.View {
    v := tea.NewView("...")
    v.MouseMode = tea.MouseModeCellMotion  // click + scroll only
    // or tea.MouseModeAllMotion           // includes movement tracking
    return v
}
```

The freedius TUI already uses the `tea.View` pattern (`proxy/tui/model.go:532-589`) with `v.AltScreen = true`, so adding mouse mode is a one-line change.

### 3. TUI Layout Geometry (for click mapping)

The TUI has a fixed 3-zone layout rendered in `View()` (`model.go:579`):

```
[stats bar]    ← row 0 (1 row)
[tab bar]      ← row 1 (1 row + border = 2 rows)
[body content] ← rows 3..height-1
```

- **Tab bar**: 3 tabs rendered horizontally via `lipgloss.JoinHorizontal` (`views.go:30`)
- **Body height**: `height - 3` rows (`model.go:551`)
- **Config tab cursor**: uses `configCursor` + visible window with `approxEntryLines = 6` per entry (`views.go:135`)

Click-to-action mapping needs to account for:
- Stats bar height (1 row)
- Tab bar height (2 rows: label + border)
- Within body: active tab determines what the Y coordinate means

### 4. Actions Mappable to Mouse Events

| Mouse Event | Potential Action | Affected Tab(s) |
|---|---|---|
| Click on tab label | Switch to that tab | All |
| Scroll wheel up | `scrollUp()` | All (log scroll, provider scroll, config cursor) |
| Scroll wheel down | `scrollDown()` | All |
| Click on config entry | Set `configCursor` to clicked entry | Config |
| Click on provider row | Select that provider row | Providers |
| Click on help modal | Close help (like Esc) | Help overlay |

### 5. Files That Need Changes

#### `proxy/tui/model.go` — Update() handler + View() mouse mode

**Add mouse message cases** in `Update()` after the `tea.KeyPressMsg` case (~line 202):

```go
case tea.MouseClickMsg:
    return d.handleMouseClick(msg)
case tea.MouseWheelMsg:
    return d.handleMouseWheel(msg)
```

**Enable mouse mode** in `View()` (~line 586):

```go
v.MouseMode = tea.MouseModeCellMotion
```

**New handler methods**:
- `handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd)` — map click coordinates to actions
- `handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd)` — delegate to existing `scrollUp()`/`scrollDown()`

#### `proxy/tui/views.go` — Tab position tracking

To map X coordinates to tab indices, the tab bar needs to track the rendered width of each tab. This could be computed from the tab label strings (which are known constants), or by storing the rendered widths after `lipgloss.JoinHorizontal`.

#### `proxy/tui/help.go` — Optional: mouse shortcuts in help modal

If mouse support is added, the help modal should list mouse actions (e.g., "Scroll wheel: scroll content").

### 6. Implementation Approach

The simplest MVP approach:

1. **Enable mouse mode**: `v.MouseMode = tea.MouseModeCellMotion` in `View()`
2. **Handle scroll wheel**: `tea.MouseWheelMsg` → call existing `scrollUp()`/`scrollDown()` — works on all tabs, zero geometry needed
3. **Handle tab clicks**: `tea.MouseClickMsg` with Y=1 (tab bar row) → compute which tab was clicked from X position and tab label widths
4. **Handle config entry clicks**: `tea.MouseClickMsg` in body region on Config tab → compute which entry is at that Y offset

### 7. Risks and Considerations

- **No `tea.MouseMsg` handler in `bubbles/v2` components**: The `list.Model` used by `providerPicker` (`picker.go:24`) may or may not handle mouse events internally. If it does, mouse clicks on the picker may "just work" once mouse mode is enabled. If not, the picker would need manual mouse handling.
- **Text input fields**: `textinput.Model` from `bubbles/v2` may not handle mouse clicks for cursor positioning within the field. This is a lower-priority enhancement.
- **Coordinate system**: Bubble Tea coordinates are 0-based cell coordinates (column, row). Tab bar is at row 1 (below stats bar). Body starts at row 3.
- **Alt screen**: The TUI uses `v.AltScreen = true` (`model.go:587`), which means mouse coordinates are relative to the terminal viewport, not the scrollback.

## Code References

- `proxy/tui/model.go:178-257` — `Update()` message dispatch (keyboard-only)
- `proxy/tui/model.go:280-354` — `handleTabModeKeyPress()` with all tab-mode shortcuts
- `proxy/tui/model.go:356-392` — `scrollUp()` / `scrollDown()` (reusable for mouse wheel)
- `proxy/tui/model.go:532-589` — `View()` with `tea.View` pattern (add `MouseMode` here)
- `proxy/tui/model.go:546-551` — Layout geometry (stats=1 row, tabs=2 rows, body=rest)
- `proxy/tui/views.go:16-32` — `renderTabs()` — tab label rendering (needed for click-to-tab mapping)
- `proxy/tui/views.go:386-401` — `renderHelpModal()` — modal dimensions for click detection
- `proxy/tui/picker.go:82-97` — `providerPicker.Update()` — may need mouse event forwarding
- `proxy/tui/help.go:8-29` — `helpShortcuts` — add mouse shortcuts here
- `go.mod:7` — `charm.land/bubbletea/v2 v2.0.7` — confirms v2 mouse API availability

## Architecture Insights

1. **View-level feature flags**: Bubble Tea v2 moved all terminal feature flags (alt screen, mouse mode, bracketed paste) from `tea.NewProgram` options to `View()` return fields. The freedius TUI already follows this pattern.

2. **Scroll delegation pattern**: All three tabs (Log, Providers, Config) share `scrollUp()` / `scrollDown()` methods that switch on `d.activeTab`. Mouse wheel events can delegate to the same methods with no per-tab logic.

3. **Coordinate-based click mapping**: Unlike keyboard shortcuts which are context-free, mouse clicks require knowing the rendered layout geometry. The TUI has a fixed layout (stats → tabs → body), so coordinate mapping is straightforward but must be kept in sync with `renderTabs()` and `renderStatsBar()`.

4. **Bubble components may auto-handle mouse**: `list.Model` (used in the picker) and `textinput.Model` (used in forms) are from `bubbles/v2`. They may internally handle mouse events once mouse mode is enabled. This needs testing — if they do, picker scrolling and text cursor positioning get mouse support "for free."

## Historical Context (from prior changes)

- `context/changes/tui-themes/plan.md` — Documents the TUI architecture, `Dashboard` struct, and `View()` pattern. The mouse mode addition follows the same pattern as `v.AltScreen = true` and theme-related View changes.
- `context/changes/missing-providers-tui/research.md` — Documents how the TUI provider picker and views work, useful context for click mapping.
- `context/changes/tui-themes/research.md` — Documents lipgloss styling and the `Styles` struct used throughout the TUI.

## Related Research

- `context/changes/tui-themes/research.md` — Theme system research
- `context/changes/missing-providers-tui/research.md` — Provider display research

## Open Questions

1. **Do `bubbles/v2` components handle mouse internally?** Need to check if `list.Model` and `textinput.Model` already respond to mouse messages. If so, enabling mouse mode may give the picker and forms mouse support automatically.

2. **Mouse mode preference**: Should there be a config option to disable mouse support? Some terminal users prefer pure keyboard workflows and find mouse capture annoying (it prevents text selection in some terminals).

3. **AllMotion vs CellMotion**: `MouseModeCellMotion` captures clicks and scrolls. `MouseModeAllMotion` also captures mouse movement (needed for hover effects). CellMotion is sufficient for the MVP; AllMotion could enable hover-highlight on config entries later.

4. **Text selection conflict**: Enabling mouse mode in Bubble Tea captures mouse events, which prevents the user from selecting text in the terminal with the mouse. This is a UX tradeoff — some users may want to copy text from the log tab. Consider adding a way to temporarily disable mouse capture (e.g., hold Shift to select, which most terminal emulators support natively).
