# Compact TUI: Merge Tabs into Topbar — Plan Brief

> Full plan: `context/changes/hide-tab-bar/plan.md`

## What & Why

Remove the separate tab bar row from the TUI and merge tab indicators (`F1:Log F2:Providers F3:Config`) into the stats bar (topline). Change tab shortcuts from `1`/`2`/`3` to `F1`/`F2`/`F3` to avoid conflicts with form text input. The layout gains 2 extra rows for body content.

## Starting Point

The TUI renders three chrome rows: stats bar (1 row) + tab labels (1 row) + tab bar border (1 row), consuming `height - 3` of the terminal for body content. Tab switching uses bare `1`/`2`/`3` keys, which conflict with text input in forms. The `renderTabs` function builds the separate tab bar; `renderStatsBar` builds the stats line independently.

## Desired End State

A single topbar line with stats on the left and tab indicators on the right:

```
 uptime: 5s │ requests: 10 │ errors: 0 │ error rate: 0.0%        F1:Log F2:Providers F3:Config
```

`F1`/`F2`/`F3` switch tabs. `Tab`/`Shift+Tab` cycles. Typing `1` in a form inserts the character. Body is 2 rows taller.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
| --- | --- | --- |
| Tab shortcut keys | `F1`/`F2`/`F3` | Function keys never conflict with form text input; standard terminal convention. |
| Tab indicator placement | Right-aligned in stats bar | Stats stay left for readability; tabs on right for discoverability without visual clutter. |
| Keep `renderTabs` function | Yes, remove call only | Pure function with unit test; useful for future re-enablement. |
| Help modal content | Update shortcut labels only | Already documents tab switching; just change `1/2/3` → `F1/F2/F3`. |

## Scope

**In scope:**
- Merge tab indicators into `renderStatsBar` (right-aligned)
- Remove `renderTabs` call from `View()`, reclaim 2 body rows
- Change tab key bindings from `1`/`2`/`3` to `F1`/`F2`/`F3`
- Update help shortcuts list
- Update layout and key-press tests

**Out of scope:**
- Changes to tab switching logic
- Changes to form mode or picker behavior
- Changes to scroll behavior or tab content rendering
- `renderTabs` function removal

## Architecture / Approach

Modify `renderStatsBar` to accept `activeTab` and `logLevel` parameters and render compact tab indicators right-aligned on the same line. Remove the `renderTabs` call from `Dashboard.View()` and change `bodyHeight` from `height - 3` to `height - 1`. In `handleTabModeKeyPress`, replace `case "1":`/`"2":`/`"3":` with `case "f1":`/`"f2":`/`"f3":`. Update tests for new key codes and layout assertions.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Merge tabs into topbar | Single topbar + F-key shortcuts + 2 extra body rows | Low — rendering + key binding change, no logic affected |
| 2. Test updates | Passing tests with new layout and key codes | Low — mechanical test adaptation |

**Prerequisites:** None
**Estimated effort:** ~15 minutes, two phases

## Open Risks & Assumptions

- **F-key terminal support**: Some terminals or SSH sessions may not pass F1/F2/F3 correctly. Bubble Tea handles this via its key detection layer; standard modern terminals (xterm, iTerm2, Terminal.app, Windows Terminal) all support F-keys. Edge case: tmux may need configuration.

## Success Criteria (Summary)

- `freedius` starts with a single topbar (stats left, tabs right)
- `F1`/`F2`/`F3` switch tabs; `Tab`/`Shift+Tab` cycles
- Typing `1`/`2`/`3` in forms inserts characters (no tab switch)
- Body area is 2 rows taller
