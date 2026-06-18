---
date: 2026-06-19T12:00:00+02:00
planner: kiro
git_commit: 379fa15
branch: token_count
repository: pfrack/freedius
topic: "TUI Dashboard — live terminal monitoring UI with Bubble Tea"
tags: [implementation, tui, bubble-tea, event-bus, dashboard]
status: planned
last_updated: 2026-06-19
last_updated_by: kiro
---

# TUI Dashboard Implementation Plan

## Overview

Add a `freedius tui` subcommand that launches a Bubble Tea terminal dashboard showing live request stream, provider health, and active config summary. The proxy core emits summary metadata events through a decoupled event bus — the TUI subscribes without the proxy knowing about it.

## Current State Analysis

### What exists today

- **Subcommand dispatch** (`main.go:62-88`): `dispatch()` routes to `runServe`, `runInit`, `runVersion`, `runHelp` via a switch statement. Adding a fifth case follows the established pattern.
- **Middleware chain** (`main.go:215-218`): `RequestIDMiddleware` → `AccessLogMiddleware` → `RecoverMiddleware` → `Dispatcher`. New middleware slots between existing layers naturally.
- **AccessLogMiddleware** (`proxy/proxy.go:418-441`): the exact pattern to follow for event emission — wraps `wroteHeaderResponseWriter`, calls `next.ServeHTTP`, reads `ww.code` (defaults to 200), `ww.Header()` (for `X-Freedius-Matched-*`), `r.Context()` (for request ID), and `time.Since(start)`.
- **wroteHeaderResponseWriter** (`proxy/proxy.go:335-365`): captures `wroteHeader` bool and `code` int. Exposes `Header()` via embedded `http.ResponseWriter`. Needs no modification.
- **Provider Interface** (`proxy/provider.go:10-14`): `Handle(w, r, m, body) error`. The "Adapter Return Contract" (lessons.md:33-43) states adapters return nil after writing headers — no risk of events firing on failed-but-already-responded requests.
- **Config model** (`config/config.go:13-27`): `Config.Models` and `Config.Mappings` — the TUI's Config tab displays these maps directly.
- **`proxy.NewDefaultRegistry`** (`proxy/adapters_gen.go:43`): builds the standard registry with all known adapters. The TUI reuses this without modification.

### Key discoveries

- The `wroteHeaderResponseWriter` already captures `status`, `matched_provider` (via headers), and `matched_model` — no new fields needed for event metadata (`proxy/proxy.go:196-197`, `proxy/proxy.go:335-365`).
- Bubble Tea v2 uses `charm.land/bubbletea/v2` (vanity domain, not GitHub). `View()` returns `tea.View`; `tea.KeyPressMsg` replaces `tea.KeyMsg`. Bubbles has no built-in tab component — tabs are built manually with Lip Gloss `JoinHorizontal`.
- The TUI subcommand runs its own proxy instance (not a client to a running server). The Dispatcher is an `http.Handler`, not an HTTP client — there's no remote introspection API.
- Channel-based event consumption uses a producer/consumer `tea.Cmd` pair via `tea.Batch`. The consumer must be re-armed after every event in `Update()`.

### What's missing

- No event bus abstraction exists — the proxy has no mechanism to publish request metadata externally.
- No middleware slot for events — `AccessLogMiddleware` logs synchronously to `slog.Logger` only.
- No TUI package — all rendering and keyboard handling must be built from scratch.

## Desired End State

After this plan is complete:

- `freedius tui` launches a fullscreen terminal dashboard with three tabbed views: **Requests** (scrolling live log of recent proxy requests), **Providers** (health table with status/avg latency/error count per provider), and **Config** (read-only display of model mappings and endpoints).
- Each proxy request emits a summary metadata event (request_id, model, provider, status, latency_ms, matched_provider, matched_model) to an in-memory ring buffer.
- The TUI subscribes to the ring buffer channel and updates the Requests tab in real time.
- Keyboard navigation: `1`/`2`/`3` or `tab`/`shift+tab` to switch tabs; `q`/`ctrl+c`/`esc` to quit.
- A persistent stats bar at the bottom shows uptime, total requests, error count, and current error rate.
- The proxy functions identically when the event bus is nil — the bus is an optional subscriber, not a required dependency.
- `freedius serve` (headless mode) is unchanged — no TUI overhead, no event bus allocation.

## What We're NOT Doing

- **No web dashboard** — deferred to v3 per roadmap "Parked" section.
- **No historical charts or file persistence** — ring buffer only; events lost on restart.
- **No config editing in the TUI** — the Config tab is read-only display.
- **No token count data in events** — summary metadata only (request_id, model, provider, status, latency_ms).
- **No separate `freedius dashboard` command** — only `freedius tui`.
- **No monitoring of an externally-running proxy** — the TUI always runs its own proxy instance.

## Implementation Approach

1. **Event bus first** — a pure data structure with no Bubble Tea dependency. Emit/Subscribe channel-based, ring-buffered. Unit-tested in isolation.
2. **Middleware wraps it** — `EventBusMiddleware` follows the `AccessLogMiddleware` pattern exactly. When the event bus is nil, the middleware is a no-op passthrough.
3. **TUI subcommand reuses proxy wiring** — `runTUI` duplicates the `runServe` Config/Registry/Dispatcher setup, adds EventBus, replaces `http.Server.ListenAndServe` with a goroutine + Bubble Tea `tea.NewProgram`.
4. **Bubble Tea v2** — uses the `charm.land` vanity import paths. Tabbed views built with Lip Gloss. Channel events consumed via producer/consumer `tea.Cmd` pair.
5. **Test event bus + model logic** — table-driven unit tests for EventBus, Bubble Tea `Update()` state transitions. Skip terminal rendering tests (`View()` output is ANSI strings, fragile to test).

---

## Phase 1: Event Bus Infrastructure

### Overview

Create a standalone `EventBus` type with ring-buffered channel-based publish/subscribe. Pure Go — no Bubble Tea dependency, no HTTP dependency.

### Changes Required

#### 1. New file: `proxy/eventbus.go`

**Intent**: Define the `RequestEvent` struct and `EventBus` type. The bus has one subscriber channel (ring-buffered) and is safe for concurrent use from the HTTP handler goroutine and the TUI consumer goroutine.

**Contract**:

- `type RequestEvent struct` with fields: `RequestID string`, `Model string` (user-requested model name), `Provider string` (resolved provider, e.g. "nim"), `Status int`, `Latency time.Duration`, `MatchedProvider string`, `MatchedModel string`, `Timestamp time.Time`
- `type EventBus struct` — unexported fields: subscriber channel (`chan RequestEvent`), event count, mutex
- `func NewEventBus(bufferSize int) *EventBus` — creates ring-buffered subscriber channel
- `func (b *EventBus) Emit(e RequestEvent)` — non-blocking send; drops event if channel full (logs a warning once per overflow burst)
- `func (b *EventBus) Subscribe() <-chan RequestEvent` — returns the subscriber channel
- `func (b *EventBus) EventCount() int` — returns total emitted count (for stats bar)

#### 2. New file: `proxy/eventbus_test.go`

**Intent**: Table-driven unit tests covering publish/subscribe, overflow behavior, concurrent safety, nil-subscriber graceful handling.

**Contract**:
- `TestEventBus_EmitAndSubscribe` — emit events, read from subscription channel, verify order and values
- `TestEventBus_Overflow` — fill the buffer, verify oldest events are dropped, verify overflow log fires
- `TestEventBus_Concurrent` — emit from multiple goroutines, verify no data races with `-race`
- `TestEventBus_NilBus` — nil pointer receiver methods are no-ops (for the no-TUI case)

### Success Criteria

#### Automated Verification

- Unit tests pass: `go test ./proxy/ -run TestEventBus -race`
- No data races: `go test -race ./proxy/`
- Linting passes: `go vet ./proxy/`

#### Manual Verification

- None (pure data structure; covered by unit tests)

---

## Phase 2: TUI Subcommand Wiring

### Overview

Add the `freedius tui` subcommand to main.go, create a `runTUI` function that loads config, builds the proxy stack with event bus, and starts the Bubble Tea program. The proxy HTTP server runs in a background goroutine.

### Changes Required

#### 1. `main.go` — add `"tui"` case to `dispatch()`

**Intent**: Route the `"tui"` subcommand to a new `runTUI` function. Follows the existing `case "serve"`, `case "init"` pattern at `main.go:69-87`.

**Contract**: Add `case "tui": return runTUI(args)` to the switch block (~line 78).

#### 2. New file: `cmd/tui.go`

**Intent**: Implement `runTUI(args []string) int` — the subcommand entry point. Follows the same structure as `runServe` (`main.go:90-252`) but replaces `http.Server.ListenAndServe` with a goroutine + Bubble Tea program.

**Contract**:
- Creates its own `flag.FlagSet("tui", flag.ContinueOnError)` with flags: `--config` (path), `--port` (bind port), `--host` (bind host), `--log-format` (text/json), `--stream-timeout` (duration)
- Loads config via `config.Load(cfgPath)` — follows `runServe`'s `resolveConfigPath` + fallback-to-default pattern
- Creates `proxy.NewDefaultRegistry(...)`, `proxy.NewDispatcher(...)`, and the new `proxy.NewEventBus(1000)` (ring buffer size 1000)
- Wraps dispatcher in middleware chain: `RequestIDMiddleware` → `AccessLogMiddleware` → `EventBusMiddleware` → `RecoverMiddleware` → `Dispatcher`
- Starts `http.Server` in a goroutine (same timeout constants as `runServe`)
- Creates a Bubble Tea model (from Phase 3) with the event bus subscription channel
- Runs `tea.NewProgram(model, tea.WithAltScreen()).Run()` — blocks until user quits
- On quit: shuts down HTTP server with context timeout (same pattern as `runServe`'s signal handler)
- Returns 0 on success, 1 on failure

#### 3. `proxy/proxy.go` — add `EventBusMiddleware`

**Intent**: New middleware that emits a `RequestEvent` to an optional event bus after each request completes. When bus is nil, passes through as no-op.

**Contract**:
- `func EventBusMiddleware(bus *EventBus, next http.Handler) http.Handler` — follows the exact pattern of `AccessLogMiddleware` (`proxy/proxy.go:418-441`): create `wroteHeaderResponseWriter`, call `next.ServeHTTP(ww, r)`, then read `ww.code` (default 200 if 0), `ww.Header().Get("X-Freedius-Matched-Provider")`, `ww.Header().Get("X-Freedius-Matched-Model")`, `r.Context()` for request ID, `r.URL.Path`, `r.Method`, `time.Since(start)`
- When bus is nil: call `next.ServeHTTP(w, r)` directly with no wrapping or event emission
- Does NOT log request/response bodies — only metadata per NFR-Privacy

### Success Criteria

#### Automated Verification

- Compiles: `go build -o freedius .`
- `freedius tui --help` prints usage
- Linting passes: `go vet ./...`

#### Manual Verification

- `freedius tui --port 0` fails gracefully with "invalid port"
- `freedius tui` with no config file writes default config and starts (same behavior as `freedius serve`)

---

## Phase 3: Bubble Tea Models and Views

### Overview

Create the `proxy/tui/` package containing the Bubble Tea model, tab views (Requests, Providers, Config), stats bar, and event consumer loop.

### Changes Required

#### 1. New package directory: `proxy/tui/`

**Intent**: All Bubble Tea code lives in its own package, isolated from the proxy core.

#### 2. New file: `proxy/tui/model.go`

**Intent**: Define the top-level Bubble Tea model struct and the `Init`/`Update`/`View` methods. The model owns the event subscription channel and the tab state.

**Contract**:
- `type Dashboard struct` with fields: `activeTab int` (0=Requests, 1=Providers, 2=Config), `events <-chan proxy.RequestEvent`, `eventLog *ringBuffer`, `config *config.Config`, `registry *proxy.Registry`, `stats statsData`, `width int`, `height int`, `quitting bool`
- `type ringBuffer struct` — inline fixed-size slice of `proxy.RequestEvent` with head/tail indices
- `type statsData struct` — `startTime time.Time`, `totalRequests int`, `errorCount int`
- `func NewDashboard(events <-chan proxy.RequestEvent, cfg *config.Config, reg *proxy.Registry) *Dashboard`
- `func (d *Dashboard) Init() tea.Cmd` — returns `tea.Batch(listenForEvents(d.events), waitForEvent(d.events))` (producer + consumer pair)
- `func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd)` — handles: `tea.KeyPressMsg` (tab switching, quit), `tea.WindowSizeMsg` (resize), custom `requestEventMsg` (append to ring buffer, update stats). On each custom event, returns `waitForEvent(d.events)` to re-arm the consumer.
- `func (d *Dashboard) View() tea.View` — renders tab bar + active tab content + stats bar

#### 3. New file: `proxy/tui/views.go`

**Intent**: View rendering functions for each tab and the stats bar. Uses Lip Gloss for styling.

**Contract**:
- `func (d *Dashboard) renderTabs() string` — Lip Gloss `JoinHorizontal` with active/inactive tab styles. Tabs: [1] Requests, [2] Providers, [3] Config.
- `func (d *Dashboard) renderRequestsTab() string` — scrollable list of recent `RequestEvent` entries from the ring buffer, each showing: timestamp, model name, provider, status code (color-coded: 2xx green, 4xx yellow, 5xx red), latency. Most recent at top.
- `func (d *Dashboard) renderProvidersTab() string` — table of mapped providers with: provider name, protocol (from config), base URL prefix, model count. Read-only display of `config.Config.Mappings` and `config.Config.Models`. No dynamic health data in MVP (needs polling/aggregation that adds complexity without a request-tracking accumulator).
- `func (d *Dashboard) renderConfigTab() string` — read-only YAML-like display of all model mappings from `config.Config.Mappings` and `config.Config.Models`: name, provider, upstream model, base_url.
- `func (d *Dashboard) renderStatsBar() string` — single-line footer with: uptime, total requests, errors, error rate percentage. Styled with inverted colors.

#### 4. New file: `proxy/tui/styles.go`

**Intent**: Centralized Lip Gloss style definitions for the dashboard theme.

**Contract**:
- `var (activeTab, inactiveTab, statusOK, statusError, statusClientErr, statsBar, window lipgloss.Style)` — initialized in `init()` or as package-level vars with chain calls.
- Style conventions: monochrome terminal-safe (works on light and dark backgrounds), no ANSI true-color required.

#### 5. New file: `proxy/tui/model_test.go`

**Intent**: Table-driven unit tests for Bubble Tea model state transitions (Update logic only, not View rendering).

**Contract**:
- `TestDashboard_Update_KeyPress` — table of key presses and expected `activeTab` changes: `"1"` → 0, `"2"` → 1, `"3"` → 2, `"q"` → quit. Includes `"tab"` → cycle forward, `"shift+tab"` → cycle backward.
- `TestDashboard_Update_Resize` — `tea.WindowSizeMsg` updates `width` and `height` fields
- `TestDashboard_Update_EventMsg` — receiving a `requestEventMsg` appends to ring buffer and increments `stats.totalRequests`

### Success Criteria

#### Automated Verification

- Unit tests pass: `go test ./proxy/tui/ -v`
- Compiles with Bubble Tea v2 imports: `go build ./proxy/tui/`
- Linting passes: `go vet ./proxy/tui/`

#### Manual Verification

- Run `freedius tui` — see tab bar with three tabs
- Press `1`, `2`, `3` — tabs switch correctly
- Press `q` — program exits cleanly, terminal restored
- Resize terminal — layout adjusts (no panic, content re-renders)

---

## Phase 4: Integration and Manual Verification

### Overview

Wire the complete pipeline end-to-end: `freedius tui` starts the proxy, sends real requests through it, and displays them live in the TUI.

### Changes Required

#### 1. `go.mod` — add Bubble Tea v2 dependencies

**Intent**: Declare the three required Go modules.

**Contract**: Run `go get charm.land/bubbletea/v2@latest charm.land/bubbles/v2@latest charm.land/lipgloss/v2@latest` to add dependency lines.

#### 2. Wire `EventBusMiddleware` into `runServe` (optional, for testing)

**Intent**: Allow the headless `freedius serve` to also publish events when a TUI is not attached — useful for manual testing with `curl`. Not required for the TUI itself; included for developer convenience.

**Contract**: In `runServe`, change `proxy.AccessLogMiddleware(logger, httpHandler)` to the chain that only includes EventBusMiddleware when a non-nil bus is passed. In the serve path, pass `nil` bus to keep current behavior.

#### 3. `cmd/tui.go` — connect Phase 2 wiring to Phase 3 model

**Intent**: Ensure the `Dashboard` model receives the event bus subscription channel and the proxy middleware chain includes `EventBusMiddleware`.

**Contract**: Verify the flow: HTTP request → `EventBusMiddleware` → `EventBus.Emit()` → channel → `waitForEvent` Cmd → Bubble Tea `Update()` → ring buffer append → `View()` renders.

### Success Criteria

#### Automated Verification

- Full build passes: `go build -o freedius .`
- All tests pass: `go test -race ./...`
- CI check passes: `go vet ./... && go test ./... && go build .`
- No new dependencies break the module graph: `go mod tidy && go mod verify`

#### Manual Verification

1. **Basic TUI launch**: Run `freedius tui` — dashboard appears in fullscreen, stats bar shows 0 requests
2. **Live request stream**: In another terminal, `curl -X POST http://localhost:8082/v1/messages -H "Content-Type: application/json" -d '{"model":"opus","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}'` — watch the request appear in the TUI's Requests tab with model, provider, status, latency
3. **Error request**: Send a request with an unknown model `{"model":"nonexistent"}` — see the 404 error appear in the TUI with red status code
4. **Provider tab**: Press `2` — see the configured providers listed
5. **Config tab**: Press `3` — see all model mappings displayed
6. **Quit**: Press `q` — TUI exits cleanly, terminal restored, proxy server stopped
7. **Headless unchanged**: Run `freedius serve` — behavior identical to before this change (no TUI, no event bus allocation)

---

## Testing Strategy

### Unit Tests

- `proxy/eventbus_test.go`: Emit/Subscribe, overflow behavior, concurrent safety with `-race`, nil bus no-ops
- `proxy/tui/model_test.go`: KeyPressMsg handling (tab switching, quit), WindowSizeMsg resize, event message ring buffer append, stats counter increment
- No unit tests for `View()` rendering functions — ANSI output is fragile across terminal sizes

### Integration Tests

- Event bus + middleware: `proxy/middleware_test.go` — new test verifying `EventBusMiddleware` emits events with correct fields after a handler responds
- Full build: `go build -o freedius .` ensures all packages compile with Bubble Tea v2 imports

### Manual Testing Steps

1. `freedius tui` launches without errors
2. Real HTTP requests appear live in the Requests tab
3. All three tabs render content correctly
4. Stats bar updates with each request
5. Tab switching via `1`/`2`/`3` and `tab`/`shift+tab`
6. Quit via `q`, `ctrl+c`, `esc`
7. `freedius serve` headless mode unchanged
8. Resize terminal — no panic, content re-renders

## Performance Considerations

- Event bus ring buffer (1000 entries) holds ~100 KB of metadata — negligible memory
- Channel send is non-blocking (`select` with `default`) — zero proxy latency impact
- Bubble Tea idle loop consumes negligible CPU (waits on channel read)
- Bubble Tea v2 dependencies add ~2 MB to binary size (estimated)

## Migration Notes

- No existing data or config changes — the TUI is purely additive
- `freedius serve` behavior is unchanged when no event bus is wired
- Users not using the TUI see zero impact on proxy performance or behavior

## References

- Research: `context/changes/tui-dashboard/research.md` — UI approach decision, event bus pattern, Bubble Tea architecture
- Roadmap: `context/foundation/roadmap.md` — V-01 slice definition, prerequisites (v1 complete S-01–S-08)
- Lessions: `context/foundation/lessons.md` — Adapter Return Contract (hooks must handle post-WriteHeader scenarios)
- `proxy/proxy.go:418-441` — AccessLogMiddleware pattern (exact template for EventBusMiddleware)
- `proxy/proxy.go:335-365` — wroteHeaderResponseWriter (no modification needed)
- `main.go:62-88` — subcommand dispatch pattern
- `main.go:211-218` — middleware chain wiring

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Event Bus Infrastructure

#### Automated

- [ ] 1.1 Unit tests pass: `go test ./proxy/ -run TestEventBus -race`
- [ ] 1.2 No data races: `go test -race ./proxy/`
- [ ] 1.3 Linting passes: `go vet ./proxy/`

### Phase 2: TUI Subcommand Wiring

#### Automated

- [ ] 2.1 Compiles: `go build -o freedius .`
- [ ] 2.2 `freedius tui --help` prints usage
- [ ] 2.3 Linting passes: `go vet ./...`

#### Manual

- [ ] 2.4 `freedius tui --port 0` fails gracefully
- [ ] 2.5 `freedius tui` with no config writes default config and starts

### Phase 3: Bubble Tea Models and Views

#### Automated

- [ ] 3.1 Unit tests pass: `go test ./proxy/tui/ -v`
- [ ] 3.2 Compiles with Bubble Tea v2: `go build ./proxy/tui/`
- [ ] 3.3 Linting passes: `go vet ./proxy/tui/`

#### Manual

- [ ] 3.4 Tab bar renders with three tabs
- [ ] 3.5 Tab switching works (1/2/3 keys)
- [ ] 3.6 Quit works (q key), terminal restored
- [ ] 3.7 Resize terminal — layout adjusts without panic

### Phase 4: Integration and Manual Verification

#### Automated

- [ ] 4.1 Full build: `go build -o freedius .`
- [ ] 4.2 All tests: `go test -race ./...`
- [ ] 4.3 CI check: `go vet ./... && go test ./... && go build .`
- [ ] 4.4 Module graph clean: `go mod tidy && go mod verify`

#### Manual

- [ ] 4.5 TUI launches: `freedius tui` shows dashboard with 0 requests
- [ ] 4.6 Live request stream: curl request appears in Requests tab
- [ ] 4.7 Error request: 404 shows red status in TUI
- [ ] 4.8 Provider tab: configured providers listed
- [ ] 4.9 Config tab: model mappings displayed
- [ ] 4.10 Quit: terminal restored, proxy stopped
- [ ] 4.11 Headless unchanged: `freedius serve` behavior identical
