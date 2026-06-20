---
date: 2026-06-18T10:42:00+02:00
researcher: kiro
git_commit: 40b076d
branch: cleanup
repository: pfrack/freedius
topic: "Best UI approach for freedius v2: TUI vs Web UI vs Native GUI for cross-platform tooling"
tags: [research, codebase, tui, bubble-tea, web-ui, cross-platform, ui-architecture]
status: complete
last_updated: 2026-06-18
last_updated_by: kiro
---

# Research: Best UI Approach for Freedius v2

**Date**: 2026-06-18T10:42:00+02:00
**Researcher**: kiro
**Git Commit**: 40b076d
**Branch**: cleanup
**Repository**: pfrack/freedius

## Research Question

What is the best UI approach (TUI, web page, native GUI) for freedius v2, particularly for multi-OS support?

## Summary

**Recommendation: TUI via Bubble Tea as the primary UI, with optional embedded web dashboard for richer visualization.**

Freedius's audience is terminal-native solo developers. A TUI keeps them in their workflow, ships as a single binary, and works cross-platform with zero runtime dependencies. Bubble Tea (charmbracelet/bubbletea) is the dominant Go TUI framework in 2026, with proven adoption in tools like lazygit, k9s, and GitLab CLI. A secondary embedded web UI (htmx + templ + go:embed) can be added later for charts/historical data without breaking the single-binary constraint.

## Detailed Findings

### Option 1: TUI (Bubble Tea) — Recommended Primary

#### Framework: charmbracelet/bubbletea

- 27k+ GitHub stars, actively maintained by Charm
- Elm Architecture (Model → Update → View) — predictable state management
- Ecosystem: Bubbles (components), Lip Gloss (styling), Harmonica (animations)
- Used by: lazygit, k9s, lazydocker, soft-serve, GitLab CLI (migrating to it)

#### Cross-platform support

- Works on any terminal: Linux (all terminals), macOS (Terminal.app, iTerm2, Alacritty), Windows (Windows Terminal, PowerShell, cmd.exe via ConPTY)
- No OS-specific rendering code needed — terminal abstraction handles it
- Windows support improved significantly via tcell/Windows Terminal improvements

#### What freedius TUI can show

- Live request/response stream (model name, provider, latency, token count)
- Provider health panel (up/down, avg response time, error rate)
- Active config summary (model mappings, endpoints)
- Usage stats (requests/min, total tokens, estimated cost savings)
- Error log with recent failures

#### Pros

- Zero context switch — user stays in terminal
- Single binary, no runtime deps
- Same Go codebase, same build
- Proven pattern for developer tools
- Low idle resource footprint

#### Cons

- Limited to terminal capabilities (no charts, no images)
- Layout complexity for dense dashboards
- Testing TUI interactions is harder than web

#### Architecture pattern

```
freedius (proxy process) --internal-bus--> freedius tui (connects via Unix socket or shared memory)
```

Or simpler: `freedius --tui` flag runs proxy + TUI in same process.

### Option 2: Embedded Web UI (htmx + templ + go:embed)

#### Stack

- `a-h/templ` — type-safe Go HTML templates (compiles to Go code)
- `htmx` — 14KB JS library for dynamic updates without SPA complexity
- `go:embed` — bundle all HTML/CSS/JS into the binary
- SSE for real-time updates (freedius already does SSE for proxy responses)

#### Cross-platform support

- Inherently cross-platform — any OS with a browser
- No OS-specific code needed
- Works even on headless servers via port forwarding

#### Pattern: Traefik dashboard model

- Traefik serves its dashboard from the same binary on a separate port/path
- freedius could serve `/_dashboard` or `:8081` alongside the proxy on `:8080`
- `freedius dashboard` command auto-opens browser

#### Pros

- Richest UI possible (charts, tables, CSS styling)
- No terminal limitations
- Easier to add features over time
- SSE expertise already exists in codebase (`proxy/translate/`)

#### Cons

- Requires browser — context switch from terminal
- Two tech stacks to maintain (Go + HTML/CSS/JS, even if minimal)
- Port management (proxy port + dashboard port, or shared)
- Security surface: local web server accepting connections

#### Architecture pattern

```
freedius binary:
  :8080 — proxy (Anthropic API)
  :8081 — dashboard (htmx web UI) or /_freedius/ path on same port
```

### Option 3: Native GUI (Wails / Fyne) — Not Recommended

#### Wails (Go + WebView)

- Go backend + web frontend rendered in native WebView
- ~25k GitHub stars
- Requires WebView2 on Windows (not always present)
- Poor Windows performance reported (5s launch vs 1s for Tauri)
- Binary size ~15MB+

#### Fyne (Pure Go GUI)

- Pure Go, no CGo required for basic use
- Looks non-native on all platforms (custom renderer)
- Limited widget ecosystem compared to web

#### Why it doesn't fit freedius

- Audience is terminal-native — they don't want a GUI window
- Adds OS-specific dependencies (WebView2, GTK, etc.)
- Higher maintenance burden for marginal benefit
- Breaks single-binary simplicity on Windows
- Overkill for a local proxy management tool

### Option 4: Hybrid (TUI + Web) — Best Long-term

#### Pattern

- `freedius` — headless proxy (default)
- `freedius tui` — terminal dashboard for daily monitoring
- `freedius dashboard` — web UI for detailed analytics/config editing

#### When to use each

| Need | TUI | Web |
|------|-----|-----|
| Quick status check | ✅ | |
| Live request stream | ✅ | ✅ |
| Historical charts | | ✅ |
| Config editing | | ✅ |
| Provider comparison | | ✅ |
| Zero-dep monitoring | ✅ | |

## Decision Matrix

| Criterion | TUI (Bubble Tea) | Web (htmx+templ) | Native GUI | Hybrid |
|-----------|:-:|:-:|:-:|:-:|
| Cross-platform (Linux/macOS/Win) | 9/10 | 10/10 | 6/10 | 9/10 |
| Fits terminal-native audience | 10/10 | 6/10 | 3/10 | 9/10 |
| Dev effort (solo dev, 2-week sprints) | 7/10 | 6/10 | 4/10 | 5/10 |
| Feature richness | 6/10 | 9/10 | 8/10 | 9/10 |
| Single binary distribution | 10/10 | 9/10 | 6/10 | 9/10 |
| Real-time data display | 8/10 | 9/10 | 8/10 | 9/10 |
| Maintenance burden (low=good) | 8/10 | 6/10 | 4/10 | 5/10 |
| **Total** | **58** | **55** | **39** | **55** |

## Code References

- `proxy/translate/anthropic_openai.go` — existing SSE emit pattern, reusable for web dashboard SSE
- `main.go` — server startup, where TUI or dashboard routes would be added
- `config/config.go` — config types that TUI would display
- `proxy/proxy.go` — request handling, source of events for live monitoring

## Architecture Insights

### Event bus pattern for UI

The proxy needs an internal event bus to feed the UI (TUI or web):

```go
type Event struct {
    Timestamp time.Time
    Model     string
    Provider  string
    Latency   time.Duration
    Tokens    int
    Error     error
}

type EventBus struct {
    subscribers []chan Event
}
```

Both TUI and web UI subscribe to the same bus. This decouples the proxy from the UI layer.

### Bubble Tea integration pattern

```go
// cmd: freedius tui
func tuiCmd() *cobra.Command {
    return &cobra.Command{
        Use: "tui",
        Run: func(cmd *cobra.Command, args []string) {
            p := tea.NewProgram(newDashModel(), tea.WithAltScreen())
            p.Run()
        },
    }
}
```

### Dependencies to add

```
github.com/charmbracelet/bubbletea v1.x
github.com/charmbracelet/bubbles v0.x
github.com/charmbracelet/lipgloss v1.x
```

## Historical Context

- PRD (`context/foundation/prd.md`) explicitly states: "No web UI in v1. Config file only. Web UI is a v2 concern."
- Shape notes confirm terminal-native audience preference
- Tech stack is Go — Bubble Tea is the natural Go TUI choice

## Related Research

- No prior TUI/UI research exists in `context/changes/` or `context/archive/`

## Open Questions

1. **TUI scope for v2**: Full dashboard or just a live log viewer as MVP?
2. **Event bus persistence**: Should events be stored for historical view, or in-memory ring buffer only?
3. **Subcommand vs flag**: `freedius tui` (separate command) vs `freedius --tui` (flag on main process)?
4. **Web UI timing**: Add web dashboard in same change or defer to v3?
