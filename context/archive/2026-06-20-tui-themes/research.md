---
date: 2026-06-20T23:56:42+0200
researcher: opencode
git_commit: 9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7
branch: main
repository: pfrack/freedius
topic: "Adding user-selectable themes (e.g. zenburn) to the freedius Bubble Tea TUI"
tags: [research, tui, theming, lipgloss, bubble-tea, surface-scan]
status: complete
last_updated: 2026-06-20
last_updated_by: opencode
---

# Research: Adding user-selectable themes to the freedius TUI

**Date**: 2026-06-20T23:56:42+0200
**Researcher**: opencode
**Git Commit**: `9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7`
**Branch**: `main`
**Repository**: `pfrack/freedius`

## Research Question

The user asked: *"zenburn or other themes for tui"*. Scoped to: **add theming to the freedius TUI**, with a **surface scan of current TUI code** (not a deep architectural dive).

Concretely: what files, style variables, and external interfaces does theming touch today, and what's the smallest set of facts needed to start designing a theme system on top of the existing Bubble Tea / lipgloss stack?

## Summary

The freedius TUI is a Bubble Tea v2 dashboard (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2 v2.0.4`) with **one file — `proxy/tui/styles.go` — owning 16 hard-coded lipgloss styles** built from raw ANSI 8-color codes (`"1"`, `"3"`, `"4"`, `"6"`, `"7"`, `"8"`). There is **no theme system, no config knob, no env var, and no palette abstraction** today. Theming is mechanically a small change — all style references stay inside `proxy/tui/`, tests assert on plain text after stripping ANSI, and `logtee.go` / `eventbus.go` produce no colored bytes that would clash with restyling. The actual design questions (light/dark detection, palette structure, theme switching trigger, how to honor `TestRenderLogTab_NoStyling`) are concentrated in **one file plus one inline outlier in `views.go:386`** plus the `NewDashboard` constructor at `[`main.go:217`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/main.go#L217)`.

Lipgloss v2.0.4 ships exactly the primitives needed for a clean implementation: `lipgloss.LightDark(isDark)` for two-way light/dark, `lipgloss.Complete(profile)` (with `colorprofile.Detect`) for three-way ANSI/256/truecolor, and `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` for background detection. There is **no built-in theme registry** — every Bubble Tea app defines its own `Theme` struct, which is the pattern freedius would follow.

## Detailed Findings

### 1. The TUI package at a glance

`proxy/tui/` — 8 files, ~2,729 lines total (Explore agent count):

| file | lines | role |
|---|---:|---|
| `model.go` | 814 | Dashboard model, Update/View, form logic, key handlers |
| `model_test.go` | 1152 | Tests (strip-ANSI pattern, no styled-output assertions) |
| `views.go` | 389 | Per-tab renderers: `renderTabs`, `renderLogTab`, `renderProvidersTab`, `renderConfigTab`, `renderStatsBar`, `renderForm`, `renderHelpModal`, `overlayModal` |
| `picker.go` | 114 | `providerPicker` wrapping bubbles `list.Model` |
| `picker_test.go` | 120 | Tests picker state only — never calls `View()` |
| `styles.go` | 82 | **All 16 named lipgloss styles** (single source of truth) |
| `help.go` | 28 | Keyboard shortcut list for the help modal |
| `loglevel.go` | 30 | Log filter types and the cycling slice |

### 2. Style inventory — `[`proxy/tui/styles.go:7-67`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/styles.go#L7-L67)`

The 16 named package vars and their current hard-coded colors:

| var | line | color slots used | purpose |
|---|---:|---|---|
| `activeTabStyle` | 8 | none — Bold + Underline | active tab label |
| `inactiveTabStyle` | 13 | none — Faint | inactive tab label |
| `statusClientErrStyle` | 17 | `Color("3")` (ANSI yellow) | client-error status (4xx) |
| `statusErrorStyle` | 20 | `Color("1")` (ANSI red) | server-error status (5xx) |
| `statsBarStyle` | 23 | none — Reverse | stats footer bar |
| `tabBarStyle` | 27 | `BorderForeground(Color("8"))` (gray) | tab bar border |
| `windowStyle` | 31 | none — Padding | body wrapper |
| `providerTableHeaderStyle` | 34 | none — Bold + Underline | providers tab table header |
| `configKeyStyle` | 38 | none — Bold | config key label |
| `configValueStyle` | 41 | none — Faint | config value label |
| `separatorStyle` | 44 | `Color("8")` | horizontal separators |
| `modalStyle` | 47 | `BorderForeground(Color("4"))` (blue) | help modal border |
| `modalTitleStyle` | 52 | `Color("4")` | help modal title |
| `modalFooterStyle` | 57 | none — Faint + Italic | help modal footer |
| `shortcutKeyStyle` | 61 | `Color("6")` (cyan) | help modal key column |
| `shortcutDescStyle` | 65 | `Color("7")` (light gray) | help modal description column |

So the **theme-able slots** that need named colors are:

- **red** (`1`) — errors
- **yellow** (`3`) — client errors / warnings
- **blue** (`4`) — modal accent (border + title)
- **cyan** (`6`) — modal key labels
- **light gray** (`7`) — modal descriptions
- **gray** (`8`) — borders and separators

A "zenburn" or similar dark theme is just a re-mapping of these six color slots (plus the implicit bold/underline/faint/italic/reverse modifiers). The 16 styles collapse to a palette struct of ~6 named colors plus the modifiers.

### 3. The single inline outlier

`[`proxy/tui/views.go:380-388`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/views.go#L380-L388)`:

```go
func overlayModal(_, modal string, width, height int) string {
    return lipgloss.Place(
        width, height,
        lipgloss.Center, lipgloss.Center,
        modal,
        lipgloss.WithWhitespaceStyle(
            lipgloss.NewStyle().Background(lipgloss.Color("0")),
        ),
    )
}
```

`lipgloss.Color("0")` (black) is hard-coded as the whitespace background behind modal overlays. This is the **only `lipgloss.NewStyle()` call outside `styles.go`** in the package (17 total `NewStyle` calls; 16 in styles.go, this 1 here). It needs to follow the theme too — the most natural move is to add a `backgroundColor` slot to the palette struct.

### 4. Tests are theme-safe

- `proxy/tui/picker_test.go` — never calls `View()`; tests picker state via `SelectedProvider()`, `list.Items()`, `list.Index()`, and `tea.KeyPressMsg`. Zero style references.
- `proxy/tui/model_test.go` — defines a `stripANSI(s string) string` helper at lines 695-713 that removes `\x1b...m` escapes, and wraps **every** `View()` call site in `stripANSI(...)` before assertion. Call sites: 640, 665, 686, 816, 895, 977, 1010, 1030, 1042.
- The **one** styling-touching assertion is `TestRenderLogTab_NoStyling` at `model_test.go:660-669`:

  ```go
  // Asserts stripANSI(out) == out — the log tab must emit no ANSI escapes.
  ```

  This invariant must be **preserved by any new theme**: the log tab renders lines from `proxy.LogEntry.Line`, which is plain `slog.NewTextHandler` text. A theme must not inject ANSI into the log content itself. Today, `renderLogTab` (`views.go:33-63`) indeed writes raw `e.Line + "\n"` without any style — confirming the test invariant holds.

**Implication for design:** tests won't break when styles change, as long as the log tab continues to pass through plain text. Any future "syntax highlighting in log lines" or "level-colored log lines" feature would conflict with this test and would need its own update.

### 5. No external color leakage

Two pieces of evidence that theming is contained:

- **Outside `proxy/tui/`, no file imports lipgloss.** A repo-wide search for `lipgloss` and `charm.land/lipgloss` matches only `proxy/tui/styles.go` and `proxy/tui/views.go`. `main.go` writes to `os.Stderr` via `fmt.Fprintln` and `slog.NewTextHandler` — no color.
- **`[`proxy/logtee.go:84`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/logtee.go#L84)`** uses `slog.NewTextHandler(buf, ...)` to format entries into the ring-buffer; the `Line` string pushed into `LogSink` is plain text only. **`proxy/eventbus.go`** is a pure data struct — no bytes emitted. Neither produces colored bytes that would clash with a TUI re-skin.

### 6. The injection point — `NewDashboard`

`[`main.go:217-222`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/main.go#L217-L222)`:

```go
model := tui.NewDashboard(
    bus.Subscribe(),
    logSink.Subscribe(),
    cfg, registry, dispatcher, cfgPath, host, port, verboseErrors,
)
prog := tea.NewProgram(model)
```

`NewDashboard` already takes 9 positional args. A `theme` argument would slot in after `verboseErrors`, or — given the tail `(cfg, registry, dispatcher, cfgPath, host, port, verboseErrors)` is config-y and growing — convert that tail into an options struct (`tui.Options`). The `Dashboard` struct at `model.go:77-110` is the matching storage site; a `theme *Palette` field would join the existing `styleBody bool` (line 82) and `currentLogLevel LogFilter` (line 94).

The `model.go:120-156` constructor would gain a theme-resolution step: if `nil`, use `DefaultPalette()`; otherwise accept the injected palette.

### 7. Cycling pattern — `cycleLogLevel` is the precedent

The existing `L`-key handler at `model.go:322-324` plus `cycleLogLevel` at `model.go:387-401` (with `logFilterCycle` slice at `loglevel.go:25`) is the **exact template** for a "cycle theme" handler:

```go
// model.go:389-401
func (d *Dashboard) cycleLogLevel() {
    for i, f := range logFilterCycle {
        if f.Label == d.currentLogLevel.Label {
            next := (i + 1) % len(logFilterCycle)
            d.currentLogLevel = logFilterCycle[next]
            d.logScroll = 0
            return
        }
    }
    d.currentLogLevel = filterAll  // fallback
    d.logScroll = 0
}
```

Pattern: `themeCycle []Palette` (package var), `currentTheme Palette` (field on Dashboard), `cycleTheme()` (helper that advances, re-runs the style-builder, and assigns the new styles to the package vars). A second handler — `toggleVerboseErrors` at `model.go:375-385` bound to `ctrl+e` — is the simpler precedent for a 2-option knob; for a multi-theme toggle the cycle shape is more apt.

A theme switch triggers a full Bubble Tea re-render automatically on the next `View()` call — no need for a `tea.Cmd` or `tea.Msg`.

### 8. Lipgloss v2 theming primitives (Context7)

Confirmed against `charmbracelet/lipgloss` v2 docs:

```go
// Background detection — v2 requires explicit I/O
hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)

// Two-way light/dark picker — returns a func(light, dark color.Color) color.Color
lightDark := lipgloss.LightDark(hasDark)
fg := lightDark(lipgloss.Color("#333333"), lipgloss.Color("#f1f1f1"))

// Three-way color-profile picker — needs colorprofile.Detect
profile := colorprofile.Detect(os.Stdout, os.Environ())
complete := lipgloss.Complete(profile)
color := complete(
    lipgloss.Color("5"),       // fallback (ANSI)
    lipgloss.Color("200"),     // mid (ANSI256)
    lipgloss.Color("#ff00ff"), // best (truecolor)
)

// Drop-in compat helper for old AdaptiveColor usage
import "charm.land/lipgloss/v2/compat"
color := compat.AdaptiveColor{
    Light: lipgloss.Color("#0000ff"),
    Dark:  lipgloss.Color("#000099"),
}
```

**Key observation:** there is **no built-in theme registry** in lipgloss v2. Every Bubble Tea application with theming (Soft Serve, Glow, lazygit, etc.) defines its own `Theme` struct. The idiomatic shape is:

```go
type Theme struct {
    Name       string
    Error      color.Color
    Warning    color.Color
    Accent     color.Color
    KeyCap     color.Color
    Muted      color.Color
    Border     color.Color
    Background color.Color
}

var themes = []Theme{
    {Name: "default", Error: lipgloss.Color("1"), Warning: lipgloss.Color("3"), ...},
    {Name: "zenburn", Error: lipgloss.Color("#cc9393"), Warning: lipgloss.Color("#e0c989"), ...},
    {Name: "catppuccin-mocha", ...},
}

// Build lipgloss.Style values from a Theme
func (t Theme) Styles() Styles { ... }
```

`color.Color` is the `go-colorful` interface (`github.com/lucasb-eyer/go-colorful`), which is already an indirect dep at `go.mod:26`.

## Code References

- `[`proxy/tui/styles.go:7-67`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/styles.go#L7-L67)` — 16 named lipgloss style vars; **single source of truth** for the TUI palette.
- `[`proxy/tui/styles.go:17-21`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/styles.go#L17-L21)` — `statusClientErrStyle` (`Color("3")` yellow), `statusErrorStyle` (`Color("1")` red).
- `[`proxy/tui/styles.go:44-55`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/styles.go#L44-L55)` — `separatorStyle` (`Color("8")`), `modalStyle` (`BorderForeground("4")`), `modalTitleStyle` (`Color("4")`).
- `[`proxy/tui/styles.go:61-66`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/styles.go#L61-L66)` — `shortcutKeyStyle` (`Color("6")` cyan), `shortcutDescStyle` (`Color("7")`).
- `[`proxy/tui/views.go:380-388`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/views.go#L380-L388)` — `overlayModal` inline `lipgloss.NewStyle().Background(lipgloss.Color("0"))` — the **one inline outlier** to migrate.
- `[`proxy/tui/views.go:33-63`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/views.go#L33-L63)` — `renderLogTab` writes `e.Line + "\n"` raw, no styling; this is what keeps `TestRenderLogTab_NoStyling` passing.
- `[`proxy/tui/model.go:77-110`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model.go#L77-L110)` — `Dashboard` struct (storage site for `currentTheme` field).
- `[`proxy/tui/model.go:120-156`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model.go#L120-L156)` — `NewDashboard` constructor (entry point for theme argument).
- `[`proxy/tui/model.go:217-221`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model.go#L217-L221)` — `main.go` `NewDashboard` call site.
- `[`proxy/tui/model.go:264-333`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model.go#L264-L333)` — `handleTabModeKeyPress` (would gain a `case "T":` arm).
- `[`proxy/tui/model.go:387-401`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model.go#L387-L401)` — `cycleLogLevel` (template for `cycleTheme`).
- `[`proxy/tui/loglevel.go:25`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/loglevel.go#L25)` — `logFilterCycle` (template for `themeCycle` slice).
- `[`proxy/tui/model_test.go:695-713`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model_test.go#L695-L713)` — `stripANSI` helper that theme changes do not break.
- `[`proxy/tui/model_test.go:660-669`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/tui/model_test.go#L660-L669)` — `TestRenderLogTab_NoStyling` — invariant that the log tab must remain ANSI-free regardless of theme.
- `[`proxy/logtee.go:84`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/logtee.go#L84)` — `slog.NewTextHandler(buf, ...)` produces the plain-text `Line` that the log tab renders.
- `[`proxy/eventbus.go:13-26`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/proxy/eventbus.go#L13-L26)` — `RequestEvent` data struct (no color).
- `[`config/config.go:28-32`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/config/config.go#L28-L32)` — top-level `Config` struct (potential site for a future `theme:` YAML key if config-driven theming is desired).
- `go.mod:8` — `charm.land/lipgloss/v2 v2.0.4`.
- `go.mod:26` — `github.com/lucasb-eyer/go-colorful v1.4.0` (indirect — already in module graph; needed by `color.Color` interface).

## Architecture Insights

1. **The blast radius is small.** Adding a theme system touches one file (`styles.go`), one inline outlier (`views.go:386`), and the constructor (`[`main.go:217`](https://github.com/pfrack/freedius/blob/9667d0a9fec82406dbfaf10fb9e6d7b9a63ea2e7/main.go#L217)`, `model.go:120`). No proxy code, no event bus, no config schema, no test logic. The total LOC delta for a minimal theming PR is plausibly <300 lines.

2. **The "single source of truth" pattern is already established.** The package already follows the `package var = lipgloss.NewStyle()...` pattern. Theming is a one-level refactor: replace those 16 package vars with a `Styles` struct held on `Dashboard`, populated by `theme.BuildStyles()`. Renderers keep reading the same names — but now via `d.styles.statusErrorStyle` (or a method receiver like `d.statusErrorStyle()`).

3. **Lipgloss v2 gives us everything we need.** `lipgloss.LightDark(hasDark)` is the two-way picker; `lipgloss.Complete(profile)` with `colorprofile.Detect(os.Stdout, os.Environ())` is the three-way. No new dependencies required. `compat.AdaptiveColor{Light, Dark}` is a drop-in for an `AdaptiveColor`-shaped palette if we want each theme to declare both light and dark variants in one struct.

4. **Theme switching is free at runtime.** Bubble Tea's `View()` is called on every state change; swapping the `Styles` field on the model triggers a redraw on the next frame. No `tea.Cmd` or message plumbing needed.

5. **`TestRenderLogTab_NoStyling` is a load-bearing constraint.** It bakes in the rule that the log tab passes `slog` text through verbatim. A future "color log lines by level" feature would need to consciously break this test. The current scoping keeps log-tab styling out of the theming question.

6. **Config-driven vs runtime-only is the open architectural fork.** A theme can be selected by (a) a hard-coded cycle (`T` key, like `L` for log level), (b) an env var (`FREEDIUS_THEME=zenburn`), (c) a YAML config key (`theme: zenburn` in `freedius.yaml`), or (d) some combination. The Explore agent found **no `FREEDIUS_TUI` / `FREEDIUS_THEME` / `FREEDIUS_COLOR` env var** already in use, so there is no existing convention to follow. The README at `README.md:84` documents the `Ctrl+E` / `Ctrl+S` / `L` shortcuts — a `T` (cycle theme) key would fit the established pattern.

7. **No `AdaptiveColor` use today.** A future-facing design might want every theme slot to be an `AdaptiveColor{Light, Dark}` (or `compat.AdaptiveColor` in v2) so the same theme name works on both light and dark terminals. The current `Color("1")` etc. codes already auto-adapt based on terminal color profile, but they don't switch on **background luminance** — only on color capability.

## Historical Context (from prior changes)

The TUI has been built across several archived changes — none of them tackled theming, but the relevant precedents for a theming PR are:

- `context/archive/tui-dashboard/` — original Bubble Tea dashboard scaffold. Confirmed: the `model.go` 9-arg constructor and 16-style `styles.go` layout date to this change.
- `context/archive/tui-config-setup/` — added form fields and error display; did **not** touch `styles.go`.
- `context/archive/tui-all-logs-level-filter/` — added the `L`-key log-level cycle. **`model.go:322-324` + `model.go:387-401` is the literal template a `cycleTheme` would follow.**
- `context/archive/tui-statusbar-modal/` — added the `?` help modal that consumes `shortcutKeyStyle` / `shortcutDescStyle` / `modalStyle` / `modalTitleStyle` — the help modal is the single most theme-sensitive view, since its text is dense and color-coded.
- `context/archive/providers-section-refactor/` — split provider/mapping forms. Did not touch `styles.go`.

**No prior change has added theming.** This is a greenfield feature inside an existing TUI.

## Related Research

- `context/archive/tui-dashboard/research.md` — TUI architecture research; relevant for understanding form patterns and the Bubble Tea model layout.
- `context/archive/tui-config-setup/research.md` — TUI config editing research; describes the current form-field structure.
- `context/archive/tui-all-logs-level-filter/research.md` — Log-level filter research; the closest precedent to a cycle-theme feature.
- `context/archive/tui-statusbar-modal/research.md` — Help modal research; relevant since the help modal consumes the most theme-sensitive styles.

## Open Questions

1. **Theme storage:** runtime-only cycle, env var, YAML config, or all three? The current TUI mixes runtime-only knobs (`L` for log level, `Ctrl+E` for verbose errors) with config-driven settings (config file path, host, port, providers/mappings). Theme arguably belongs with the config-driven set if a user wants persistence, but cycle-key gives discoverability. A clean answer: cycle-key for discovery + optional `theme:` YAML key for persistence (cycle starts from the configured theme if set).

2. **Light/dark auto-switching:** Should each theme name map to a single fixed palette, or should each theme name carry light/dark variants and auto-switch via `lipgloss.HasDarkBackground`? The latter is more powerful but doubles the palette per theme. A reasonable MVP: single palette per theme name; `default` auto-detects background.

3. **Built-in themes:** Which presets to ship? Options: zenburn, gruvbox (dark + light), catppuccin (mocha/latte/frappe/macchiato), dracula, nord, tokyo night, solarized. A minimal MVP is `default` + `zenburn`; a richer set is ~5-6 dark + 1-2 light.

4. **Config schema impact:** Adding `theme:` to `Config` requires a struct field + YAML tag + validation in `validate()` (whitelist against the theme registry). If the theme registry lives in `proxy/tui/`, the `config` package would either need to import it (potential cycle) or accept any string and have `proxy/tui` validate at startup. Worth checking whether other config strings follow the same "accept and validate later" pattern.

5. **Picker UX:** Cycle key vs a theme-picker modal? A modal is friendlier once there are >3 themes; a cycle key fits the current minimalism. Lazy: cycle key for ≤5 themes, modal for more.

6. **Help modal updates:** Adding a `T` shortcut (or whatever) requires updating `proxy/tui/help.go` and re-running the help-modal tests. Trivial but worth flagging in the plan.
