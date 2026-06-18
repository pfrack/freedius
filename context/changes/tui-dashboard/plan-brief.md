# TUI Dashboard — Plan Brief

> Full plan: `context/changes/tui-dashboard/plan.md`
> Research: `context/changes/tui-dashboard/research.md`

## What & Why

Add a `freedius tui` subcommand with a tabbed terminal dashboard showing live request stream, provider health, and config summary. Freedius's audience is terminal-native solo developers — a TUI keeps them in their workflow, ships as a single binary, and works cross-platform with zero runtime dependencies. Bubble Tea v2 is the chosen framework.

## Starting Point

Freedius v1 has a working proxy with subcommand dispatch (`main.go:62-88`), a middleware chain (`main.go:215-218`), and `AccessLogMiddleware` that already captures request metadata (status, latency, matched provider/model) after each response. The TUI builds on this by adding a decoupled event bus that both the TUI and proxy can use without coupling.

## Desired End State

- `freedius tui` launches a fullscreen dashboard with three tabs: **Requests** (scrolling live log), **Providers** (health table), **Config** (model mappings)
- Each proxy request emits summary metadata to an in-memory ring buffer → TUI updates in real time
- Keyboard navigation: `1`/`2`/`3` or `tab` for tabs, `q`/`ctrl+c` to quit
- Stats bar: uptime, total requests, errors, error rate
- `freedius serve` (headless) is unchanged — no event bus allocation, no TUI code runs

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|----------|--------|-------------------|--------|
| Primary UI framework | Bubble Tea v2 | Terminal-native audience, single binary, proven in Go CLI tools (lazygit, k9s). | Research |
| Launch mechanism | Subcommand `freedius tui` | Clean separation from serve path; follows existing init/version pattern. | Plan |
| Event persistence | In-memory ring buffer (1000 events) | Zero disk I/O, matches single-binary spirit; historical analysis deferred to v3. | Plan |
| MVP scope | Live log viewer + stats bar | Ships fast, proves event bus + TUI pipeline; additional panels added via tabs. | Plan |
| Layout | Tabbed (Requests/Providers/Config) | Full dashboard without cramming; scales to more tabs later. | Plan |
| Event granularity | Summary metadata only | Privacy-safe (no bodies), matches AccessLogMiddleware fields, lightweight. | Plan |
| Testing approach | Unit-test event bus + model Update() | Skip fragile ANSI rendering tests; table-driven state-transition tests cover logic. | Plan |
| Proxy/TUI relationship | TUI runs its own proxy instance | Dispatcher is an http.Handler, not a client; avoids IPC complexity. | Plan |
| Tabs implementation | Lip Gloss JoinHorizontal | Bubbles v2 has no tab component; tabs are built manually with styling. | Research |
| Event bus nil-safety | Passthrough when nil | Proxy must function identically in headless `freedius serve` with no bus allocation. | Plan |

## Scope

**In scope:**
- `proxy/eventbus.go` — ring-buffered event bus with Emit/Subscribe
- `proxy/proxy.go` — EventBusMiddleware (follows AccessLogMiddleware pattern)
- `cmd/tui.go` — `runTUI` subcommand wiring
- `proxy/tui/` — Bubble Tea dashboard (model, views, styles, tests)
- `main.go` — dispatch case for "tui"
- `go.mod` — Bubble Tea v2, Lip Gloss v2, Bubbles v2 deps

**Out of scope:**
- Web dashboard (v3)
- Historical charts or file persistence
- Config editing in TUI (read-only display)
- Token count data in events
- Separate `freedius dashboard` command
- Monitoring an externally-running proxy

## Architecture / Approach

```
freedius tui
├── Config.Load() → Config
├── NewDefaultRegistry() → Registry
├── NewDispatcher(Config, Registry, Logger) → http.Handler
├── NewEventBus(1000) → EventBus
│
├── HTTP goroutine:
│   http.Handler chain:
│     RequestIDMiddleware
│     → AccessLogMiddleware
│     → EventBusMiddleware  ← reads ww.code/headers, calls bus.Emit()
│     → RecoverMiddleware
│     → Dispatcher
│
└── Main goroutine (blocks):
    tea.NewProgram(Dashboard) → Model → Update(View):
        Dashboard subscribes to bus.Subscribe() channel
        waitForEvent(busCh) tea.Cmd → tea.Msg → ring buffer append → re-arm
```

The `EventBus` is the only coupling point between proxy and TUI. When nil (headless mode), `EventBusMiddleware` passes through unchanged.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Event bus infrastructure | `proxy/eventbus.go` + unit tests | Concurrent safety — must pass `-race` with multiple goroutines |
| 2. TUI subcommand wiring | `cmd/tui.go`, `main.go` dispatch case, `EventBusMiddleware` | Port conflict if user runs `serve` and `tui` simultaneously |
| 3. Bubble Tea models and views | `proxy/tui/` package — tabbed dashboard, stats bar, keyboard nav | Bubble Tea v2 import path (charm.land vanity domain) — verify Go module resolution |
| 4. Integration and verification | End-to-end wiring, live request stream, manual testing | Channel re-arm pattern — must return `waitForEvent` after each event or stream stops |

**Prerequisites:** v1 complete (S-01–S-08 on roadmap) — proxy core, all adapters, event flow stable.
**Estimated effort:** ~3 sessions across 4 phases (Phases 1-2: 1 session; Phase 3: 1 session; Phase 4: 1 session).

## Open Risks & Assumptions

- **Go 1.25+ required** for Bubble Tea v2 vanity-domain imports — freedius uses Go 1.26.4, so this is met.
- **Bubbles v2 has no tab component** — tabs built manually with Lip Gloss. Verified from Bubble Tea docs.
- **Event bus overflow** — drops oldest events silently. If the TUI can't keep up with proxy throughput, the dashboard will miss events rather than block the proxy. Acceptable for a local single-user tool.
- **Channel re-arm pattern** — Bubble Tea requires returning a new consumer `tea.Cmd` after each event; forgetting to re-arm causes the TUI to freeze. This is a well-known pitfall documented in the plan.

## Success Criteria (Summary)

- `freedius tui` launches and displays a three-tab terminal dashboard
- HTTP requests appear live in the Requests tab with model, provider, status, latency
- Tab switching works via keyboard; quit restores terminal
- `freedius serve` headless mode is unchanged
- All unit tests pass with `-race`; no data races
