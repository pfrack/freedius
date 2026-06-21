# Mouse Support for TUI — Plan Brief

> Full plan: `context/changes/mouse-support/plan.md`
> Research: `context/changes/mouse-support/research.md`

## What & Why

Add mouse click and scroll wheel support to the freedius Bubble Tea TUI dashboard. The TUI is entirely keyboard-driven today; mouse support makes it more accessible for users who prefer point-and-click interaction, especially for tab switching and config entry editing.

## Starting Point

The TUI (`proxy/tui/`) handles input exclusively via `tea.KeyPressMsg` in `Update()` — zero mouse code exists. Bubble Tea v2 has first-class mouse support via typed messages and a `MouseMode` field on `tea.View`. The TUI already uses the `tea.View` pattern with `v.AltScreen = true`, so enabling mouse mode is a one-line addition.

## Desired End State

The TUI supports scroll wheel on all tabs, click-to-switch on tab labels, click-to-edit on config entries, and click-to-dismiss on the help modal. Text selection remains possible via Shift+click (terminal-native bypass).

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Mouse mode | `MouseModeCellMotion` (clicks + scroll only) | Captures clicks/scroll without hover tracking; relies on terminal Shift+click for text selection bypass | Plan |
| Config click behavior | Move cursor + open edit form | Most intuitive single-click action; matches existing Enter key behavior | Plan |
| Text selection conflict | CellMotion only, no runtime toggle | Zero added complexity; Shift+click is the standard terminal bypass | Plan |
| Help modal | Add mouse shortcuts section | Makes mouse support discoverable without reading docs | Plan |
| Mouse mode toggle | No runtime toggle (always on) | Keeps implementation minimal; Shift+click handles the selection case | Plan |

## Scope

**In scope:**
- Enable `MouseModeCellMotion` in `View()`
- Handle `tea.MouseClickMsg` and `tea.MouseWheelMsg` in `Update()`
- Scroll wheel delegates to existing `scrollUp()`/`scrollDown()`
- Click on tab labels switches tabs
- Click on config entries opens edit form
- Click on help modal closes it
- Add mouse shortcuts to help modal

**Out of scope:**
- Hover effects (`MouseModeAllMotion`)
- Mouse-driven form interaction (text inputs remain keyboard-only)
- Provider tab click-to-select (no cursor concept on Providers tab)
- Runtime mouse mode toggle

## Architecture / Approach

Single-phase implementation localized to `proxy/tui/` (model.go, views.go, help.go). Mouse events are handled in `Update()` via two new message cases (`tea.MouseClickMsg`, `tea.MouseWheelMsg`). Click routing uses the known 3-zone layout geometry (stats=row 0, tabs=row 1, body=rows 3+). Tab width calculation uses label string lengths + lipgloss padding. Config entry mapping uses `approxEntryLines = 6` (same as `renderConfigTab`).

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Mouse Event Handling | Full mouse support: scroll, tab click, config click, help dismiss | Coordinate mapping accuracy; `bubbles/v2` list component may intercept mouse events |

**Prerequisites:** None — changes are self-contained in `proxy/tui/`.
**Estimated effort:** ~1 session, single phase.

## Open Risks & Assumptions

- **`list.Model` mouse handling**: The `bubbles/v2` list component used by the picker may auto-handle mouse events once mouse mode is enabled. If it does, picker scrolling gets mouse support "for free." If it conflicts with our click handling, we may need to filter mouse events when the picker is open.
- **Tab width accuracy**: Tab label widths depend on the log level label (e.g., `[all]` vs `[debug]`). The width computation must use the current `d.currentLogLevel.Label`, not a hardcoded string.

## Success Criteria (Summary)

- Scroll wheel scrolls content on all three tabs
- Clicking tab labels switches between tabs
- Clicking a config entry opens its edit form
- Help modal shows and dismisses with mouse
- `go test ./proxy/tui/...` passes with new mouse tests
