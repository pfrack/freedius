# TUI Themes — Plan Brief

> Full plan: `context/changes/tui-themes/plan.md`
> Research: `context/changes/tui-themes/research.md`

## What & Why

Add user-selectable, terminal-background-adaptive themes to the freedius Bubble Tea TUI. Currently all 16 style vars are hard-coded ANSI colors (`lipgloss.Color("1")` … `"8"`) in `proxy/tui/styles.go`. This plan introduces a `Theme`/`Palette`/`Styles` type hierarchy, 5 built-in themes (default + 3–4 dark alternatives), `Ctrl+T` cycling, and optional `theme:` YAML persistence.

## Starting Point

The TUI has 16 package-level `lipgloss.Style` vars using raw ANSI 8-color codes. There is no palette abstraction, no theme config, and no env var. Research confirmed the blast radius is small: `proxy/tui/styles.go` is the single source of truth, one inline outlier at `views.go:386`, tests are safe (all pass through `stripANSI`), and the `cycleLogLevel` handler at `model.go:387-401` is the exact precedent pattern.

## Desired End State

A user starts freedius, opens the TUI, and sees the familiar look. Pressing `Ctrl+T` cycles through themes (default → zenburn → gruvbox-dark → catppuccin-mocha → …), with the active name shown in the stats bar. Adding `theme: zenburn` to `freedius.yaml` persists the choice across restarts. Unknown theme names silently fall back to default. The help modal lists the `Ctrl+T` shortcut. All existing tests pass unchanged.

## Key Decisions Made

| Decision                       | Choice                     | Why                                                                  | Source   |
| ------------------------------ | -------------------------- | -------------------------------------------------------------------- | -------- |
| Theme storage                  | Config-backed + cycle key  | Cycle key gives discoverability; config gives persistence.           | Research |
| Light/dark adaptation          | Adaptive (light+dark pair) | Same theme name works on any terminal background.                    | Research |
| Built-in theme set             | Small: default + 3–4 darks | Covers most preferences without maintenance burden.                  | Research |
| Config validation              | Accept any string, TUI init | Avoids import cycle; TUI silently falls back on mismatch.            | Plan     |
| Theme cycle keybind            | `Ctrl+T`                   | Memonic; avoids shadowing tab-specific shortcuts.                    | Plan     |
| Picker UX                      | Simple cycle (no modal)    | Matches `L` log-level pattern exactly; fine for ≤5 themes.          | Plan     |
| Terminal background detection  | Once at startup            | `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` — no re-detect.   | Plan     |

## Scope

**In scope:**
- `AdaptiveColor`, `Palette`, `Theme`, `Styles` types in `proxy/tui/styles.go`
- Styles moved from package vars to `Dashboard.styles` struct
- `Ctrl+T` → `cycleTheme()` handler following `cycleLogLevel` template
- 5 built-in themes: default, zenburn, gruvbox-dark, catppuccin-mocha, + one more
- Active theme name in stats bar
- `theme:` YAML field on `Config` (no validation in `config` package)
- `overlayModal` outlier fixed to use theme background
- Help modal updated with `Ctrl+T` entry

**Out of scope:**
- Color-profile detection (truecolor fallback)
- Modal theme picker (cycle-only for now)
- `FREEDIUS_THEME` env var
- Dynamic theme file loading
- Per-tab style customization
- Re-detecting terminal background on SIGWINCH

## Architecture / Approach

A `Palette` holds 7 `AdaptiveColor{Light, Dark}` slots. `NewStyles(p Palette, isDark bool)` builds the complete 17-field `Styles` struct using `lipgloss.LightDark(isDark)` to resolve each slot. `NewDashboard` detects background once at init, resolves the theme name against a `themeRegistry`, and builds initial styles. `cycleTheme()` advances through the registry, rebuilds styles, and updates `stats.message` — Bubble Tea re-renders automatically on the next `View()`. Config stores only the theme name string; TUI validates and falls back.

## Phases at a Glance

| Phase     | What it delivers                                                  | Key risk                                    |
| --------- | ----------------------------------------------------------------- | ------------------------------------------- |
| 1. Foundation | Types + Styles struct + refactor off package vars                | Missed a package-var reference (caught by `go vet` + build) |
| 2. Themes + Cycle | 4 extra themes, Ctrl+T handler, stats bar, help update        | Theme palette values look wrong on terminal (mitigated by manual QA) |
| 3. Config | `theme:` YAML field, main.go wiring, TUI-init validation       | Config round-trip breaks `Save()` (mitigated by marshaling test) |

**Prerequisites:** None — all changes are in `proxy/tui/`, `config/`, and `main.go`.
**Estimated effort:** ~3 implementation sessions (one per phase).

## Open Risks & Assumptions

- Adaptive light colors are approximated (themes are dark-oriented); a light-terminal user may see washed-out colors on the wrong theme until they cycle to the next one. Acceptable for MVP.
- `lipgloss.LightDark` requires Go 1.22+ and `charm.land/lipgloss/v2` v2.0.4 — confirmed present at `go.mod:8`.

## Success Criteria (Summary)

- All existing tests pass unchanged throughout
- `Ctrl+T` cycles through all built-in themes with visible palette changes
- `theme: zenburn` in `freedius.yaml` persists across restarts; unknown names fall back silently
- The overlay modal background matches the active theme
- The help modal lists `Ctrl+T`
