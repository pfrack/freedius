# TUI Log Tab: All slog Lines + Level-Cycle Filter — Implementation Plan

## Overview

The TUI's first tab currently shows only `AccessLogMiddleware` request events, styled with lipgloss. This change rewires the process logger through a tee that fans every `slog` line into a ring buffer the TUI consumes, and switches the Log tab to plain-text rendering filtered by a user-selected level. The level cycles through `All → Debug → Info → Warn → Error → All` on a single `L` keypress, with the active level shown in the tab label.

## Current State Analysis

- **Logger construction** lives at `main.go:52-62` (`newLogger`) and is called once at `main.go:124`. The result is installed as the slog default at `main.go:128`, so package-level `slog.Warn` calls in `proxy/eventbus.go:65` and `internal/envinject/settings.go:41` resolve through it.
- **Log tab renderer** at `proxy/tui/views.go:34-89` builds a single line per `proxy.RequestEvent` and applies one of three lipgloss styles (`statusOKStyle`/`statusClientErrStyle`/`statusErrorStyle`) to the `status=NNN` substring. The whole body is wrapped in `windowStyle` (which adds `Padding(0, 1)`) at `model.go:508`.
- **EventBus pattern** at `proxy/eventbus.go:31-69` is the established template: `chan + atomic.Int64 + mutex-guarded overflow flag + non-blocking select`. The new log sink mirrors it.
- **Stats are fed by the EventBus** via `case requestEventMsg` at `model.go:221-231` (`totalRequests`, `errorCount`). The `eventLog` ring buffer is used only for display.
- **No key collision for `L`**: every printable single letter is enumerated in `model.go:257-323`; `l` is free.
- **Status message pattern**: `toggleVerboseErrors` at `model.go:365-375` shows the pattern for "transient feedback after a keypress" using `d.stats.message`.

### Key Discoveries:

- The same `slog` line written by `AccessLogMiddleware` at `proxy/proxy.go:490` will already be in the new slog ring buffer — the `eventLog` (RequestEvent-shaped) becomes redundant for display once the slog buffer exists. Stats counting is the only remaining use of the EventBus path (`research.md:38-39` confirmed, user-selected in planning).
- `slog.NewMultiHandler` is available in Go 1.26.0+ (`research.md:155`) but a single `ringHandler` wrapper around the stderr handler is one less indirection (`research.md:157`). Project is on Go 1.26.4 per `go.mod`.
- `slog.Record` is not safe to retain past `Handle` return — the tee must `r.Clone()` before passing to the format child handler (`research.md:152`).
- `windowStyle` wraps **every** tab body in `View()` — surgical removal needs a per-Dashboard flag, not a renderer-local change (the wrap sits in the parent, not the child).

## Desired End State

When this plan is complete:

- Every `slog` line emitted by the process (startup banner, access logs, dispatcher debug, error middleware, event bus warnings, envinject warnings) appears on the TUI's Log tab in chronological order, rendered as plain text with no color or padding.
- Pressing `L` in tab mode cycles the level filter `All → Debug → Info → Warn → Error → All`. The active level is visible in the tab label `[1] Log [<level>]`. Filtered-out levels are hidden from the Log tab; stderr is unaffected.
- `EventBus` continues to feed `totalRequests`/`errorCount`. The `eventLog` ring buffer is removed (it was only used for display, which is now sourced from the slog buffer).
- The `?` help modal lists `L` as a shortcut. The README mentions it.

### Key Discoveries (re-stated for the end state):

- The Log tab is no longer a "requests only" view; it is the unified process log, including requests.
- The change does not introduce any new flag, env var, or persistent config — the level filter is a TUI-only view concern.

## What We're NOT Doing

- **No change to the slog level on stderr.** The TUI filter is a *display* concern; stderr must remain the operator's source of truth. Filtering at the `Handler.Enabled` boundary (which would silence stderr) is explicitly rejected (`research.md:233-237`).
- **No new log format flag.** `--log-format=json|text` already exists; we do not add a TUI-specific format.
- **No scroll-into-view changes.** The existing `d.logScroll = 0` on `case requestEventMsg` and on level change is enough; we do not add auto-scroll on every slog line (would be too jumpy).
- **No persistence of the level filter across restarts.** The level resets to `All` each launch.
- **No change to the EventBus, AccessLogMiddleware, or the access-log format contract.** They continue to emit `slog.Info("request complete", ...)` and `RequestEvent` independently; we only stop *displaying* the `RequestEvent` shape on the Log tab.

## Implementation Approach

Three phases, each shippable on its own:

1. **Tee handler** — new `proxy/logtee.go` with `ringHandler` + `LogSink`. `main.go` constructs the sink alongside the logger. The handler is a single wrapper that delegates to the existing stderr handler **and** to a text-format child handler writing into the sink's channel. Output: a fully-tested `LogSink` with `Snapshot()`; the rest of the system is unchanged.
2. **TUI consumer + plain-text rendering** — `Dashboard` gains a `logs` channel, a `logBuffer *ringBuffer[LogEntry]`, a `styleBody bool`, and consumes slog entries via `waitForLog`. `renderLogTab` switches to the slog buffer and drops every `*Style.Render` call from the body. `View()` conditionally wraps with `windowStyle` based on `styleBody`. `renderTabs` adds the level indicator. Output: Log tab shows every slog line, plain text, with the level in the tab label.
3. **Level filter (`L` key)** — `LogFilter` type with 5 states, `cycleLogLevel()` helper, `case "L":` in `handleTabModeKeyPress`, filter applied at render time, help modal updated, README updated. Output: fully interactive level cycling.

The EventBus subscription stays in `Dashboard` solely to drive `totalRequests`/`errorCount` (matches the user-selected "EventBus for stats, slog buffer for display" architecture). The `eventLog` field and the `renderLogTab` request-event path are removed in Phase 2.

## Critical Implementation Details

- **`slog.Record` lifetime** — the tee's `formatH.Handle` MUST be called with `r.Clone()`. The first child (`inner`) consumes the original; `Record` docs explicitly state copies share state and are not safe to retain past `Handle` return. A `sync.Pool` of `*bytes.Buffer` keyed to the format handler avoids per-record allocation.
- **Tee ordering** — call `inner.Handle` first (stderr) then `formatH.Handle` (ring buffer). If stderr is the operator's source of truth, the operator should see the line even if the buffer push races. The channel is non-blocking, so the ring push can never block the stderr path.
- **Filter level semantics** — `slog.Level` is ordered `Debug (-4) < Info (0) < Warn (4) < Error (8)`. Filter "Info" shows `entry.Level >= slog.LevelInfo` (hides Debug). Filter "All" shows everything regardless of level. The `All` sentinel uses `Min == nil` rather than a special `slog.Level` value to avoid clashing with `slog.Level(-5)` rendering as `"DEBUG-1"` via `Level.String()`.
- **Scroll reset on level change** — pressing `L` sets `d.logScroll = 0` to follow the same "show the latest matching entries" pattern as `case requestEventMsg` (line 230). This is the only state mutation beyond `currentLogLevel`.

## Phase 1: Tee Handler + Ring Sink

### Overview

Add a custom `slog.Handler` that wraps the existing stderr handler and also pushes a pre-rendered copy of each record into a bounded channel. Wire the channel through `main.go` so the consumer (Phase 2) can `Subscribe()` to it. Mirror the `EventBus` overflow semantics: non-blocking `select { default: drop }` with a once-per-burst `slog.Warn` (via the wrapped stderr handler, which now goes through the tee — chicken-and-egg avoided by holding the overflow flag on the sink, not on the inner handler).

### Changes Required:

#### 1. New file: `proxy/logtee.go`

**File**: `proxy/logtee.go`

**Intent**: Define `LogEntry`, `LogSink`, and `ringHandler` — a `slog.Handler` that fans every record out to (a) the wrapped stderr handler and (b) a text-format child handler writing into a pooled `*bytes.Buffer`. The pre-rendered text line is what consumers see, regardless of the original `--log-format` flag.

**Contract**:
- `type LogEntry struct { Time time.Time; Level slog.Level; Line string }` — `Time` is the time the record was emitted, `Level` is `r.Level`, `Line` is the text-handler output (always text-shaped, never JSON).
- `type LogSink struct { ch chan LogEntry; emitted atomic.Int64; mu sync.Mutex; overflow bool }` — same shape as `EventBus` (`proxy/eventbus.go:31-36`).
- `func NewLogSink(capacity int) *LogSink` — capacity mirrors `EventBus(1000)` (`main.go:185`).
- `func (s *LogSink) Subscribe() <-chan LogEntry` — read-only channel handed to the TUI.
- `func (s *LogSink) Snapshot() []LogEntry` — non-blocking drain used by the TUI's `waitForLog` (single-goroutine consumer; no lock needed at the snapshot site because the channel is the synchronization point).
- `func (s *LogSink) EventCount() int64` — total emits (including drops), mirrors `EventBus.EventCount` for the warning surface.
- `type ringHandler struct { inner slog.Handler; formatH slog.Handler; bufPool sync.Pool; sink *LogSink }` — implements `Enabled(ctx, level)`, `Handle(ctx, r)`, `WithAttrs(attrs)`, `WithGroup(name)`.
  - `Handle`: stamp `r.Time`, call `inner.Handle(ctx, r)` first (stderr), then `formatH.Handle(ctx, r.Clone())` to render the line into the pooled buffer, then non-blocking send to `sink.ch`. Reset the buffer in a `defer` and return it to the pool.
  - `Enabled`: returns `inner.Enabled(ctx, level) OR true` (the tee never drops at the handler boundary — filtering is a renderer concern).
  - `WithAttrs`/`WithGroup`: returns a new `ringHandler` with both children wrapped via the same methods.
- The `sync.Pool` is created in `NewLogSink` and shared across all `ringHandler` instances (the format handler is the same instance, so one pool per `ringHandler` is fine — but sharing across `WithAttrs` clones is necessary to avoid per-attr allocation).

#### 2. Logger construction takes the sink

**File**: `main.go`

**Intent**: Construct the sink before `newLogger` so the handler can be wired in. Surface the sink as a return so `main.go` can hand it to the TUI.

**Contract**:
- `func newLogger(format string, w io.Writer, sink *LogSink) (*slog.Logger, error)` — added `sink` parameter. Builds the inner `TextHandler`/`JSONHandler` over `w`, then a text-format child over a fresh `*bytes.Buffer` (the `formatH`), then the `ringHandler` wrapping both, then `slog.New(ringHandler)`.
- `main.go:124` area (logger construction) becomes:
  ```go
  logSink := proxy.NewLogSink(1000)
  logger, err := newLogger(logFormat, os.Stderr, logSink)
  if err != nil { ... }
  slog.SetDefault(logger)
  ```
- `main.go:213` area (TUI construction) gains `logSink.Subscribe()` and passes it to `tui.NewDashboard` (Phase 2 wires the parameter).

#### 3. New tests: `proxy/logtee_test.go`

**File**: `proxy/logtee_test.go`

**Intent**: Verify the tee fans out, drops on overflow, and is race-safe. Mirror the buffer-backed `TestAccessLogMiddleware_LogsStatusAndDuration` pattern at `proxy/middleware_test.go:168-220`.

**Contract**:
- `TestRingHandler_EmitsToStderrAndBuffer` — feed a record through the tee, assert stderr captured the formatted line AND `Snapshot()` returned it.
- `TestRingHandler_PreRenderIsTextShapeRegardlessOfStderrFormat` — construct two tees, one with stderr format `json`, one with `text`. Both should produce identical `LogEntry.Line` values.
- `TestRingHandler_DropsOnOverflow` — fill a sink of capacity N, send N+M records, assert `Snapshot()` returns at most N, assert `EventCount()` returns N+M.
- `TestRingHandler_Concurrent` — `go test -race` safety: 8 goroutines × 1000 records each into a single sink; assert no panic, assert `Snapshot()` plus the cumulative drop count equal `EventCount()`.
- `TestRingHandler_Enabled` — `Enabled` returns true even when `inner.Enabled` returns false (tee is permissive).

### Success Criteria:

#### Automated Verification:

- `go build ./...` — compiles cleanly.
- `go vet ./...` — no new vet warnings.
- `go test -race ./proxy/...` — all proxy tests pass with the race detector on.
- `go test -run TestRingHandler ./proxy/...` — the new tests pass.

#### Manual Verification:

- Run `./freedius --log-format=json` and trigger an HTTP request. Confirm stderr shows JSON lines for the request and the startup banner. (The TUI will not yet show anything from this sink — Phase 2 wires the consumer.)
- Run `./freedius --log-format=text` and confirm stderr continues to show text lines identically to before this change.
- Check that `slog.SetDefault` package-level calls (event bus overflow, envinject malformed settings) still resolve to stderr.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that stderr behavior is unchanged before proceeding to the next phase. Phase blocks use plain bullets — the corresponding `- [ ]` checkboxes for these items live in the `## Progress` section at the bottom of the plan.

---

## Phase 2: TUI Consumer + Plain-Text Rendering

### Overview

Wire the new `LogSink` into `Dashboard` so every slog line becomes a `LogEntry` in a new `logBuffer` ring. Rewrite `renderLogTab` to render those entries as plain text, drop the lipgloss styles from the body, and surgically strip `windowStyle` from the Log tab only. Add the level indicator to the tab label. The `eventLog` field is removed — the slog buffer is the single source of truth for display; the EventBus path is kept only for `totalRequests`/`errorCount` stats.

### Changes Required:

#### 1. `Dashboard` struct gains slog fields, drops `eventLog`

**File**: `proxy/tui/model.go`

**Intent**: Add the slog consumer fields; remove `eventLog` (it was only used for display, which is now sourced from the slog buffer). Add a `styleBody` flag for the surgical `windowStyle` strip.

**Contract**:
- Remove: `eventLog *ringBuffer` (line 82).
- Add: `logs <-chan proxy.LogEntry` (line 81 area), `logBuffer *ringBuffer[proxy.LogEntry]` (new generic ring; see point 2), `styleBody bool` (defaults to `true`; set to `false` when `activeTab == tabLog` in `View()`), `currentLogLevel LogFilter` (added in Phase 3; declared here so the struct stays consistent).

#### 2. Generic `ringBuffer[T any]`

**File**: `proxy/tui/model.go`

**Intent**: Make the existing `ringBuffer` type generic so it can hold either `RequestEvent` (no remaining use) or `LogEntry` (new use). The existing `RequestEvent`-specific ring is replaced.

**Contract**:
- `type ringBuffer[T any] struct { buf []T; head int; size int; cap int }`.
- Existing methods (`newRingBuffer`, `push`, `all`) become generic. The `TestRingBuffer` test at `proxy/tui/model_test.go:593-625` still compiles because the field access uses `RequestID`, which is on `RequestEvent` — that test will be deleted (it tested a now-removed code path) and replaced with a `TestLogRingBuffer` that uses `LogEntry.Line` instead.

#### 3. `NewDashboard` accepts the log channel

**File**: `proxy/tui/model.go`

**Intent**: Accept the slog channel as a parameter; construct the new ring buffer.

**Contract**:
- Signature: `func NewDashboard(events <-chan proxy.RequestEvent, logs <-chan proxy.LogEntry, cfg *config.Config, reg *proxy.Registry, dispatcher *proxy.Dispatcher, cfgPath, host string, port int, verboseErrors bool) *Dashboard`.
- `d.logs = logs`, `d.logBuffer = newRingBuffer[proxy.LogEntry](1000)`, `d.currentLogLevel = filterAll` (Phase 3 constant).
- Existing panic checks for nil cfg/reg/dispatcher stay.

#### 4. `Init` returns both event waiters

**File**: `proxy/tui/model.go`

**Intent**: Compose the new log waiter alongside the existing event waiter.

**Contract**:
- `Init()` returns `tea.Batch(waitForEvent(d.events), waitForLog(d.logs))` when both channels are non-nil; falls back to either one alone for nil-tolerant tests.

#### 5. New `waitForLog` command

**File**: `proxy/tui/model.go`

**Intent**: Mirror `waitForEvent` at `model.go:522-533` for the slog channel.

**Contract**:
- `func waitForLog(ch <-chan proxy.LogEntry) tea.Cmd` — returns a `tea.Cmd` that reads one entry, wraps it in a `logEntryMsg proxy.LogEntry` typed message, and returns nil on closed channel.

#### 6. `Update` handles `logEntryMsg`

**File**: `proxy/tui/model.go`

**Intent**: Push new log entries into the ring buffer; do not reset `logScroll` per line (would be too jumpy — the existing `case requestEventMsg` resets it because requests are the primary scroll driver on Tab 1; slog lines include those requests but at a much higher rate).

**Contract**:
- New case before the closing `}` of the type switch (around line 232):
  ```go
  case logEntryMsg:
      d.logBuffer.push(proxy.LogEntry(msg))
      return d, waitForLog(d.logs)
  ```
- `type logEntryMsg proxy.LogEntry` declared near `requestEventMsg` at line 22.

#### 7. `View()` conditionally wraps with `windowStyle`

**File**: `proxy/tui/model.go`

**Intent**: Set `styleBody` to `false` for the Log tab, and skip the `windowStyle` wrap in that case.

**Contract**:
- Before the `switch d.activeTab` block (line 494), add: `d.styleBody = d.activeTab != tabLog`.
- Replace line 508 `body := windowStyle.Width(max(width-2, 0)).Render(content)` with:
  ```go
  var body string
  if d.styleBody {
      body = windowStyle.Width(max(width-2, 0)).Render(content)
  } else {
      body = content
  }
  ```
- The other two tabs (Providers, Config) keep their chrome unchanged.

#### 8. `renderLogTab` rewritten to plain text + level filter

**File**: `proxy/tui/views.go`

**Intent**: Render the slog buffer's entries as plain text (no lipgloss), apply the active level filter, and keep the chronological ordering. The renderer is a near-pure pass-through over the pre-rendered lines.

**Contract**:
- Signature: `func renderLogTab(entries []proxy.LogEntry, _ int, height, scroll int, filter LogFilter) string`.
- Body: iterate `entries`, apply the filter (`filter.Min == nil || entry.Level >= *filter.Min`), slice for the visible window using the same `available := height - 4` math (line 39), and `b.WriteString(entry.Line + "\n")` per visible row. No `*Style.Render` calls. The empty state becomes `"No log entries yet..."` (replaces `"No requests yet..."` from `views.go:36`).
- `View()` at `model.go:496` becomes:
  ```go
  case tabLog:
      content = renderLogTab(d.logBuffer.all(), width, bodyHeight, d.logScroll, d.currentLogLevel)
  ```

#### 9. `renderTabs` shows the level indicator

**File**: `proxy/tui/views.go`

**Intent**: Append the active level to the first tab's label.

**Contract**:
- Signature: `func renderTabs(active int, width int, level LogFilter) string`.
- First tab label: `"[1] Log [" + level.Label + "]"` — e.g. `"[1] Log [info]"`, `"[1] Log [all]"`.
- `View()` at `model.go:507` becomes `tabs := renderTabs(d.activeTab, width, d.currentLogLevel)`.

#### 10. `main.go` wires `logSink.Subscribe()` to `NewDashboard`

**File**: `main.go`

**Intent**: Pass the slog channel into the TUI constructor.

**Contract**:
- Line 213 area: `model := tui.NewDashboard(bus.Subscribe(), logSink.Subscribe(), cfg, registry, dispatcher, cfgPath, host, port, verboseErrors)`.

#### 11. New tests in `proxy/tui/model_test.go`

**File**: `proxy/tui/model_test.go`

**Intent**: Verify the new consumer path and the plain-text rendering.

**Contract**:
- Delete the old `TestRingBuffer` at `model_test.go:593-625` (it tests a now-removed type) and replace with `TestLogRingBuffer` that exercises `ringBuffer[proxy.LogEntry]`.
- Delete the old `TestRenderLogTab_Format` at `model_test.go:627-655`, `TestRenderLogTab_Empty` at `model_test.go:657-662`, `TestRenderLogTab_ErrorSuffix` at `model_test.go:664-679` (they test a removed `RequestEvent`-shaped renderer) and replace with:
  - `TestRenderLogTab_Empty` — `renderLogTab(nil, 80, 24, 0, filterAll)` contains `"No log entries"`.
  - `TestRenderLogTab_RendersLogLines` — push three `LogEntry` records with different levels, assert all three lines appear in the output.
  - `TestRenderLogTab_NoStyling` — assert `stripANSI(output)` equals `output` (no ANSI escapes — no lipgloss styles applied).
  - `TestRenderLogTab_AppliesFilter` — push `Debug` and `Error` entries, render with `filterError`, assert only the Error line appears.
- Add `TestDashboard_LogBufferReceivesEntries` — construct a dashboard with a non-nil `logs` channel, push a `logEntryMsg`, assert `d.logBuffer.all()` contains the entry.
- Update `newTestDashboard` at `model_test.go:25-30` to pass a `nil` `logs` channel and initialize `d.logBuffer = newRingBuffer[proxy.LogEntry](1000)`.
- Update `TestRenderTabs_LabelIsLog` at `model_test.go:681-689` to call `renderTabs(0, 80, filterAll)` and assert `"[1] Log [all]"` appears.
- Update `TestDashboard_Update_KeyPress` at `model_test.go:32-` to use the new `NewDashboard` signature.

### Success Criteria:

#### Automated Verification:

- `go build ./...` — compiles cleanly.
- `go vet ./...` — no new vet warnings.
- `go test ./proxy/tui/...` — all TUI tests pass; the rewritten render tests cover plain-text + filter behavior.
- `go test -race ./...` — all tests pass with the race detector on.
- The test for `TestRenderLogTab_NoStyling` confirms the body has zero ANSI escapes.

#### Manual Verification:

- Start `./freedius`, send an HTTP request, switch to Tab 1. The Log tab shows every line that appeared on stderr, in chronological order, with no color and no left/right padding.
- Switch to Tab 2 and Tab 3 — confirm their layout is unchanged (chrome still wraps the body).
- Resize the terminal — confirm the Log tab re-flows without padding artifacts.
- Trigger a deliberate `slog.Warn` (e.g. hit `/v1/messages` with an unmapped model) — confirm the line appears on Tab 1, not just on stderr.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the Log tab renders as expected before proceeding to Phase 3.

---

## Phase 3: Level Filter (L Key)

### Overview

Add the `L` key to cycle the level filter through `All → Debug → Info → Warn → Error → All`, surface the active level in the tab label (already wired in Phase 2), update the help modal, and update the README. The level filter is applied at render time; the slog buffer holds every line.

### Changes Required:

#### 1. `LogFilter` type + 5 cycle states

**File**: `proxy/tui/loglevel.go` (new)

**Intent**: Define the filter as a value type with a `Label` and a `Min` pointer to `slog.Level`. The `Min == nil` case is the "All" sentinel.

**Contract**:
```go
type LogFilter struct {
    Label string
    Min   *slog.Level
}

var (
    slogLevelDebug = slog.LevelDebug
    slogLevelInfo  = slog.LevelInfo
    slogLevelWarn  = slog.LevelWarn
    slogLevelError = slog.LevelError

    filterAll   = LogFilter{Label: "all", Min: nil}
    filterDebug = LogFilter{Label: "debug", Min: &slogLevelDebug}
    filterInfo  = LogFilter{Label: "info", Min: &slogLevelInfo}
    filterWarn  = LogFilter{Label: "warn", Min: &slogLevelWarn}
    filterError = LogFilter{Label: "error", Min: &slogLevelError}
)

var logFilterCycle = []LogFilter{filterAll, filterDebug, filterInfo, filterWarn, filterError}

func (f LogFilter) Matches(level slog.Level) bool {
    return f.Min == nil || level >= *f.Min
}
```

#### 2. `cycleLogLevel` helper on `Dashboard`

**File**: `proxy/tui/model.go`

**Intent**: Advance the cycle, reset `d.logScroll` so the user sees the latest matching entries.

**Contract**:
- `func (d *Dashboard) cycleLogLevel()` — find current index in `logFilterCycle`, advance by 1 modulo `len`, set `d.currentLogLevel`, set `d.logScroll = 0`. No `d.stats.message` mutation (user-selected: tab label is the only indicator).

#### 3. `L` key in `handleTabModeKeyPress`

**File**: `proxy/tui/model.go`

**Intent**: Wire the cycling key into the tab-mode handler.

**Contract**:
- New case inside the switch at `model.go:258-321`:
  ```go
  case "l":
      d.cycleLogLevel()
      return d, nil
  ```
- No form-mode handler — `handleFormKeyPress` at `model.go:377-434` falls through to the textinput update at line 430-432, which inserts a literal `l` into the focused field. This matches the existing pattern for unbound letters.

#### 4. Help modal entry

**File**: `proxy/tui/help.go`

**Intent**: Document the new shortcut in the `?` modal.

**Contract**:
- Append `{"L", "Cycle log level filter"}` to `helpShortcuts` (line 8-27), placed after the `"Ctrl+E"` entry to keep modifier-keys together with other modifier keys, and before the form-mode cluster.

#### 5. README mention

**File**: `README.md`

**Intent**: Add the `L` shortcut to the existing shortcut sentence at line 84.

**Contract**:
- Update the sentence "Press `Ctrl+E` to toggle verbose errors and `Ctrl+S` in Config to install the shell env block." to "Press `Ctrl+E` to toggle verbose errors, `Ctrl+S` in Config to install the shell env block, and `L` to cycle the Log tab's level filter."

#### 6. Tests for the filter and cycle

**File**: `proxy/tui/model_test.go`

**Intent**: Cover the cycle, the filter, the keyboard handler, and the help modal.

**Contract**:
- `TestLogFilter_Matches` — table-driven: `(filterAll, Debug) == true`, `(filterInfo, Debug) == false`, `(filterInfo, Info) == true`, `(filterError, Warn) == false`, `(filterError, Error) == true`.
- `TestDashboard_CycleLogLevel` — mirror `TestDashboard_CtrlETogglesVerboseErrors` at `model_test.go:711-732`:
  1. Initial `d.currentLogLevel == filterAll`.
  2. Press `L` → `filterDebug`.
  3. Press `L` → `filterInfo`.
  4. Press `L` → `filterWarn`.
  5. Press `L` → `filterError`.
  6. Press `L` → `filterAll` (cycle wraps).
  7. Assert `d.logScroll == 0` after each press.
- `TestDashboard_LKeyInTabMode` — full Update call with `tea.KeyPressMsg{Text: "l"}`, assert `d.currentLogLevel == filterDebug` afterwards. No form-mode short-circuit, no quit.
- `TestDashboard_LKeyInsertsInFormMode` — set `d.formMode = formEditProvider`, press `L`, assert `d.formFields[0].Value()` contains `"l"`. Assert `d.currentLogLevel` is still `filterAll`.
- `TestRenderLogTab_AppliesFilter` (already added in Phase 2) — assert that `renderLogTab(entries, ..., filterError)` hides Debug/Info/Warn entries.
- `TestHelpShortcuts_ContainsL` — assert `helpShortcuts` contains `{"L", "Cycle log level filter"}`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` — compiles cleanly.
- `go vet ./...` — no new vet warnings.
- `go test ./...` — all tests pass.
- `go test -race ./...` — race detector clean.
- `gofumpt -l .` — no unformatted files (project's stricter-than-gofmt style; CI enforces).

#### Manual Verification:

- Start `./freedius`, switch to Tab 1, press `L` repeatedly. Confirm the tab label cycles `[all] → [debug] → [info] → [warn] → [error] → [all]` and the visible entries narrow accordingly.
- With filter `error`, send a request that triggers a `slog.Warn` (e.g. event bus overflow, envinject warning) — confirm it is hidden from the Log tab but still appears on stderr.
- With filter `all`, switch to Tab 2, press `L` — confirm nothing happens (no form, no effect on other tabs).
- Open the `?` help modal — confirm `L` is listed.
- Restart `freedius` — confirm the filter resets to `[all]` (no persistence).

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation that the `L` cycle works end-to-end and that stderr remains unaffected by the TUI filter.

---

## Testing Strategy

### Unit Tests:

- `proxy/logtee_test.go` — tee delegation, overflow, race-safety, format-independence of pre-render.
- `proxy/tui/model_test.go`:
  - `TestLogRingBuffer` — generic ring buffer (replaces `TestRingBuffer`).
  - `TestRenderLogTab_Empty` / `TestRenderLogTab_RendersLogLines` / `TestRenderLogTab_NoStyling` / `TestRenderLogTab_AppliesFilter`.
  - `TestDashboard_LogBufferReceivesEntries` — slog → ring buffer.
  - `TestLogFilter_Matches` — filter table.
  - `TestDashboard_CycleLogLevel` — 5-step cycle.
  - `TestDashboard_LKeyInTabMode` / `TestDashboard_LKeyInsertsInFormMode` — keyboard handler.
  - `TestHelpShortcuts_ContainsL` — help modal.
  - `TestRenderTabs_LabelIsLog` — updated to include the level.

### Integration Tests:

- Existing `TestAccessLogMiddleware_LogsStatusAndDuration` at `proxy/middleware_test.go:168-220` is unaffected — the access log format is unchanged.
- Existing `TestDashboard_CtrlETogglesVerboseErrors` at `model_test.go:711-732` and `TestDashboard_RequestEventClearsStatusMessage` at `model_test.go:797-805` are unaffected — the verbose-errors feature and request-event status clearing are orthogonal to this change.

### Manual Testing Steps:

1. Start `./freedius --log-format=text` and confirm stderr looks identical to pre-change.
2. Start `./freedius --log-format=json` and confirm stderr is JSON.
3. Send a successful `POST /v1/messages` — confirm the access log line appears on Tab 1 in plain text and on stderr.
4. Trigger a dispatch warning (e.g. unmapped model) — confirm the warning appears on Tab 1, and remains on stderr.
5. Press `L` five times — confirm tab label cycles correctly and the visible entries narrow/expand.
6. Open `?` help — confirm `L` is listed.
7. Check `README.md` line 84 mentions `L`.
8. Resize the terminal to a narrow width — confirm the Log tab still flows without padding artifacts.
9. Run `go test -race ./...` and confirm clean.

## Performance Considerations

- The tee allocates one `slog.Record.Clone()` and one `*bytes.Buffer` per record. The buffer is pooled; the clone is unavoidable because the inner handler retains nothing past `Handle` return. This is the minimum work to keep stderr and the ring buffer in sync.
- `Snapshot()` is O(N) per call where N ≤ 1000. The TUI's `Update` is rate-limited by the message loop; in practice, `Snapshot()` runs only on a fresh `logEntryMsg` (Phase 2 point 6) — the `View()` side reads `d.logBuffer.all()` which is the already-snapshotted ring buffer. No additional O(N) per render.
- The level filter is O(N) at render time (one comparison per entry). At N=1000 and 60 FPS, that's 60k comparisons/second — negligible.

## Migration Notes

- No data migration. The `eventLog` field is removed in Phase 2; tests using it are updated in the same phase. No config files or env vars change.
- No rollback strategy beyond reverting the commits — the change is local to the TUI and the logger construction; stderr behavior is preserved by Phase 1's contract (tee delegation).

## References

- Related research: `context/changes/tui-all-logs-level-filter/research.md`
- Predecessor change: `context/changes/unified-server-logs-tab/` — established the `tabLog` constant and `renderLogTab` skeleton
- `proxy/tui/statusbar-modal/` — `stats.message` lifecycle (not used by this change; user-selected tab-label-only)
- `proxy/tui/error-detail-provider-defaults/` — `toggleVerboseErrors` pattern (template for `cycleLogLevel`)
- Stdlib docs: `pkg.go.dev/log/slog#Handler` (concurrency clause), `#MultiHandler` (rejected, see research.md:155-157), `#Record.Clone` (required for tee, see research.md:152)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Tee handler + ring sink

#### Automated

- [x] 1.1 `go build ./...` — compiles cleanly
- [x] 1.2 `go vet ./...` — no new vet warnings
- [x] 1.3 `go test -race ./proxy/...` — all proxy tests pass with the race detector on
- [x] 1.4 `go test -run TestRingHandler ./proxy/...` — the new tests pass

#### Manual

- [ ] 1.5 `./freedius --log-format=json` — stderr shows JSON lines for the request and the startup banner
- [ ] 1.6 `./freedius --log-format=text` — stderr shows text lines identically to before
- [ ] 1.7 Package-level `slog.Warn` calls (event bus overflow, envinject) still resolve to stderr

### Phase 2: TUI consumer + plain-text rendering

#### Automated

- [ ] 2.1 `go build ./...` — compiles cleanly
- [ ] 2.2 `go vet ./...` — no new vet warnings
- [ ] 2.3 `go test ./proxy/tui/...` — all TUI tests pass; rewritten render tests cover plain-text + filter behavior
- [ ] 2.4 `go test -race ./...` — all tests pass with the race detector on
- [ ] 2.5 `TestRenderLogTab_NoStyling` — body has zero ANSI escapes

#### Manual

- [ ] 2.6 Tab 1 shows every stderr line in chronological order, plain text, no padding
- [ ] 2.7 Tab 2 and Tab 3 layout unchanged
- [ ] 2.8 Terminal resize doesn't introduce padding artifacts on Tab 1
- [ ] 2.9 Triggered `slog.Warn` (unmapped model) appears on Tab 1

### Phase 3: Level filter (L key)

#### Automated

- [ ] 3.1 `go build ./...` — compiles cleanly
- [ ] 3.2 `go vet ./...` — no new vet warnings
- [ ] 3.3 `go test ./...` — all tests pass
- [ ] 3.4 `go test -race ./...` — race detector clean
- [ ] 3.5 `gofumpt -l .` — no unformatted files

#### Manual

- [ ] 3.6 Tab label cycles `[all] → [debug] → [info] → [warn] → [error] → [all]` on repeated `L` presses
- [ ] 3.7 Filter `error` hides `slog.Warn` lines from Tab 1, but they remain on stderr
- [ ] 3.8 Pressing `L` on Tab 2 is a no-op
- [ ] 3.9 `?` help modal lists `L`
- [ ] 3.10 Restart resets filter to `[all]` (no persistence)
