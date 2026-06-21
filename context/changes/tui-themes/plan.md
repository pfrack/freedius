# TUI Themes Implementation Plan

## Overview

Add user-selectable, terminal-background-adaptive themes to the freedius Bubble Tea TUI. The current 16 hard-coded ANSI-color styles in `proxy/tui/styles.go` move to a `Styles` struct owned by `Dashboard`, built from a `Theme` palette with light/dark adaptive color pairs. A `Ctrl+T` cycle and optional `theme:` YAML config field let users switch between themes.

## Current State Analysis

- **16 lipgloss style vars** in `proxy/tui/styles.go:7-67` — all hard-coded with raw `lipgloss.Color("1")` ... `lipgloss.Color("8")` ANSI codes.
- **6 theme-able color slots**: red (errors), yellow (warnings), blue (modal accent), cyan (key caps), light gray (descriptions), gray (borders/separators). Plus one background slot for the `overlayModal` whitespace.
- **One inline outlier** at `proxy/tui/views.go:386` — `Background(lipgloss.Color("0"))` hard-coded black in `overlayModal`.
- **Styles are package vars** — not attached to `Dashboard`, so render functions reference them globally.
- **Tests are theme-safe**: all `View()` assertions go through `stripANSI()`; the `TestRenderLogTab_NoStyling` invariant must be preserved (log tab passes through plain `slog` text).
- **NewDashboard** at `proxy/tui/model.go:120-156` takes 9 positional args, no theme parameter.
- **`cycleLogLevel`** at `proxy/tui/model.go:387-401` is the exact pattern template for `cycleTheme`.
- **No existing theme env var or config key** — greenfield in that sense.
- **Lipgloss v2.0.4** ships `lipgloss.LightDark(isDark)` for adaptive color selection and `lipgloss.HasDarkBackground` for terminal background detection.

## Desired End State

- `proxy/tui/styles.go` defines `AdaptiveColor{Light, Dark}`, `Palette` (7 slots), `Theme{Name, Palette}`, and `Styles` (struct of 16 pre-built `lipgloss.Style` values). A `NewStyles(p Palette, isDark bool) Styles` builder and a `themeRegistry []Theme` are package-level.
- `Dashboard` owns a `styles Styles` field, initialized in `NewDashboard` from a resolved theme name.
- 5 built-in themes: `default` (reproduces current ANSI colors), `zenburn`, `gruvbox-dark`, `catppuccin-mocha`, and an unnamed fourth dark theme.
- `Ctrl+T` cycles through themes in the registry; the active theme name appears in the stats bar.
- `config.Config` carries a `Theme string` YAML field; unknown names fall back to `default` silently at TUI init.
- The `overlayModal` outlier reads background from the active theme.
- All existing tests pass unchanged; the `TestRenderLogTab_NoStyling` invariant holds.

## What We're NOT Doing

- **Color-profile detection** (`lipgloss.Complete` with `colorprofile.Detect`) — deferred. MVP uses 2-way `LightDark` only; truecolor is not a goal yet.
- **Modal theme picker** — cycle key only for now (≤5 themes, matching the `L`-key log-level UX). If we add more themes later, a modal can be built on the existing `providerPicker` pattern.
- **Environment variable (`FREEDIUS_THEME`)** — config-backed is sufficient; env var duplicates without additional value.
- **Re-detecting terminal background on SIGWINCH** — detected once at startup; resizing doesn't change the terminal emulator's background.
- **Dynamic theme file loading** — themes are compiled in, not loaded from disk.
- **Per-tab style customization** — all tabs share the same theme.

## Implementation Approach

1. **Phase 1 — Foundation**: Define the type hierarchy (`AdaptiveColor` → `Palette` → `Theme` → `Styles`), move styles from package vars to `Dashboard.styles`, fix the `overlayModal` outlier. The default palette reproduces current ANSI colors exactly.
2. **Phase 2 — Themes + Cycling**: Add 3–4 additional dark themes, wire `Ctrl+T` → `cycleTheme()`, display active theme in stats bar, update help modal.
3. **Phase 3 — Config Persistence**: Add `Theme` field to `config.Config`, thread it through `main.go` → `NewDashboard`, validate at TUI init (unknown names fall back to default).

## Phase 1: Theme Data Model + Styles Refactor

### Overview

Define the type hierarchy and mechanically move style ownership from package-level vars to `Dashboard.styles`. All 16 style vars remain functionally identical — just accessed differently.

### Changes Required:

#### 1. `proxy/tui/styles.go` — Type definitions

**Intent**: Define `AdaptiveColor{Light, Dark}` (with `lipgloss.Color` values), `Palette` (7 color slots), `Theme{Name, Palette}`, and a `Styles` struct mirroring the 16 existing style vars. Add `NewStyles(p Palette, isDark bool) Styles` builder that uses `lipgloss.LightDark(isDark)` to resolve each adaptive slot into a concrete `color.Color` before building each `lipgloss.Style`. Add a `themeRegistry []Theme` package var and a `resolveTheme(name string) *Theme` function (returns nil if not found). Replace the 16 `var (...)` declarations with the `Styles` struct definition and the registry. Keep all existing `const` blocks.

**Contract**:
- `AdaptiveColor` — `struct { Light, Dark lipgloss.Color }`
- `Palette` — `struct { Error, Warning, Accent, KeyCap, Muted, Border, Background AdaptiveColor }`
- `Theme` — `struct { Name string; Palette Palette }`
- `Styles` — `struct` with 16 exported fields matching the current style names (PascalCase), plus `OverlayBgStyle lipgloss.Style` for the overlay whitespace.
- `DefaultPalette() Palette` — reproduces current ANSI colors in both light/dark. Light variant uses lighter versions; dark variant maps to the current ANSI values (since the TUI currently runs against a dark terminal).
- `DefaultTheme() Theme` — `{Name: "default", Palette: DefaultPalette()}`
- `NewStyles(p Palette, isDark bool) Styles` — calls `lipgloss.LightDark(isDark)` per slot, builds each of the 17 style fields.
- `resolveTheme(name string) *Theme` — linear scan of `themeRegistry`; returns `&DefaultTheme()` on mismatch.

#### 2. `proxy/tui/model.go` — Dashboard struct + constructor

**Intent**: Add `styles Styles` and `isDark bool` fields to `Dashboard`. Extend `NewDashboard` signature with `themeName string`; detect terminal background via `lipgloss.HasDarkBackground`; resolve theme; build `Styles`. Update all `View()` and `handleTabModeKeyPress` references from package vars to `d.styles.Xxx`.

**Contract**:
- `Dashboard` gains: `styles Styles`, `isDark bool`
- `NewDashboard` signature changes: add `themeName string` as 10th positional arg (after `verboseErrors`)
- Body adds:
  ```go
  d.isDark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
  theme := resolveTheme(themeName)
  d.styles = NewStyles(theme.Palette, d.isDark)
  ```
- In `View()` line 537: `body = windowStyle.Width(...)` → `body = d.styles.WindowStyle.Width(...)`
- In `View()` lines 544+: `modal := renderHelpModal(width)` → needs `d.styles` passed in
- In `View()` line 546: `result = overlayModal(...)` → pass `d.styles.OverlayBgStyle`
- All view function calls updated to pass `d.styles` where needed

#### 3. `proxy/tui/views.go` — Render functions accept Styles

**Intent**: Each render function that uses styles receives them as a parameter instead of reading package vars. `overlayModal` accepts a `lipgloss.Style` for background whitespace.

**Contract**:
- `renderTabs`, `renderProvidersTab`, `renderConfigTab`, `renderStatsBar`, `renderHelpModal` gain a `styles Styles` parameter (as first or last — use last for consistency).
- `overlayModal` gains a `bgStyle lipgloss.Style` parameter replacing the hard-coded `NewStyle().Background(Color("0"))`.
- `renderForm` already takes `*Dashboard` — it reads from `d.styles` directly.
- Update each function body to reference the parameter instead of package vars.
- **Do NOT import lipgloss in `model.go` for style var access** — `d.styles` subsumes that.
- Call sites in `Dashboard.View()` updated to pass `d.styles`.

### Success Criteria:

#### Automated Verification:
- `go vet ./proxy/tui/` passes — no unused imports, no unresolved identifiers
- `go test ./proxy/tui/` passes — all existing tests still pass
- `go build ./...` succeeds — no broken call sites

#### Manual Verification:
- `go run .` starts and shows the same visual output as before (identical styling)
- `TestRenderLogTab_NoStyling` invariant holds (log tab emits no ANSI escapes)
- The overlay modal background matches the rest of the TUI

---

## Phase 2: Additional Themes + Ctrl+T Cycle

### Overview

Add 3–4 dark themes to the registry, implement `Ctrl+T` cycling, show the active theme in the stats bar, and update the help modal.

### Changes Required:

#### 1. `proxy/tui/styles.go` — Theme registry entries

**Intent**: Add `zenburn`, `gruvbox-dark`, `catppuccin-mocha` (and one more) to `themeRegistry`. Each palette maps its 7 color slots to dark-terminal-appropriate colors (the Light variant can approximate on a light terminal even if not perfectly visible — the themes are dark-oriented).

**Contract**: Each new theme's palette is an entry in `themeRegistry` after `DefaultTheme()`:
```go
var themeRegistry = []Theme{
    DefaultTheme(),
    {Name: "zenburn", Palette: Palette{...}},
    {Name: "gruvbox-dark", Palette: Palette{...}},
    {Name: "catppuccin-mocha", Palette: Palette{...}},
}
```
Zenburn color references (classic zenburn.vim-inspired):
- Error: `#cc9393` (light red)
- Warning: `#e0c989` (gold)
- Accent: `#8cd0d3` (teal)
- KeyCap: `#f0dfaf` (warm yellow)
- Muted: `#dcdccc` (light warm gray)
- Border: `#7f9f7f` (muted green-gray)
- Background: `#3f3f3f` (dark warm gray)

Gruvbox-dark (reference: morhetz/gruvbox):
- Error: `#fb4934` (bright red)
- Warning: `#fabd2f` (bright yellow)
- Accent: `#83a598` (blue-gray)
- KeyCap: `#b8bb26` (bright green)
- Muted: `#ebdbb2` (light beige)
- Border: `#928374` (gray-brown)
- Background: `#282828` (dark)

Catppuccin-mocha (reference: catppuccin/catppuccin):
- Error: `#f38ba8` (red)
- Warning: `#fab387` (peach)
- Accent: `#89b4fa` (blue)
- KeyCap: `#a6e3a1` (green)
- Muted: `#cdd6f4` (light lavender)
- Border: `#585b70` (gray-purple)
- Background: `#1e1e2e` (dark)

#### 2. `proxy/tui/model.go` — Ctrl+T cycle handler

**Intent**: Add `currentTheme *Theme` to `Dashboard` (stored after resolution), add `cycleTheme()` method following the `cycleLogLevel` pattern, add `Ctrl+T` case to `handleTabModeKeyPress`.

**Contract**:
- `Dashboard` gains `currentTheme *Theme` field (initialized in `NewDashboard` from `resolveTheme`)
- New method `cycleTheme()`:
  ```go
  func (d *Dashboard) cycleTheme() {
      for i, t := range themeRegistry {
          if t.Name == d.currentTheme.Name {
              next := (i + 1) % len(themeRegistry)
              d.currentTheme = &themeRegistry[next]
              d.styles = NewStyles(d.currentTheme.Palette, d.isDark)
              d.stats.message = fmt.Sprintf("Theme: %s", d.currentTheme.Name)
              return
          }
      }
      d.currentTheme = &themeRegistry[0]
      d.styles = NewStyles(d.currentTheme.Palette, d.isDark)
      d.stats.message = fmt.Sprintf("Theme: %s", d.currentTheme.Name)
  }
  ```
- In `handleTabModeKeyPress`, add case `"ctrl+t"`:
  ```go
  case "ctrl+t":
      d.cycleTheme()
      return d, nil
  ```
- Note: the `themeName` arg in `NewDashboard` should also set `d.currentTheme` after resolution.

#### 3. `proxy/tui/help.go` — Add Ctrl+T entry

**Intent**: Add the `Ctrl+T` shortcut to the help modal.

**Contract**: Insert `{"Ctrl+T", "Cycle color theme"}` into `helpShortcuts` slice (before the form-specific entries like Tab/Enter/Esc, after `{"L", "Cycle log level filter"}`).

### Success Criteria:

#### Automated Verification:
- `go vet ./proxy/tui/` passes
- `go test ./...` passes — all tests remain green
- `go build ./...` succeeds

#### Manual Verification:
- Pressing `Ctrl+T` advances to the next theme; stats bar shows "Theme: zenburn", "Theme: gruvbox-dark", etc.
- Cycling wraps around back to "default"
- Each theme has distinct, readable visual appearance on a dark terminal
- The help modal (`?`) shows the `Ctrl+T` entry
- The overlay modal background matches the theme background

---

## Phase 3: Config Persistence

### Overview

Add `theme` to the YAML config schema, thread it from `main.go` through `NewDashboard`, validate at TUI init (unknown names silently fall back to `default`).

### Changes Required:

#### 1. `config/config.go` — Theme field

**Intent**: Add `Theme` string field to `Config` with `yaml:"theme,omitempty"` tag. No validation in `validate()` — the TUI handles unknown names at init time (chosen design: accept any string, validate at TUI init).

**Contract**:
```go
type Config struct {
    mu        sync.RWMutex
    Providers map[string]Provider `yaml:"providers"`
    Mappings  map[string]Mapping  `yaml:"mappings,omitempty"`
    Theme     string              `yaml:"theme,omitempty"`
}
```

#### 2. `main.go` — Wire theme through

**Intent**: Pass `cfg.Theme` as the new argument to `NewDashboard`.

**Contract**: At line 217-221:
```go
model := tui.NewDashboard(
    bus.Subscribe(),
    logSink.Subscribe(),
    cfg, registry, dispatcher, cfgPath, host, port, verboseErrors,
    cfg.Theme,
)
```
If `cfg.Theme` is empty string, `resolveTheme("")` in `NewDashboard` returns `DefaultTheme()`.

### Success Criteria:

#### Automated Verification:
- `go test ./config/...` passes — existing Config tests not affected by new optional field
- `go test ./...` passes
- `go vet ./...` passes
- `go build ./...` succeeds

#### Manual Verification:
- Starting with `theme: zenburn` in `freedius.yaml` starts the TUI with zenburn applied
- Starting with `theme: nonexistent` falls back to default (no crash, no visible error)
- Removing the `theme` key keeps default behavior
- Cycling via `Ctrl+T` works both with and without config-backed theme set
- `Save()` preserves the `theme` field on round-trip (marshals/unmarshals correctly)

---

## Testing Strategy

### Unit Tests:

- No new test files needed for Phase 1 — existing tests cover style rendering via `stripANSI`. The `TestRenderLogTab_NoStyling` assertion at `model_test.go:660-669` must remain passing.
- Phase 2: Add a test for `cycleTheme` — verify `d.currentTheme.Name` changes and `d.styles` is rebuilt. Small table-driven test on `model_test.go`.
- Phase 3: Add a Config round-trip test in `config/config_test.go` confirming `Theme` marshals/unmarshals with `omitempty`.

### Integration Tests:

- No integration-test changes needed — theming is purely cosmetic and tested via the existing `stripANSI` pattern.

### Manual Testing Steps:

1. Start freedius, verify TUI looks identical to before (default theme)
2. Press `Ctrl+T`, verify theme advances, stats bar updates
3. Cycle through all themes, verify each is readable and distinct
4. Open help modal (`?`), verify `Ctrl+T` entry is listed
5. Close help modal (`?` again), verify overlay modal respects theme background
6. Add `theme: zenburn` to `freedius.yaml`, restart, verify it takes effect
7. Add `theme: nonexistent`, restart, verify fallback to default with no error
8. Remove `theme` key, restart, verify default behavior unaffected

## Performance Considerations

- Terminal background detection (`lipgloss.HasDarkBackground`) is called once at startup — negligible.
- `NewStyles` is called once at startup + once per theme cycle. Building 17 `lipgloss.Style` values from a palette is well under 1ms. No optimization needed.
- No heap allocations in the hot render path — `Styles` is a struct of pre-built values, read by the existing render functions.

## Migration Notes

- The `NewDashboard` signature change (adding `themeName string`) breaks any external callers. The only call site is `main.go:217-221` — update it in Phase 3. During Phase 1–2 the new param can be `""` if kept temporarily, but it's cleaner to thread it through immediately.

## References

- Research doc: `context/changes/tui-themes/research.md`
- Style inventory: `proxy/tui/styles.go:7-67`
- Inline outlier: `proxy/tui/views.go:380-388`
- Cycle pattern: `proxy/tui/model.go:387-401`
- Constructor: `proxy/tui/model.go:120-156`
- Help shortcuts: `proxy/tui/help.go:8-28`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Theme Data Model + Styles Refactor

#### Automated

- [x] 1.1 `go vet ./proxy/tui/` passes — 3fa8f70
- [x] 1.2 `go test ./proxy/tui/` passes — 3fa8f70
- [x] 1.3 `go build ./...` succeeds — 3fa8f70

#### Manual

- [x] 1.4 Visual output identical to before (default theme)
- [x] 1.5 `TestRenderLogTab_NoStyling` invariant holds
- [x] 1.6 Overlay modal background matches rest of TUI

### Phase 2: Additional Themes + Ctrl+T Cycle

#### Automated

- [x] 2.1 `go vet ./proxy/tui/` passes — e157c21
- [x] 2.2 `go test ./...` passes — e157c21
- [x] 2.3 `go build ./...` succeeds — e157c21

#### Manual

- [x] 2.4 Ctrl+T advances theme; stats bar shows name
- [x] 2.5 Cycling wraps around to default
- [x] 2.6 Each theme is readable on a dark terminal
- [x] 2.7 Help modal lists Ctrl+T shortcut
- [x] 2.8 Overlay modal respects theme background

### Phase 3: Config Persistence

#### Automated

- [x] 3.1 `go test ./config/...` passes — c59ccc9
- [x] 3.2 `go test ./...` passes — c59ccc9
- [x] 3.3 `go vet ./...` passes — c59ccc9
- [x] 3.4 `go build ./...` succeeds — c59ccc9

#### Manual

- [x] 3.5 `theme: zenburn` in YAML takes effect on startup
- [x] 3.6 `theme: nonexistent` falls back to default silently
- [x] 3.7 No `theme` key keeps default behavior
- [x] 3.8 Ctrl+T cycle works with config-backed theme
- [x] 3.9 `Save()` round-trips the `theme` field correctly
