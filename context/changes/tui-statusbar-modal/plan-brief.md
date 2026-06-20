# TUI Statusbar on Top + `?` Keyboard Shortcuts Modal — Plan Brief

> Full plan: `context/changes/tui-statusbar-modal/plan.md`
> Research: `context/changes/tui-statusbar-modal/research.md`

## What & Why

Two coordinated dashboard chrome improvements for the freedius Bubble Tea
TUI: (1) move the existing bottom stats bar to the top of the screen so
uptime, request count, error count, error rate, and the transient status
message are always visible, and (2) add a `?`-triggered help modal that
lists every current keybinding in a flat two-column layout.

The two changes are functionally independent but land in the same review
because they jointly improve the dashboard chrome.

## Starting Point

The dashboard is composed in `Dashboard.View` at `proxy/tui/model.go:457-497`
as `tabs → body → stats`. The stats bar (`renderStatsBar`,
`proxy/tui/views.go:219-241`) is the last line; the tab bar
(`renderTabs`, `proxy/tui/views.go:16-32`) is two lines (label + bottom
border); the body fills the rest. The reserved chrome is 3 rows, so
`bodyHeight := height - 3` at `proxy/tui/model.go:471` is the body size.
There is no help system; the only overlay today is the
`providerPicker` (`proxy/tui/picker.go:23-99`), which is body-scoped
(replaces one field inside the form, not the whole screen). The form
footer at `proxy/tui/views.go:370` advertises a `Ctrl+D=Delete` shortcut
that is not bound anywhere in the package.

## Desired End State

**Layout:** stats bar (1 row) → tab bar (label + bottom border = 2 rows) →
body. Stats bar always visible regardless of mode. `bodyHeight := height - 3`
stays correct (the chrome row count is symmetric: 1 + 2 either way).
Form footer no longer mentions `Ctrl+D=Delete`.

**Modal:** pressing `?` while no form is open renders a centered dialog
titled "Keyboard Shortcuts" listing every current binding as
`key — description` rows in a flat list. Pressing `?` or `Esc` while open
closes it. While open, no other key propagates to the dashboard — `1`/`2`/`3`
do not switch tabs, `q`/`Ctrl+C` does not quit, `e`/`a`/`p`/`d` does not
open forms.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Layout | Stats on top, then tabs, then content | User-decided; matches the "always visible" intent for stats. | Plan |
| Modal trigger | `?` | Standard TUI convention; verified unbound across all 17 case labels. | Research |
| Modal content | All current shortcuts in a flat list (no visual group headers) | User-decided; matches lazygit / k9s / btop convention. | Plan |
| Shortcut data location | New `proxy/tui/help.go` | Per-concern file pattern; mirrors `picker.go`. | Plan |
| Form footer `Ctrl+D=Delete` bug | Drop the misleading substring (one-line fix) | User-decided; in-scope because it touches the same chrome review. | Plan |
| Phase structure | 2 phases: layout+footer, then modal | User-decided; each phase ships a reviewable diff with a manual gate. | Plan |
| Implementation style | Hand-roll with tests-after | User-decided; code is read top-to-bottom in the same order. | Plan |
| Layout regression test | Include `TestDashboard_Layout_StatsAboveTabs` | User-decided; locks in the new chrome order. | Plan |
| Modal overlay technique | `lipgloss.Place` + `WithWhitespaceStyle` | Idiomatic Charm approach; v2 API requires the new style-merging option. | Research |
| Modal open scope | Tab mode only (`handleTabModeKeyPress`) | `?` must remain typeable in form fields and be a no-op in the picker. | Research |
| Modal capture position | First branch in `KeyPressMsg` dispatch in `Update` | Early `return d, nil` swallows all keys, preventing accidental input. | Research |
| Bodyheight math comment | Add a 4-line comment at `model.go:471` | The `-3` is symmetric but undocumented; future maintainers will benefit. | Research |
| No new dependencies | lipgloss v2.0.4 already provides everything needed | Avoids a `go.mod` churn and a `go.sum` audit. | Research |
| Test depth | 1 layout regression + 6 modal tests | Covers open/close/toggle/capture/view/form-scoping. | Research |

## Scope

**In scope:**
- Reorder the Sprintf in `Dashboard.View` (stats first, tabs second, body third).
- Drop the misleading `Ctrl+D=Delete` substring from the form footer.
- Add a regression test that locks in the new chrome order.
- Add a comment explaining the `bodyHeight` math.
- Create `proxy/tui/help.go` with the shortcut data.
- Add 5 modal styles to `proxy/tui/styles.go`.
- Add 3 new functions to `proxy/tui/views.go`: `modalWidthFor`, `renderHelpModal`, `overlayModal`.
- Add a `showHelp` field to the `Dashboard` struct.
- Add a help-capture block at the top of the `KeyPressMsg` dispatch in `Update`.
- Add a `?` case in `handleTabModeKeyPress`.
- Add overlay composition at the end of `View`.
- Add 6 new tests for the modal.

**Out of scope:**
- Fix the providerPicker's `q`/`Ctrl+C` leak (pre-existing, separate concern).
- Add a real `Ctrl+D` binding for in-form delete (separate product decision).
- Make the modal scrollable (clip on small terminals is acceptable for MVP).
- Add visual group headers in the modal (data is logically grouped; visually flat per user decision).
- Change any non-TUI code (`main.go`, `proxy/`, `config/`, etc.).
- Change `bodyHeight := height - 3` math (already correct for both layouts).
- Add any new external dependencies.

## Architecture / Approach

The layout change is a one-line Sprintf reorder at `proxy/tui/model.go:493`.
The modal change follows the existing `d.showPicker` boolean pattern at
`proxy/tui/model.go:101`: a new `d.showHelp` field, a new
`case "?"` in `handleTabModeKeyPress` to open it, a new capture block at
the top of the `KeyPressMsg` switch in `Update` to close it (and swallow
all other input), and a new overlay composition at the end of `View` that
uses `lipgloss.Place` to center the modal on the screen. The shortcut
data lives in a new `proxy/tui/help.go` (per-concern file pattern from
`picker.go`); the renderer lives in `views.go` next to the other
renderers; the styles live in `styles.go` next to the existing palette.

```
┌────────────────────────────────────┐
│  stats bar (uptime, requests, ...) │  ← always visible, 1 row
├────────────────────────────────────┤
│  [1] Log [2] Providers [3] Config  │  ← tab bar, 2 rows
├────────────────────────────────────┤
│                                    │
│  body (active tab content or form) │
│                                    │
│                                    │
│                                    │
└────────────────────────────────────┘

When `?` is pressed:

┌────────────────────────────────────┐
│  stats bar                         │
├────────────────────────────────────┤
│  tabs                              │
├────────────────────────────────────┤
│  body... (overwritten by modal)    │
│      ┌─────────────────────────┐   │
│      │   Keyboard Shortcuts    │   │
│      │   ─────────────────     │   │
│      │   q / Ctrl+C   Quit     │   │
│      │   ?            Show...  │   │
│      │   ...                   │   │
│      │   Press ? or Esc ...    │   │
│      └─────────────────────────┘   │
└────────────────────────────────────┘
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Layout reorder + footer fix + regression test | Stats on top, tabs below, content below. `Ctrl+D=Delete` removed from form footer. One regression test locks in the order. | Layout regression unnoticed without the test. |
| 2. `?` keyboard shortcuts modal | `?` opens centered help modal listing every binding; `?` or `Esc` closes it; all other keys are swallowed while open. Six new tests. | `?` accidentally fired in form fields if opener isn't scoped to tab mode. |

**Prerequisites:** Go 1.21+ (project uses Go 1.26.4 per `go.mod`); `lipgloss/v2 v2.0.4` already pinned; no new dependencies to add.

**Estimated effort:** ~1-2 sessions for both phases; Phase 1 is ~15 minutes, Phase 2 is the bulk of the work.

## Open Risks & Assumptions

- **Bodyheight budget is undocumented.** Today `bodyHeight := height - 3` works for both layouts because the chrome is symmetric. A future change that adds a row to either bar will silently overflow. The plan adds a comment but does not refactor the math.
- **Modal is not scrollable.** With 18 rows the modal fits on a 24-line terminal. Smaller terminals will clip. Acceptable for MVP per user decision; flag for a follow-up.
- **The `Ctrl+D=Delete` fix touches a different line than the layout reorder.** Bundled in Phase 1 because both are chrome-string edits. If the reviewer wants a separate review, split into 3 phases.
- **Form-mode `?` typed into a text field is asserted by the `TestDashboard_HelpModal_NotOpenedInForm` test.** The bubble text input may or may not accept `?` as a literal character depending on the bubble version; if it doesn't, the test should be adjusted to verify the field value is unchanged after `?` is pressed.
- **Picker `q`/`Ctrl+C` leak is unfixed.** Visible to the user but unrelated to this change; note for a follow-up.

## Success Criteria (Summary)

- Stats bar appears at the top of the TUI on every screen, always visible regardless of mode.
- Pressing `?` opens a centered help modal listing every current binding; pressing `?` or `Esc` closes it.
- The form footer no longer mentions `Ctrl+D=Delete`.
- All existing tests pass; 1 new layout test + 6 new modal tests pass.
- `go vet ./...` clean; `go build -o freedius .` produces a working binary.
