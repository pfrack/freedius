---
date: 2026-06-20T20:36:16+02:00
researcher: research-agent
git_commit: ada3e6e
branch: main
repository: pfrack/freedius
topic: "Pin stats bar to top of TUI, move tabs below it, add `?` keyboard shortcuts modal"
tags: [research, codebase, tui, bubble-tea, lipgloss, modal, layout, shortcuts]
status: complete
last_updated: 2026-06-20
last_updated_by: research-agent
---

# Research: Top-pinned stats bar + `?` keyboard shortcuts modal

**Date**: 2026-06-20T20:36:16+02:00
**Researcher**: research-agent
**Git Commit**: ada3e6e
**Branch**: main
**Repository**: pfrack/freedius

## Research Question

The user wants two related TUI changes in the Bubble Tea dashboard:

1. **Layout:** the bottom stats bar (uptime, requests, errors, error rate,
   transient status message) should be moved to the top of the screen, kept
   always visible, with the tab switcher shown directly below it and the
   active tab's content filling the rest of the height.
2. **Modal:** a keyboard shortcuts help modal should be added, opened with
   `?`, listing every existing binding grouped by context.

User-decided scope (from clarifying questions): stats on top, then tabs, then
content; modal triggered by `?`; modal shows all current shortcuts grouped by
context.

## Summary

The two changes are independent, small, and have well-contained blast radius.
No external dependencies need to be added.

- **Layout refactor** is a one-argument-order change to the Sprintf at
  `proxy/tui/model.go:493`. The `bodyHeight := height - 3` math at
  `proxy/tui/model.go:471` stays correct because the total reserved rows
  remain 3 (1 for the stats row + 2 for the tab bar's label row and its
  bottom border row). Zero existing tests depend on the order of the
  composed `View()` output, so no test updates are required for the layout
  move itself.
- **Modal** uses the idiomatic `lipgloss.Place(width, height, Center, Center,
  modal, lipgloss.WithWhitespaceStyle(...))` overlay from lipgloss v2. The
  existing `d.showPicker` boolean at `proxy/tui/model.go:101` is the pattern
  to mirror: a new `d.showHelp` field toggled by a `?` case in
  `handleTabModeKeyPress` (open) and a help-capture block at the top of the
  `KeyPressMsg` switch in `Update` (close on `?` or `esc`). Total new code
  is roughly 160 lines across 5 files, no new dependencies.

`?` is verified unbound across every `case` in the package, so there is no
key collision to resolve.

## Detailed Findings

### 1. Current View() composition (proxy/tui/model.go:457-497)

The relevant lines of `Dashboard.View`:

```go
// proxy/tui/model.go:489-496
tabs  := renderTabs(d.activeTab, width)
stats := renderStatsBar(d.stats, width)
body  := windowStyle.Width(max(width-2, 0)).Render(content)

result := fmt.Sprintf("%s\n%s\n%s", tabs, body, stats)
v := tea.NewView(result)
v.AltScreen = true
return v
```

And the bodyHeight math:

```go
// proxy/tui/model.go:471
bodyHeight := height - 3
```

The `-3` accounts for:

| Row | Source | Lines consumed |
|---|---|---|
| 1 | `renderTabs` content (the joined `[1] Log [2] Providers [3] Config ...` row) | 1 |
| 2 | `tabBarStyle` bottom border (`Border(NormalBorder(), false, false, true, false)`) | 1 |
| 3 | `renderStatsBar` (1 line, no border, see `statsBarStyle` at `proxy/tui/styles.go:26-28`) | 1 |
| — | literal `\n` separators in the Sprintf | 0 |

Total = 3 reserved rows, leaving `bodyHeight` rows for the body content. The
`tabBarStyle` border was confirmed against
`charm.land/lipgloss/v2@v2.0.4/borders.go:413-419`: a `hasBottom=true` border
adds exactly 1 `\n` + 1 line of border characters, contributing 1 terminal row.

### 2. New layout — bodyHeight math still correct

The new composition swaps the order: `stats → tabs → body`. The reserved-row
count is unchanged:

- 1 row for `renderStatsBar` (now at the top instead of the bottom)
- 2 rows for `renderTabs` (label + bottom border)
- N rows for the body, where N = `bodyHeight = height - 3`

So `bodyHeight := height - 3` at `proxy/tui/model.go:471` stays exactly as
is. The only code change required is the Sprintf at line 493 — the three
arguments reorder, nothing else moves.

**Proposed new View() layout block** (replaces `proxy/tui/model.go:489-496`):

```go
stats := renderStatsBar(d.stats, width)
tabs  := renderTabs(d.activeTab, width)
body  := windowStyle.Width(max(width-2, 0)).Render(content)

result := fmt.Sprintf("%s\n%s\n%s", stats, tabs, body)
v := tea.NewView(result)
v.AltScreen = true
return v
```

**Minimum terminal height:** both the old and the new layout need height ≥ 3
to render without overflow. Below that, both overflow by the same amount
(`3 - height` rows). The default fallback at `proxy/tui/model.go:467-470`
clamps `height` to 24 when `d.height <= 0`, so the only way to hit the
overflow case is a real 1- or 2-row terminal resize.

### 3. Form mode, picker, and active-tab state — all preserved

- **Form mode** still renders the form as the body content via
  `renderForm(d, width, bodyHeight)` at `proxy/tui/model.go:475`. The form
  body lives between the (now top) stats bar and... well, fills the body
  section. Stats bar remains visible. The tab bar also remains visible
  (showing the active tab as decorative chrome in form mode), which keeps
  the layout visually consistent across modes. No `formMode` branch is
  needed in the new layout block.
- **Picker overlay** continues to work because `renderForm` is called
  identically — the picker is composed into the form body, not at the
  dashboard level. See `proxy/tui/views.go:351-356` (where `d.picker.View()`
  replaces a field's view). Path is body-content-agnostic, so the layout
  reorder does not affect it.
- **Active tab state** (`d.activeTab`, `d.logScroll`, `d.providerScroll`,
  `d.configCursor`, `d.formMode`, form fields) is untouched by the layout
  change. The new layout is purely a `View()`-time rendering decision.

### 4. `stats.message` lifecycle is independent of position

`stats.message` is a transient string set by `installShellRC`
(`proxy/tui/model.go:230, 239, 242, 245`), `toggleVerboseErrors`
(`proxy/tui/model.go:359, 361`), and cleared on the next request event at
`proxy/tui/model.go:219`. It is rendered at `proxy/tui/views.go:232`
inside `renderStatsBar`. Moving the stats bar to the top changes only the
on-screen y-coordinate of the message; the lifecycle, the field, and the
renderer are all unchanged. The existing test
`TestDashboard_RequestEventClearsStatusMessage` at
`proxy/tui/model_test.go:797-805` continues to pass without modification.

### 5. Complete keyboard shortcut inventory

Every `case` label in the TUI was enumerated across `proxy/tui/model.go` and
`proxy/tui/picker.go`. Here is the complete list, grouped by context (this
is the data the modal will display):

**Global (active whenever no form/picker is open):**
- `q` — Quit — `proxy/tui/model.go:250`
- `ctrl+c` — Quit — `proxy/tui/model.go:250`
- `esc` — Quit when no form is active — `proxy/tui/model.go:187`
- `tab` — Next tab — `proxy/tui/model.go:262`
- `shift+tab` — Previous tab — `proxy/tui/model.go:265`
- `ctrl+e` — Toggle verbose errors — `proxy/tui/model.go:300`
- `?` — **NEW** open help modal

**Tab switching (also global, but only meaningful as direct switches):**
- `1` — Switch to Log — `proxy/tui/model.go:253`
- `2` — Switch to Providers — `proxy/tui/model.go:256`
- `3` — Switch to Config — `proxy/tui/model.go:259`

**Log tab:**
- `j` / `down` — Scroll down (newer events) — `proxy/tui/model.go:271`
- `k` / `up` — Scroll up (older events) — `proxy/tui/model.go:268`

**Providers tab:**
- `j` / `down` — Scroll provider list down — `proxy/tui/model.go:271`
- `k` / `up` — Scroll provider list up — `proxy/tui/model.go:268`

**Config tab:**
- `j` / `down` — Move cursor down — `proxy/tui/model.go:271`
- `k` / `up` — Move cursor up — `proxy/tui/model.go:268`
- `e` / `enter` — Edit selected entry — `proxy/tui/model.go:274`
- `a` — Add new mapping — `proxy/tui/model.go:279`
- `p` — Add new provider — `proxy/tui/model.go:284`
- `d` — Delete selected entry (opens delete-confirm) — `proxy/tui/model.go:289`
- `ctrl+s` — Install shell RC — `proxy/tui/model.go:303`

**Form mode (provider / mapping):**
- `tab` — Next form field — `proxy/tui/model.go:378`
- `shift+tab` — Previous form field — `proxy/tui/model.go:382`
- `enter` — On `provider`/`behavior` field opens picker; otherwise validates and submits — `proxy/tui/model.go:386`
- `esc` — Cancel form, return to underlying tab — `proxy/tui/model.go:411`

**Delete confirm:**
- `y` — Confirm delete (saves config under lock) — `proxy/tui/model.go:426`
- `n` — Cancel delete — `proxy/tui/model.go:450`
- `esc` — Cancel delete — `proxy/tui/model.go:450`

**Picker (provider / behavior):**
- `enter` — Select highlighted item, write into focused form field — `proxy/tui/picker.go:83`
- `esc` — Close picker without selecting — `proxy/tui/picker.go:88`
- Bubble list default navigation keys (forwarded to `p.list.Update` at `proxy/tui/picker.go:93`):
  `↑` / `k`, `↓` / `j`, `←` / `h` / `pgup` / `b` / `u`, `→` / `l` / `pgdn` / `f` / `d`,
  `home` / `g`, `end` / `G`. Filtering is disabled (`proxy/tui/picker.go:47, 74`)
  and the help row is hidden (`proxy/tui/picker.go:48, 75`).

### 6. `?` is unbound — safe to use for the new modal

A full enumeration of every `case` label in the package (Grep across
`proxy/tui`) returned 17 matches. `?` appears in none of them. The only
shift-modified key explicitly bound is `shift+tab` (`proxy/tui/model.go:265, 382`).
On a US keyboard `?` is `Shift+/`; no existing case matches `?` or any
shift-modified printable.

**Caveat:** when the picker is active, keypresses fall through
`providerPicker.Update` to `p.list.Update` at `proxy/tui/picker.go:93`. The
bubble list's default keymap includes `?` for `ShowFullHelp` /
`CloseFullHelp` (`charm.land/bubbles/v2@v2.1.0/list/keys.go:81-88`), which
would flip `m.Help.ShowAll`. Because the picker sets
`SetShowHelp(false)` (`proxy/tui/picker.go:48, 75`), no help chrome is
visible — the visible result of pressing `?` in the picker is a no-op.
The new `?` binding should therefore live **only in tab mode** (i.e. inside
`handleTabModeKeyPress`), so it never fires while the picker is open. This
also prevents `?` from being intercepted while the user is typing it into a
form field (it remains a valid character for URLs, model names, etc.).

### 7. Form footer has a latent bug — flag for follow-up

The form footer at `proxy/tui/views.go:370`:

```go
footer := statusClientErrStyle.Render("Enter=Save  Esc=Cancel  Tab=Next Field  Ctrl+D=Delete")
```

says `Ctrl+D=Delete`, but **no `case "ctrl+d"` exists anywhere in the
repository** (verified by Grep across the whole repo — the only mention
of "Ctrl+D" is this footer string). The actual delete flow is two steps:
press `d` on the Config tab to enter delete-confirm
(`proxy/tui/model.go:289-298`), then `y`/`n` in the confirm
(`proxy/tui/model.go:424-453`). The footer is therefore misleading on two
counts: the key is not bound, and there is no in-form delete.

**Recommended handling for this change:** drop `Ctrl+D=Delete` from the
footer at `proxy/tui/views.go:370` so the remaining three labels
(`Enter=Save`, `Esc=Cancel`, `Tab=Next Field`) are accurate. If in-form
deletion is desired, that is a separate small change.

### 8. Modal implementation — `lipgloss.Place` overlay

The idiomatic Charm approach to centering a box on top of an existing
rendered screen is `lipgloss.Place`. From the local v2 source
(`charm.land/lipgloss/v2@v2.0.4/position.go:36`):

```go
func Place(width, height int, hPos, vPos Position, str string, opts ...WhitespaceOption) string
```

with `Position` constants `Top`, `Bottom`, `Center`, `Left`, `Right`
(`position.go:26-32`).

The v2 API consolidated v1's `WithWhitespaceBackground` and
`WithWhitespaceForeground` into a single
`WithWhitespaceStyle(s lipgloss.Style)`
(`charm.land/lipgloss/v2@v2.0.4/whitespace.go:65`, upgrade notes in
`UPGRADE_GUIDE_V2.md:339-353`).

**Recommended overlay helper** (new, `proxy/tui/views.go` or a new
`proxy/tui/overlay.go`):

```go
func overlayModal(base, modal string, width, height int) string {
    return lipgloss.Place(width, height,
        lipgloss.Center, lipgloss.Center,
        modal,
        lipgloss.WithWhitespaceStyle(
            lipgloss.NewStyle().Background(lipgloss.Color("0")),
        ),
    )
}
```

How it works: `Place` measures `modal`'s content height. If it fits in
`height`, it returns a `(width × height)` string with the modal centered
and every other cell filled by the whitespace style (a single space styled
with `Background("0")` = black). The `base` parameter is currently
discarded — the dashboard content is overwritten by the next frame
anyway. (If a dimmed look-behind is desired in the future, `base` can
be passed through `WithWhitespaceChars` + a custom whitespace
renderer, but for the MVP we discard it.)

### 9. New state and dispatch changes for the modal

**Field on `Dashboard` struct** — add at `proxy/tui/model.go:101` (next to
`showPicker`):

```go
showHelp bool
```

**KeyPressMsg dispatch** — insert a new block at the **top** of the
`case tea.KeyPressMsg:` in `Update` at `proxy/tui/model.go:185-203`,
before the existing branches:

```go
case tea.KeyPressMsg:
    // (0) Help modal: capture every key while open.
    if d.showHelp {
        switch msg.String() {
        case "?", "esc":
            d.showHelp = false
        }
        return d, nil
    }

    // (1) existing esc-quit
    // (2) existing delete-confirm
    // (3) existing form-mode
    // (4) existing tab-mode
    ...
```

Because the early `return d, nil` swallows the message, **no key
propagates to the form/tab dispatchers while help is open**, which
preserves dashboard state and prevents accidental tab-switching / quit /
form-opening.

**Tab-mode opener** — add to `handleTabModeKeyPress` at
`proxy/tui/model.go:248-311`:

```go
case "?":
    d.showHelp = true
    return d, nil
```

This is only reached when `formMode == formNone` and `showHelp == false`
(both conditions are enforced by the existing dispatch), so `?` opens
help only in tab mode — never inside a form, never inside the picker,
never while help is already open. Toggling with a second `?` works
automatically: the second `?` lands in the new (0) block and flips
`showHelp` to false.

### 10. New View() composition for the modal

After the layout reorder from section 2, append one overlay step at the
end of `Dashboard.View` (in `proxy/tui/model.go:457-497`):

```go
result := fmt.Sprintf("%s\n%s\n%s", stats, tabs, body)

if d.showHelp {
    modal := renderHelpModal(width)
    result = overlayModal(result, modal, width, height)
}

v := tea.NewView(result)
v.AltScreen = true
return v
```

### 11. New `renderHelpModal` function (proposed)

In `proxy/tui/views.go` (or a new `proxy/tui/help.go` — recommendation
below):

```go
const modalMaxWidth = 60

func modalWidthFor(terminalWidth int) int {
    w := terminalWidth * 60 / 100
    return min(max(w, 40), modalMaxWidth)
}

func renderHelpModal(terminalWidth int) string {
    title := modalTitleStyle.Render(" Keyboard Shortcuts ")
    var rows []string
    for _, s := range helpShortcuts {
        key := shortcutKeyStyle.Width(14).Render(s.key)
        desc := shortcutDescStyle.Render(s.desc)
        rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, key, desc))
    }
    body := lipgloss.JoinVertical(lipgloss.Left, rows...)
    footer := modalFooterStyle.Render("Press ? or Esc to close")
    sep := separatorStyle.Render(strings.Repeat("─", modalWidthFor(terminalWidth)-2))

    return modalStyle.
        Width(modalWidthFor(terminalWidth)).
        Render(lipgloss.JoinVertical(lipgloss.Left, title, sep, body, "", footer))
}
```

`min` and `max` are Go 1.21+ builtins. The project already uses `max`
extensively (e.g. `proxy/tui/views.go:31, 102, 142, 374, 380` and
`proxy/tui/model.go:491`).

### 12. New styles to add (proxy/tui/styles.go:7-49)

```go
modalStyle = lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("4")).
    Padding(1, 2)

modalTitleStyle = lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("4")).
    Padding(0, 1)

modalFooterStyle = lipgloss.NewStyle().
    Faint(true).
    Italic(true)

shortcutKeyStyle = lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("6"))

shortcutDescStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("7"))
```

Colors reuse the palette already in use (`"1"`, `"2"`, `"3"`, `"4"`, `"6"`,
`"7"`, `"8"`). Exact values can be tuned during implementation.

### 13. Proposed shortcut data structure

A new file `proxy/tui/help.go` (per the per-concern file pattern that
`picker.go` already follows) holding:

```go
package tui

type shortcut struct {
    key  string
    desc string
}

var helpShortcuts = []shortcut{
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
    // --- Form mode ---
    {"Tab",               "Next form field"},
    {"Shift+Tab",         "Previous form field"},
    {"Enter",             "Save / open picker"},
    {"Esc",               "Cancel form"},
    // --- Delete confirm ---
    {"y / n",             "Confirm / cancel delete"},
    // --- Picker ---
    {"Enter / Esc",       "Select / cancel (picker)"},
}
```

The same 12-onward rows can be conditionally shown or grouped by a
section header in the renderer if the user later asks for visual
grouping in the modal; the data structure already supports it.

### 14. Tests to add (proxy/tui/model_test.go)

| Test name | What it asserts |
|---|---|
| `TestDashboard_HelpModal_OpensWithQuestionMark` | `?` sets `d.showHelp = true` |
| `TestDashboard_HelpModal_EscCloses` | `esc` while open sets `d.showHelp = false` |
| `TestDashboard_HelpModal_QuestionMarkToggles` | `?` then `?` opens then closes |
| `TestDashboard_HelpModal_CapturesTabSwitchKey` | With help open, pressing `2` does NOT change `d.activeTab` |
| `TestDashboard_HelpModal_ViewContainsTitle` | With help open, `View()` output contains "Keyboard Shortcuts" |
| `TestDashboard_HelpModal_NotOpenedInForm` | Opening a form, then pressing `?`, does NOT set `d.showHelp` (verifies the `?` opener is scoped to tab mode) |
| `TestDashboard_Layout_StatsAboveTabs` | Optional regression test: `strings.Index(out, "uptime:") < strings.Index(out, "[1] Log")` — locks in the new ordering so future refactors don't silently regress the layout |
| `TestOverlayModal_CentersContent` | Unit test for the `overlayModal` helper: input of `80×24` returns a 24-line string with the modal content on line 12 (center) |

The 5 modal tests use the existing helpers `newTestDashboard`
(`proxy/tui/model_test.go:25`), `tea.KeyPressMsg` constructors
(`proxy/tui/model_test.go:39-44`), `stripANSI` (`proxy/tui/model_test.go:691-709`),
and `viewContent` (`proxy/tui/model_test.go:807-809`).

### 15. Tests that must NOT need updating

A scan of every `d.View()` call site in `proxy/tui/model_test.go` (4 sites
at lines 863, 896, 916, 928) found that all assertions are `strings.Contains`
checks for body content (provider names, entry names, "No requests" markers)
and do not depend on the position of stats or tab elements. The 4 tests
that exercise the view directly:

- `TestDashboard_ConfigTabScrollsToCursor` (line 833)
- `TestDashboard_ProvidersTabScroll` (line 870, three View() calls)

...all continue to pass after the layout reorder. Helpers called directly
(`renderTabs`, `renderLogTab`) are also unaffected.

## Code References

- `proxy/tui/model.go:457-497` — `Dashboard.View`, the function to modify
  for both changes
- `proxy/tui/model.go:471` — `bodyHeight := height - 3` (unchanged)
- `proxy/tui/model.go:489-496` — current Sprintf composition to reorder
- `proxy/tui/model.go:185-203` — `KeyPressMsg` dispatch (insert help-modal
  block at top)
- `proxy/tui/model.go:248-311` — `handleTabModeKeyPress` (add `?` case
  for opener)
- `proxy/tui/model.go:101-102` — `showPicker` / `picker` (mirror as
  `showHelp`)
- `proxy/tui/views.go:16-32` — `renderTabs` (unchanged)
- `proxy/tui/views.go:219-241` — `renderStatsBar` (unchanged)
- `proxy/tui/views.go:338-376` — `renderForm` (unchanged)
- `proxy/tui/views.go:370` — form footer with the latent `Ctrl+D` bug
- `proxy/tui/styles.go:7-49` — `var ( ... )` block to extend with modal
  styles
- `proxy/tui/styles.go:26-28` — `statsBarStyle` (no border, 1-line output)
- `proxy/tui/styles.go:30-32` — `tabBarStyle` (bottom border, 2-line
  output)
- `proxy/tui/picker.go:23-99` — `providerPicker` (reference pattern for
  overlay sub-component)
- `proxy/tui/model_test.go:25, 39-44, 691-709, 807-809` — test helpers
  reusable for the new tests
- `go.mod:6-8` — already-pinned `charm.land/bubbles/v2 v2.1.0`,
  `charm.land/bubbletea/v2 v2.0.7`, `charm.land/lipgloss/v2 v2.0.4`
- `charm.land/lipgloss/v2@v2.0.4/position.go:26-36` — `Place` API
- `charm.land/lipgloss/v2@v2.0.4/borders.go:413-419` — bottom-border
  row-cost verification
- `charm.land/lipgloss/v2@v2.0.4/whitespace.go:65` — `WithWhitespaceStyle`
- `charm.land/bubbles/v2@v2.1.0/list/keys.go:34-96` — bubble list
  default keymap (forwarded by picker, not by freedius)

## Architecture Insights

- **The existing `showPicker` flag is the design template** for the new
  `showHelp` flag. Both are booleans, both gate a sub-view on the
  dashboard, both are toggled in tab-mode (or form-mode for picker) key
  handlers. Following the same pattern keeps the codebase consistent and
  makes the new code reviewable in isolation.
- **`bodyHeight := height - 3` is a conservative budget** that happens to
  work for both the old and new layouts because the chrome composition is
  symmetric (1 + 2 rows regardless of order). This is a happy accident
  that means no recomputation is needed, but it is also a small risk: if
  a future change adds a row to either the stats bar or the tab bar, the
  budget will silently overflow. A comment explaining the math (1 + 2
  reserved rows) would help future maintainers — propose adding this as
  part of the layout refactor.
- **The dashboard has only one true overlay today** (the picker, which
  is body-scoped, not screen-scoped). The help modal is the first
  screen-scoped overlay. The `lipgloss.Place` pattern is reusable for
  any future dialog (about, confirm-quit, etc.).
- **`min` and `max` builtins** (Go 1.21+) are already used freely; no
  utility import is needed for the modal-sizing code.

## Historical Context

No prior research or change in the repository touches the stats-bar
position or any modal/overlay work. The closest related changes:

- `context/changes/tui-dashboard/research.md` — the original TUI-vs-Web-vs-Native
  decision that established Bubble Tea as the v2 UI; documents the
  TUI as the primary surface but does not discuss layout details.
- `context/changes/unified-server-logs-tab/` — the most recent TUI
  change (the request log got replaced with raw server access logs).
  Touched `model.go` (event handling) and `views.go` (renderer); did not
  modify the chrome composition.
- `context/changes/tui-config-setup/` — added the form mode, picker,
  delete-confirm, and the `?`-less footer string. The form footer bug
  documented in section 7 originated here.

## Related Research

- `context/changes/tui-dashboard/research.md` — framework selection for
  the TUI (Bubble Tea vs. Web UI vs. Native GUI)
- `context/changes/unified-server-logs-tab/` — most recent TUI change
  (server log tab)
- `context/changes/tui-config-setup/` — added the form/picker/delete UI
  that the help modal must not interfere with

## Open Questions

1. **Form footer `Ctrl+D=Delete` bug** — should this change drop the
   misleading text (3 LOC), add a real `ctrl+d` binding (more code, plus
   product decision on in-form delete), or leave it alone for a follow-up?
   Recommendation: drop the text in this change (it's a one-line fix in
   `proxy/tui/views.go:370`).
2. **Picker `q`/`Ctrl+C` leak** — while the picker is open, the bubble
   list's default Quit keymap will quit the TUI. Unrelated to this
   change but visible in the same code path. Note for a follow-up.
3. **Should the modal be scrollable?** — with ~17 shortcut rows the modal
   fits in a 40%-of-height 24-line terminal (17 + 5 chrome = 22 lines).
   On a 20-line terminal the body would clip. lipgloss has no built-in
   scroll, so this would need a `viewport` component. For the MVP, hard-clip
   is acceptable; flag for a follow-up if the user requests it.
4. **Should the modal render the group headers** ("Global", "Tab switching",
   "Log tab", ...)? The data structure supports it (just intersperse
   `shortcut{key: "— Global —", desc: ""}` rows or add a `group` field).
   The user said "grouped by context" — interpret as "logically grouped"
   in code, not necessarily "visually separated by headers in the modal".
   Recommend starting without headers and adding if the user asks.
5. **`?` opener while picker is open** — section 6 documents that pressing
   `?` in the picker is currently a no-op (list's help toggle, hidden).
   The new `?` opener lives in `handleTabModeKeyPress`, which is not
   reached when the picker is open (form mode intercepts first). So the
   modal is unreachable from the picker today. That is correct behavior
   for now. If the user later wants help to be openable from the picker,
   move the `?` opener up one level in the dispatch (but then it would
   also fire inside form text input, which is wrong).

## Implementation Estimate

- ~160 LOC total, 0 LOC removed
- 5 files modified, 1 file added (`proxy/tui/help.go`)
- 0 new dependencies
- 6-8 new test cases; 0 existing tests need updates
- No impact on `main.go`, `proxy/`, `config/`, or any non-TUI package
