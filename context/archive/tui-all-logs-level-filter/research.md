---
date: 2026-06-20T19:39:07Z
researcher: opencode (mini)
git_commit: 5bfeaa1a35d40254b3e9655b52de90eebe244164
branch: main
repository: pfrack/freedius
topic: "TUI Log Tab: show all slog lines without special drawing + cycle-level filter shortcut"
tags: [research, tui, slog, log-filter, ringbuffer, multi-handler]
status: complete
last_updated: 2026-06-20
last_updated_by: opencode (mini)
---

# Research: TUI Log Tab — All slog lines, no styling, level-cycle filter

**Date**: 2026-06-20T19:39:07Z
**Researcher**: opencode (mini)
**Git Commit**: `5bfeaa1a35d40254b3e9655b52de90eebe244164`
**Branch**: `main`
**Repository**: `pfrack/freedius`

## Research Question

> "On the first tab in tui everything from logs everything without special drawing it. And some shortcuts to toggle which level of logs."

Clarified in conversation:

1. The **first tab** ("`[1] Log`") should show **every `slog` line emitted by the process**, not just the `AccessLogMiddleware` request events. The existing access-log entries stay; non-access slog lines are appended chronologically.
2. Rendering should be **plain text** — no lipgloss styles (no status color, no padding from `windowStyle`, no box drawing).
3. A **single cycling key** (`L`) toggles the level filter: `All → Debug → Info → Warn → Error → All`. The active level is shown in the status bar / tab label.

## Summary

Three independent moves, in dependency order:

1. **Plumb `slog` into a tee** — `main.go:52-62` (`newLogger`) is the only place a real `slog.Handler` is built. Wrap it with a custom `ringHandler` that delegates to the wrapped stderr handler (preserving today's behavior) **and** pushes a pre-rendered copy of each record into a bounded `chan logEntry`. Stdlib already ships `slog.MultiHandler` (Go 1.26.0+, current is `1.26.4` per `go.mod`), but a single-handler ring implementation is one less indirection. Capacity mirrors the existing `proxy.NewEventBus(1000)` at `main.go:185`.
2. **Mirror the EventBus pattern** — the new tee uses the same `chan + atomic.Int64 + sync.Mutex`-guarded overflow flag as `proxy/eventbus.go:31-69`. Non-blocking `select { case ch <- e: default: drop }`. Channel is the natural fit because `slog.Handler` documents *"Any of the Handler's methods may be called concurrently with itself or with other methods. It is the responsibility of the Handler to manage this concurrency."* (`pkg.go.dev/log/slog#Handler`).
3. **Wire TUI consumer + shortcut** — `Dashboard` gains a `logs <-chan logEntry` field plus a `logBuffer *ringBuffer[logEntry]`. `Init` returns both `waitForEvent(d.events)` and `waitForLog(d.logs)`. `Update` gains `case logEntryMsg`. `handleTabModeKeyPress` gains `case "L":` calling a `cycleLogLevel()` helper that mutates a `currentLogLevel` field and sets `d.stats.message`. `renderLogTab` is rewritten to render plain text from the merged buffer, with no `statusOKStyle`/`statusClientErrStyle`/`statusErrorStyle` and no `windowStyle` padding inside the body.

No key collisions — `L` is unused in tab mode today (`proxy/tui/model.go:257-323`). Help modal (`proxy/tui/help.go:8-27`) and tab label (`proxy/tui/views.go:17-21`) need a one-line update each.

## Detailed Findings

### 1. Current state of Tab 1 ("Log")

**Renderer** (`proxy/tui/views.go:34-89`):

```go
func renderLogTab(events []proxy.RequestEvent, _ int, height, scroll int) string {
    if len(events) == 0 {
        return windowStyle.Render("No requests yet...")
    }
    ...
    statusStyled := statusOKStyle.Render(statusStr)   // green < 400
    // or statusClientErrStyle.Render(statusStr)        // yellow 4xx
    // or statusErrorStyle.Render(statusStr)            // red 5xx
    line := fmt.Sprintf("time=%s request_id=%s method=%s path=%s status=%s ...", ...)
}
```

Three lipgloss styles are applied **inside** the log-tab body itself:

| Style | Definition | Where used |
|---|---|---|
| `statusOKStyle` | `styles.go:17-18` (`Foreground(Color("2"))`) | `views.go:67` (`status=NNN` when `e.Status < 400`) |
| `statusClientErrStyle` | `styles.go:20-21` (`Foreground(Color("3"))`) | `views.go:65` (`status=NNN` when `400 ≤ Status < 500`) |
| `statusErrorStyle` | `styles.go:23-24` (`Foreground(Color("1"))`) | `views.go:63` (`status=NNN` when `Status ≥ 500`) |
| `windowStyle` | `styles.go:34-35` (`Padding(0, 1)`) | `views.go:36` (empty-state) **and** `model.go:508` (whole body wrap, applies to every tab) |

`windowStyle` wraps **every** tab body in `Dashboard.View` (`model.go:508`), not just the log tab. Stripping `windowStyle` only from the log tab requires either a conditional in `View()` or a per-tab flag in the renderer — see Open Questions.

**Data source**: `d.eventLog.all()` (`model.go:495-496`), fed from `case requestEventMsg:` at `model.go:221-231`. Backed by `ringBuffer` (`model.go:31-71`), capacity 1000.

**Format contract** (asserted by `TestRenderLogTab_Format` at `model_test.go:627-655`):

```
time=HH:MM:SS request_id=<hex> method=METHOD path=PATH status=NNN duration_ms=N matched_provider=PROVIDER matched_model=MODEL[ error="..."]
```

Tests use `stripANSI(...)` (`model_test.go:691-709`) to scrub ANSI escape sequences before substring assertions.

### 2. Where every `slog` line is produced today

A grep across the codebase yields **20 production log sites** plus the process-level default logger:

| Package | Site | Level | Purpose |
|---|---|---|---|
| `main` | `main.go:158` | Error | Config path resolution failed |
| `main` | `main.go:171-177` | Info | Startup banner ("listening on…") |
| `main` | `main.go:216` | Error | `tea.NewProgram(...).Run()` returned error |
| `main` | `main.go:219` | Info | "TUI shutdown, stopping proxy" |
| `main` | `main.go:223` | Error | `server.Shutdown` error |
| `main` | `main.go:225` | Info | "shutdown complete" |
| `proxy` | `proxy/proxy.go:195` | Debug | "no match for model" |
| `proxy` | `proxy/proxy.go:212` | Error | "provider not registered" |
| `proxy` | `proxy/proxy.go:232` | Debug | "dispatch" |
| `proxy` | `proxy/proxy.go:252` | Error | "behavior not registered" |
| `proxy` | `proxy/proxy.go:274` | Warn | "adapter config error" |
| `proxy` | `proxy/proxy.go:285` | Error | "adapter transport error" |
| `proxy` | `proxy/proxy.go:300` | Error | "adapter returned error after writing response headers" |
| `proxy` | `proxy/proxy.go:352` | Error | "response encode failed" |
| `proxy` | `proxy/proxy.go:444` | Error | "panic recovered" (with stack) |
| `proxy` | `proxy/proxy.go:490` | Info | **`AccessLogMiddleware` — "request complete"** |
| `proxy` | `proxy/eventbus.go:65` | Warn | "event bus overflow, dropping events" (uses package-level `slog.Warn`) |
| `proxy` | `proxy/errors.go:168` | Debug | "client disconnect" |
| `proxy` | `proxy/errors.go:177` | Error | "upstream transport error" |
| `proxy` | `proxy/errors.go:184` | Debug | "upstream transport error detail (verbose)" |
| `proxy` | `proxy/openai_compat.go:155` | Error | "stream translation error" |
| `proxy` | `proxy/mix.go:59,62` | Debug | "mix routing" |
| `proxy` | `proxy/count_tokens_local.go:37,45,61` | Debug / Error | local BPE counter lines |
| `envinject` | `internal/envinject/settings.go:41` | Warn | "envinject: malformed existing settings.json, replacing" (uses `slog.Warn`) |

**Logger construction** (`main.go:52-62`):

```go
func newLogger(format string, w io.Writer) (logger *slog.Logger, err error) {
    opts := &slog.HandlerOptions{Level: slog.LevelInfo}   // hardcoded
    switch format {
    case "json":
        return slog.New(slog.NewJSONHandler(w, opts)), nil
    case "text":
        return slog.New(slog.NewTextHandler(w, opts)), nil
    ...
}
```

- **Single call site**: `main.go:124` (`newLogger(logFormat, os.Stderr)`).
- **Writer**: `os.Stderr` only (no stdout, no file).
- **Level**: hardcoded `slog.LevelInfo`; no flag/env exposes a level change. All `Debug` calls are currently silent.
- **Default logger installed**: `slog.SetDefault(logger)` at `main.go:128`. This is what makes the package-level `slog.Warn` calls in `eventbus.go:65` and `envinject/settings.go:41` resolve.

`Dispatcher.Logger` is `logger.With("component", "proxy")` (`proxy/proxy.go:71`); adapter loggers add their own component tags (`openai_compat.go:51`, `anthropic_compat.go:32`, `mix.go:38`). Because the tee is built **inside** `newLogger`, every downstream `With("component", ...)` call propagates correctly through the wrapper.

### 3. The `slog.Handler` interface and the tee pattern

The full interface per `pkg.go.dev/log/slog#Handler` (Go 1.26.4):

```go
type Handler interface {
    Enabled(context.Context, Level) bool
    Handle(context.Context, Record) error
    WithAttrs(attrs []Attr) Handler
    WithGroup(name string) Handler
}
```

Key contract clauses:

- `Enabled` is the **fast filter** — called before `Handle` per record. (`slog.NewTextHandler`/`slog.NewJSONHandler` use it to discard records under the configured level.)
- `Handle` may be called concurrently with itself (the docs explicitly say *"Any of the Handler's methods may be called concurrently with itself or with other methods. It is the responsibility of the Handler to manage this concurrency."*).
- The built-in `TextHandler` and `JSONHandler` already acquire a lock before `io.Writer.Write`, so stderr writes are already serialized. The wrapper only needs to make **its own** buffer push safe.
- `Record` is **not safe to retain** after `Handle` returns. The docs warn *"Copies of a Record share state."* — use `Record.Clone()` if a second handler needs the same record.
- `Record` does **not** have a `String()` method. To re-render, either delegate to a second `slog.Handler` over a `*bytes.Buffer`, or walk `r.Attrs(func(slog.Attr) bool)` yourself.

**Stdlib tee available**: Go 1.26.0 added `slog.NewMultiHandler(handlers ...Handler) *MultiHandler` ([pkg.go.dev/log/slog#MultiHandler](https://pkg.go.dev/log/slog#MultiHandler)). It fan-outs `Handle`/`WithAttrs`/`WithGroup` and ORs `Enabled` across children.

**Recommendation**: since the tee only needs two children (stderr + ring buffer), a single `ringHandler` that already wraps the stderr handler is **simpler** than `MultiHandler`. The wrapper already calls `inner.Handle(ctx, r)` for the stderr side; the buffer push is a second line in the same `Handle`. One less allocation per record and no dependency on `MultiHandler`'s level-OR semantics.

**Pre-render via second handler** (canonical idiom):

```go
buf := bufPool.Get().(*bytes.Buffer)
buf.Reset()
if err := formatH.Handle(ctx, r.Clone()); err == nil {
    line := buf.String()
    // push line + r.Level into the channel
}
bufPool.Put(buf)
```

`formatH` is a `slog.NewTextHandler(&buf, opts)` constructed once with the same `HandlerOptions` as `inner`. Because it shares the format, the ring buffer line is **byte-identical** to stderr — no risk of drift across Go versions.

### 4. Concurrency — match `proxy.EventBus`

The project's chosen pattern is in `proxy/eventbus.go:31-69`:

```go
type EventBus struct {
    ch       chan RequestEvent    // bounded, 1000
    emitted  atomic.Int64         // total attempts
    mu       sync.Mutex
    overflow bool                 // debounced warning flag
}

func (b *EventBus) Emit(e RequestEvent) {
    e.Timestamp = time.Now()
    b.emitted.Add(1)
    select {
    case b.ch <- e:
        b.mu.Lock()
        b.overflow = false
        b.mu.Unlock()
    default:
        b.mu.Lock()
        if !b.overflow {
            b.overflow = true
            slog.Warn("event bus overflow, dropping events")
        }
        b.mu.Unlock()
    }
}
```

Three properties to mirror:

1. **Non-blocking** via `select { default: drop }` — never blocks the hot path (HTTP handlers).
2. **Bounded channel** with capacity matching the existing `EventBus` (1000).
3. **`atomic.Int64` counters + mutex-guarded overflow flag** — once-per-burst warning, no log spam.

For the ring handler, the `Snapshot()` method replaces `all()` (single-goroutine reader from the TUI):

```go
func (h *ringHandler) Snapshot() []logEntry {
    out := make([]logEntry, 0, cap(h.ch))
    for {
        select {
        case e := <-h.ch:
            out = append(out, e)
        default:
            return out
        }
    }
}
```

The TUI's `Update` runs single-threaded (Bubble Tea contract), so the snapshot does not need a lock — and the `logBuffer *ringBuffer[logEntry]` in `Dashboard` is the same lock-free buffer pattern, just re-typed.

### 5. Level filtering — handler-level vs. renderer-level

| | Handler-level (`Enabled` returns false for filtered) | Renderer-level (TUI render filters snapshot) |
|---|---|---|
| **Performance** | Skips `Handle` entirely — no format, no write | Always formats & buffers |
| **Stderr impact** | ❌ filtered levels disappear from stderr too | ✅ stderr is always complete |
| **Mental model** | ❌ filter controls logging, not display | ✅ filter is a view concern |
| **Multi-format** | N/A | ✅ stored `Level` works for both text and JSON |

**Recommendation: renderer-level.** Stderr must always be the source of truth; the TUI filter is a display concern only. The ring handler's `Enabled` returns `true` unconditionally (or delegates to `inner.Enabled` for consistency). The TUI render function compares `entry.Level` against the active filter.

Filter cycle (5 states, single-letter key `L`):

| Index | Label | `slog.Level` |
|---|---|---|
| 0 | `All` | `slog.Level(-5)` (or a named constant `levelAll`) |
| 1 | `Debug` | `slog.LevelDebug` (`-4`) |
| 2 | `Info` | `slog.LevelInfo` (`0`) |
| 3 | `Warn` | `slog.LevelWarn` (`4`) |
| 4 | `Error` | `slog.LevelError` (`8`) |

`L` advances `idx = (idx + 1) % 5`. `slog.Level.String()` renders named levels as `"DEBUG"`/`"INFO"`/`"WARN"`/`"ERROR"` (uppercase); `Level(-5).String()` would render `"DEBUG-1"`, which is why the `All` state should be labeled as a literal in the TUI rather than relying on `String()`.

### 6. Keyboard surface — no collision for `L`

Existing tab-mode keys (per `model.go:257-323`):

| Bound | Free (single printable) | Free (Ctrl+) |
|---|---|---|
| `q`, `e`, `a`, `p`, `d`, `?`, `1`/`2`/`3`, `up`/`down`/`k`/`j`, `tab`/`shift+tab` | `b`, `c`, `f`, `g`, `h`, **`l`**, `m`, `n`, `o`, `r`, `s`, `t`, `u`, `v`, `w`, `x`, `y`, `z` | `ctrl+a`, `ctrl+d`, `ctrl+f`, `ctrl+g`, **`ctrl+l`**, `ctrl+r`, … |

`L` is free in tab mode (only `delete-confirm` uses `y`/`n`, which is unreachable from tab mode per `model.go:201-208` precedence).

**Help-modal capture block** at `model.go:186-193` swallows `?`/`esc` first — any new tab-mode key is naturally inert while the help overlay is open.

**Form-mode precedence** at `model.go:206-208` short-circuits to `handleFormKeyPress` — `L` will not accidentally trigger while a user is typing into a config form field. No extra guard needed.

**Pattern to mirror**: `toggleVerboseErrors` at `model.go:365-375` is the exact template — synchronous state mutation + `d.stats.message = "..."`. Status message auto-clears on next `requestEventMsg` per `model.go:228`. Tested by `TestDashboard_CtrlETogglesVerboseErrors` at `model_test.go:711-732` (22 lines, two `Update` calls, two state assertions).

```go
// Template at model.go:365-375
func (d *Dashboard) toggleVerboseErrors() {
    d.verboseErrors = !d.verboseErrors
    if d.dispatcher != nil {
        d.dispatcher.SetVerboseErrors(d.verboseErrors)
    }
    if d.verboseErrors {
        d.stats.message = "Verbose errors: ON"
    } else {
        d.stats.message = "Verbose errors: OFF"
    }
}
```

### 7. Rendering "without special drawing"

To produce plain text, the body builder at `views.go:55-87` must drop three render calls and the outer wrap:

```go
// REMOVE
statusStyled := statusErrorStyle.Render(statusStr)   // any of the three styles

// KEEP
statusStr := fmt.Sprintf("%d", e.Status)             // plain digits
```

For the **merged-buffer** version (access log lines + slog lines), the renderer takes `entries []logEntry` (or a unified `mergedBuffer` that holds either kind) and emits `entry.Line + "\n"` per row. Since `logEntry.Line` is already a pre-rendered stderr-shape string (`time=... level=INFO msg=...`), the log-tab renderer becomes a near-pure pass-through.

`windowStyle` is applied at `model.go:508` to **every** tab body, not just the log tab. Stripping it from the log tab only:

- **Option A**: Add a `styleBody bool` field on `Dashboard` and pass it into `View()`'s windowing wrap — affects the wrapper in `View()`, not the per-tab renderer.
- **Option B**: Change `windowStyle` to a no-op style (`Padding(0, 0)`) and let the log-tab renderer not call it. Cheaper, but affects every tab.

Recommend **Option A** for surgical impact (changes only the log tab's chrome). The renderer for Log would output `b.String()` directly with no `Render(...)` call.

### 8. Plumbing sketch (what the implementation phase will look like)

This is the dependency-ordered plan from the research; the actual implementation lands in a follow-up `/10x-plan` + `/10x-implement` cycle.

#### Phase A: tee handler + new ring buffer type

- New file `proxy/logtee.go` with `ringHandler` + `logEntry{Line string; Level slog.Level}` + `Snapshot()`.
- `main.go:124` swaps `newLogger(logFormat, os.Stderr)` for `newLogger(logFormat, os.Stderr, ringSink)` — or `newLogger` is restructured to return `(logger, sink)` so `main.go` can wire `sink` into the Dashboard.
- `main.go:185` neighbor: `logSink := ringHandler.NewSink(1000)` (or similar).

#### Phase B: TUI consumer

- `proxy/tui/model.go:79-108` (`Dashboard` struct): add `logs <-chan logEntry` and `logBuffer *ringBuffer[logEntry]`.
- `model.go:118-153` (`NewDashboard`): accept a `logs <-chan logEntry` parameter, store on `d`.
- `model.go:156-158` (`Init`): return `tea.Batch(waitForEvent(d.events), waitForLog(d.logs))` (or compose via `tea.Sequence`).
- `model.go:162-234` (`Update`): add `case logEntryMsg: d.logBuffer.push(entry); d.logScroll = 0`.
- `model.go:257-323` (`handleTabModeKeyPress`): add `case "L": d.cycleLogLevel()`.
- `model.go:365-375`-area: new `cycleLogLevel()` helper mirroring `toggleVerboseErrors`.

#### Phase C: merged render + plain styling

- `proxy/tui/views.go:34-89` (`renderLogTab`): signature changes from `(events []proxy.RequestEvent, …)` to `(entries []logEntry, …)`. Body builder drops all `*Style.Render` calls.
- `model.go:495-496` (`View()`): pass `mergedEntries(d.eventLog.all(), d.logBuffer.all())` — chronological merge of the two buffers by timestamp.
- `proxy/tui/model.go:507-509` (`View()`): skip `windowStyle` wrap when `activeTab == tabLog` (or use Option A from §7).

#### Phase D: discoverability

- `proxy/tui/views.go:17-21`: change `"[1] Log"` to `"[1] Log (L=level)"` and append current level (e.g. `"[1] Log [info] (L=level)"`).
- `proxy/tui/help.go:8-27`: append `{"L", "Cycle log level filter"}`.
- `README.md:84`: mention `L` in the shortcut list.

#### Phase E: tests

- `proxy/logtee_test.go` (new) — mirrors `TestAccessLogMiddleware_LogsStatusAndDuration` at `middleware_test.go:168-195` (buffer-backed `TextHandler` + substring assertions) plus a `go test -race` concurrent emit test.
- `proxy/tui/model_test.go` — append `TestDashboard_CycleLogLevel`, `TestDashboard_RenderLogTab_NoStyling`, `TestDashboard_RenderLogTab_LevelFilter`, all mirroring the existing 22-line `TestDashboard_CtrlETogglesVerboseErrors` template.

## Code References

GitHub permalinks anchor on commit `5bfeaa1`:

### Production code

- `proxy/tui/model.go:79-108` — `Dashboard` struct fields (`activeTab`, `eventLog`, `logScroll`, `stats`, etc.) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L79)
- `proxy/tui/model.go:118-153` — `NewDashboard` constructor (gains `logs <-chan logEntry` parameter) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L118)
- `proxy/tui/model.go:156-158` — `Init` (returns both event-waiters via `tea.Batch`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L156)
- `proxy/tui/model.go:162-234` — `Update` dispatch (adds `case logEntryMsg`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L162)
- `proxy/tui/model.go:221-231` — `case requestEventMsg` (template for `case logEntryMsg`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L221)
- `proxy/tui/model.go:257-323` — `handleTabModeKeyPress` (adds `case "L":`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L257)
- `proxy/tui/model.go:325-361` — `scrollUp`/`scrollDown` — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L325)
- `proxy/tui/model.go:365-375` — `toggleVerboseErrors` (template for `cycleLogLevel`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L365)
- `proxy/tui/model.go:469-520` — `Dashboard.View` (passes merged entries, conditional `windowStyle`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L469)
- `proxy/tui/model.go:522-533` — `waitForEvent` (template for `waitForLog`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model.go#L522)
- `proxy/tui/views.go:16-32` — `renderTabs` (label `[1] Log` gains level indicator) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/views.go#L16)
- `proxy/tui/views.go:34-89` — `renderLogTab` (signature changes, drops styles) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/views.go#L34)
- `proxy/tui/views.go:219-241` — `renderStatsBar` (shows `d.stats.message` for level changes) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/views.go#L219)
- `proxy/tui/styles.go:7-49` — style definitions (only `statusOKStyle`/`statusClientErrStyle`/`statusErrorStyle`/`windowStyle` are removed for log tab) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/styles.go#L7)
- `proxy/tui/styles.go:72-85` — tab and form constants — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/styles.go#L72)
- `proxy/tui/help.go:8-27` — `helpShortcuts` (add `{"L", "Cycle log level filter"}`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/help.go#L8)
- `main.go:52-62` — `newLogger` (extended to accept the ring sink) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/main.go#L52)
- `main.go:117-128` — logger construction site (`newLogger` → `slog.SetDefault`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/main.go#L117)
- `main.go:185` — `bus := proxy.NewEventBus(1000)` neighbor for the new log sink — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/main.go#L185)
- `main.go:213` — `tui.NewDashboard(bus.Subscribe(), …)` (extended with `logSink.Subscribe()`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/main.go#L213)
- `proxy/eventbus.go:31-69` — `EventBus` pattern (template for `ringHandler`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/eventbus.go#L31)
- `proxy/proxy.go:475-510` — `AccessLogMiddleware` (already produces slog lines that will flow through the new tee) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/proxy.go#L475)

### Test code (mirror patterns)

- `proxy/tui/model_test.go:25-30` — `newTestDashboard` helper — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L25)
- `proxy/tui/model_test.go:593-625` — `TestRingBuffer` (template for `TestLogRingBuffer`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L593)
- `proxy/tui/model_test.go:627-655` — `TestRenderLogTab_Format` (extended with no-style assertions) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L627)
- `proxy/tui/model_test.go:691-709` — `stripANSI` helper — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L691)
- `proxy/tui/model_test.go:711-732` — `TestDashboard_CtrlETogglesVerboseErrors` (template for `TestDashboard_CycleLogLevel`) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L711)
- `proxy/tui/model_test.go:797-805` — `TestDashboard_RequestEventClearsStatusMessage` (template for "next event clears level indicator") — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/tui/model_test.go#L797)
- `proxy/middleware_test.go:168-220` — `TestAccessLogMiddleware_LogsStatusAndDuration` (template for ring-handler tests) — [link](https://github.com/pfrack/freedius/blob/5bfeaa1/proxy/middleware_test.go#L168)

## Architecture Insights

1. **The Tee lives in `newLogger`**, not at a separate call site. Putting it at `main.go:52-62` ensures that every downstream `logger.With("component", ...)` correctly threads through both the stderr side and the ring side. Any other insertion point (wrapping `*slog.Logger` post-construction) would lose the `WithAttrs` propagation requirement.
2. **Pre-render via second handler, not custom formatter.** `slog.Record` has no `String()` method. Re-implementing text/JSON serialization is ~40 lines of formatting that duplicates stdlib and silently desyncs from stderr on Go upgrades. A second `slog.NewTextHandler(buf, opts)` over a pooled `*bytes.Buffer` is ~3 lines and guaranteed byte-identical to stderr.
3. **`slog.MultiHandler` is available but not needed.** Go 1.26.0 ships it; the project is on Go 1.26.4. But the ring handler already wraps the stderr handler — collapsing the tee into the ring handler removes one indirection. `MultiHandler` is the right call when *N* heterogeneous handlers need OR-ed `Enabled`; for two, the wrapper is simpler.
4. **Renderer-level filtering only.** `Enabled` returning `false` for filtered levels would drop stderr output. The whole point of the request is to make Tab 1 a *display* of what's already happening — not a filter on what happens. Stderr must always be complete.
5. **`status.message` is the established transient-indicator channel.** `toggleVerboseErrors` (model.go:365-375) and `installShellRC` (model.go:236-255) both use it. Auto-clears on the next `requestEventMsg`. No `tea.Tick` needed (the codebase has none anyway — confirmed by grep across `proxy/`).
6. **`L` is the cleanest available single-letter key** in tab mode. The user's "cycle-level" UX maps to exactly one keypress per state change. Single-key is also how `q`/`e`/`a`/`p`/`d` work today; keeping the convention matters more than discoverability since the level state is visible in the status bar.
7. **The "without special drawing" requirement applies only inside the log tab.** `windowStyle` wraps every body in `View()`; stripping it from log only is a one-line conditional in `View()` (or a `styleBody bool` field on `Dashboard`). The other tabs keep their chrome.
8. **Buffer capacity mirrors `EventBus(1000)`.** Same ring size means same memory ceiling (~100 lines × ~120 bytes/line ≈ 120 KB) and same "drop-oldest" semantics that users already understand from request events.

## Historical Context (from prior changes)

- **`context/changes/unified-server-logs-tab/`** — the immediate predecessor. It renamed the first tab from "Requests" to "Log" and rewrote `renderRequestsTab` as `renderLogTab` matching `AccessLogMiddleware`'s format. **This change strictly augments that work** — it doesn't replace it. The access-log lines stay; everything else (`slog` from dispatcher, adapters, recover middleware, etc.) gets added. The Progress section at `plan.md:557-650` shows manual verification is still pending — any change here should re-verify those points.
- **`context/changes/tui-statusbar-modal/`** — added the `?` help modal and re-positioned the stats bar to the top. Its `research.md:149-159` confirms the `stats.message` lifecycle (set by `installShellRC`/`toggleVerboseErrors`, cleared on next `requestEventMsg`). This is the exact pattern the level indicator should reuse — *do not invent a parallel transient-message mechanism.*
- **`context/changes/tui-error-detail-provider-defaults/`** — added verbose-error detail expansion in the Config tab. Establishes the pattern for "transient feedback after a keypress" that `cycleLogLevel` will mirror.
- **`context/changes/tui-dashboard/`** — the original TUI work. Created `Dashboard`, `ringBuffer`, `waitForEvent`, `requestEventMsg` — the entire substrate that the new `case logEntryMsg` slot into. Reference for the access-log table format and the dispatcher→TUI event flow.
- **`context/changes/tui-config-setup/`** — embedded-config startup with lazy write. Its `plan.md:69` notes that `Ctrl+S` for shell-install lives in `tabConfig` only — the same conditional pattern should be considered for `L` if it's decided the level filter should only apply on Tab 1 (current decision: applies everywhere; the level state is a *view* concern, not a tab-local concern).

### Archived context

- **`context/archive/openai-count-tokens/plan-brief.md:28`** — only prior mention of "log level" as a concept. Notes "Default `info` stays quiet; operators can opt in to count visibility" — not about runtime toggling, just a comment about using `slog.Debug`. No prior decision to revisit.
- **`context/archive/error-hardening/research.md:335`** — explicitly defers unifying `slog.Error`/`failf` to a follow-up. Not in scope for this change.

## Related Research

- `context/changes/unified-server-logs-tab/research.md` — closest prior art; established the Log tab format and `tabLog` constant.
- `context/changes/tui-statusbar-modal/research.md` — `stats.message` lifecycle and layout decisions.
- `context/changes/tui-dashboard/research.md` — the original TUI architecture and EventBus design.

## Open Questions

1. **Should the level filter reset `logScroll` to the tail on change?** Today only `requestEventMsg` (model.go:230) does this. A level change is conceptually similar — the user wants to see the *latest matching* lines after the filter narrows. Recommend: yes, mirror the `d.logScroll = 0` line. To confirm in the plan phase.
2. **Where should the active-level indicator live — `[1] Log [info]` or `Filter: info` in the status bar?** The tab label is more discoverable (always visible when on Tab 1); the status bar follows the `stats.message` precedent but competes for space with the "Verbose errors: ON" message and the toggle feedback. Recommend: tab label as primary, status-bar `stats.message` as transient confirmation on each press (e.g. `"Filter: info"`).
3. **JSON format handling**: when `--log-format=json` is set, every line in the ring buffer is a JSON object (`{"time":"...","level":"INFO","msg":"..."}`). The level-storing-in-`logEntry.Level` design handles filtering, but the displayed line will be raw JSON on Tab 1. Recommend: in JSON mode, the TUI could pretty-print or render the `msg`/`level` keys without the JSON envelope, or just accept the raw JSON as the "without special drawing" form. Needs UX decision.
4. **`windowStyle` removal scope**: Option A (`styleBody bool` on Dashboard) is surgical but introduces a new field. Option B (remove `windowStyle` from the log tab's `renderLogTab` only, leave `View()` alone) requires the renderer to take responsibility for its own padding. Recommend Option B for minimal diff — to confirm.
5. **Should `L` work in form mode?** Today `L` is unbound in `handleFormKeyPress`. If a user is editing a config field and presses `L`, it should likely insert the literal letter `L` into the text input (Bubble Tea's `textinput.Update` already handles that case at `model.go:430-432`). Confirm that the new shortcut does not need a form-mode handler — only tab-mode.
6. **Capacity for the log buffer vs. request buffer**: 1000 is the `EventBus` size, but log lines are denser (no `request_id`/`method`/`path` repetition). Should the log ring be larger? Recommend keep at 1000 for symmetry — the test `TestRingBuffer` at `model_test.go:593-625` already pins capacity behavior.
7. **Tee + package-level `slog.Warn`**: `eventbus.go:65` and `envinject/settings.go:41` use `slog.Warn` (package-level), which resolves to whatever `slog.SetDefault` returned. Once the tee is installed at `main.go:124-128`, those calls automatically fan out to the ring buffer too. Good — but if the ring handler drops a "buffer overflow" warning, the warning itself is dropped too (chicken-and-egg). Recommend: drop counter + atomic emit count make this auditable even without seeing the warning.
8. **Should `verbose-errors` interact with the level filter?** Today `Ctrl+E` toggles a separate flag that gates one Debug line in `errors.go:184`. Recommend: leave them orthogonal. `Ctrl+E` keeps its current scope (one specific line); `L` cycles the global level filter for the entire log tab.

## Sources

- [pkg.go.dev/log/slog](https://pkg.go.dev/log/slog) — Go 1.26.4 stdlib reference. Specific anchors: `#Handler` (concurrency clause), `#MultiHandler` / `#NewMultiHandler` (stdlib tee, Go 1.26.0+), `#Level` (`-4 / 0 / 4 / 8`), `#Level.String` (`"WARN"` etc.), `#Record` (`Level` field, `Attrs(func(Attr) bool)`, `Clone`), `#HandlerOptions.ReplaceAttr` (transformation, not filtering), `#example-Handler-LevelHandler` (canonical wrapping template).
- [samber/slog-multi](https://github.com/samber/slog-multi) — third-party fanout/pipe patterns. Reference for design vocabulary; not imported (stdlib `MultiHandler` covers needs).
- `/home/pawel/code/freedius/main.go`, `proxy/tui/*.go`, `proxy/eventbus.go`, `proxy/proxy.go`, `proxy/middleware_test.go` — all paths anchored at commit `5bfeaa1a35d40254b3e9655b52de90eebe244164`.