# TUI Log Tab: All slog Lines + Level-Cycle Filter — Plan Brief

> Full plan: `context/changes/tui-all-logs-level-filter/plan.md`
> Research: `context/changes/tui-all-logs-level-filter/research.md`

## What & Why

The TUI's first tab currently shows only `AccessLogMiddleware` request events, styled with lipgloss. We want it to show every `slog` line emitted by the process — startup banner, dispatcher debug, error middleware, envinject warnings, event-bus overflow — in chronological order, rendered as plain text. Add a single `L` key to cycle the level filter `All → Debug → Info → Warn → Error → All` with the active level shown in the tab label. The change gives operators a unified, filterable in-process log without touching stderr (which remains the source of truth).

## Starting Point

- `newLogger` at `main.go:52-62` is the single construction site for the process logger; its output goes only to stderr (`main.go:124`, `slog.SetDefault` at line 128).
- `renderLogTab` at `proxy/tui/views.go:34-89` builds lines from `[]proxy.RequestEvent` only, applying one of three lipgloss status styles. The whole body is wrapped in `windowStyle` (padding) at `model.go:508`.
- The `EventBus` at `proxy/eventbus.go:31-69` is the established pattern for non-blocking, drop-on-overflow pub/sub; the new log sink will mirror it.
- 20 production `slog` call sites flow through the single logger (research §2). Once the tee is in place, every one of them becomes visible on the TUI.

## Desired End State

After this plan:

- Every `slog` line appears on the TUI's Log tab in chronological order, plain text, no color or padding. The other tabs keep their chrome.
- Pressing `L` in tab mode cycles the level filter; the active level is shown in the tab label `[1] Log [<level>]`. Filtered-out levels are hidden from the Log tab; stderr is unaffected.
- `EventBus` continues to feed `totalRequests`/`errorCount`. The `eventLog` ring buffer is removed — the slog buffer is the single source of truth for display.
- The `?` help modal lists `L`. The README mentions it.

## Key Decisions Made

| Decision                       | Choice            | Why (1 sentence)  | Source           |
| ------------------------------ | ----------------- | ----------------- | ---------------- |
| Where to tee into the logger   | Inside `newLogger` | Ensures every `With("component", ...)` propagates correctly to both stderr and the ring. | Research |
| Stderr filter on TUI cycle?    | No — render-time filter only | Stderr is the operator's source of truth; TUI filter is a view concern. | Research |
| `slog.MultiHandler` vs custom  | Custom `ringHandler` wrapping stderr handler | Only two children, one less indirection, exact `EventBus` mirror. | Research |
| JSON line shape on the TUI     | Always text-shaped via a second `TextHandler` over a pooled buffer | TUI is readable in both `--log-format=text` and `--log-format=json`; byte-identical to stderr text rendering; no parse-fallback needed. | Plan |
| Active level indicator         | Tab label only — `[1] Log [info]` | Single source of truth; no competition with `verbose-errors` / shell-install messages. | Plan |
| Strip lipgloss padding from Log tab | `styleBody bool` field on Dashboard, conditional `windowStyle` wrap in `View()` | Surgical — only Log tab loses chrome. The other tabs keep their padding. | Plan |
| Stats counting source          | `EventBus` for stats, slog buffer for display | Eliminates merge step; slog buffer already contains access log lines; stats stay accurate. | Plan |
| `LogEntry` shape               | `Time, Level, Line` — `Line` always pre-rendered text | Filtering needs `Level`; display needs `Line`; no JSON parsing at the TUI. | Plan |
| `L` key collision check        | Free in tab mode; form-mode falls through to textinput | Mirrors how every other unbound letter works; no special handling needed. | Research |
| Scroll reset on level change   | `d.logScroll = 0` (same as `case requestEventMsg`) | "Show the latest matching entries" is the user's intent after a filter change. | Plan |

## Scope

**In scope:**
- New `proxy/logtee.go` with `ringHandler`, `LogSink`, `LogEntry`, `Snapshot()`, `EventCount()`.
- `main.go`: `newLogger` gains a `*LogSink` parameter; `logSink.Subscribe()` is wired into `NewDashboard`.
- `proxy/tui/model.go`: `Dashboard` gains `logs`, `logBuffer`, `styleBody`, `currentLogLevel`; loses `eventLog`. `ringBuffer` becomes generic `[T any]`. New `waitForLog` command and `logEntryMsg` case.
- `proxy/tui/views.go`: `renderLogTab` rewritten to plain text + level filter; `renderTabs` shows the level indicator.
- `proxy/tui/loglevel.go` (new): `LogFilter` type, 5 cycle states, `Matches` method.
- `proxy/tui/help.go`: append `{"L", "Cycle log level filter"}`.
- `README.md`: mention `L` in the shortcut sentence (line 84).
- Tests in `proxy/logtee_test.go` and appended to `proxy/tui/model_test.go`.

**Out of scope:**
- Changing the slog level on stderr (TUI filter is a *display* concern).
- New log format flag or env var.
- Persistence of the level filter across restarts.
- Auto-scroll on every slog line (too jumpy; only on level change and on `requestEventMsg`).
- Changes to `EventBus`, `AccessLogMiddleware`, or the access-log format contract.

## Architecture / Approach

```
slog.Logger.With("component", ...)
         │
         ▼
   ringHandler { inner: TextHandler/JSONHandler,   ← stderr (unchanged)
                 formatH: TextHandler(*bytes.Buffer pool) }  ← ring buffer
         │
         ├─→ os.Stderr (preserved)
         │
         └─→ LogSink.ch (cap 1000, non-blocking)
                  │
                  ▼ waitForLog → logEntryMsg → d.logBuffer.push
                                                  │
                                                  ▼
                                            renderLogTab(entries, filter)
                                                  │
                                                  ▼
                                              plain text
```

- **Single tee at the logger boundary.** Not at the call sites (would miss the `With` chain) and not at the `slog.Logger` post-construction (would miss `WithAttrs`/`WithGroup` propagation). Inside `newLogger` is the one place every downstream `logger.With(...)` call threads through both children.
- **`slog.Record.Clone()`** is mandatory: the inner stderr handler consumes the original; the format child handler must work on a clone. `Record` is not safe to retain past `Handle` return.
- **Buffer pool** keyed to the format handler keeps the pre-render allocation-free across records.
- **Renderer-level filter** (not handler-level `Enabled`) so stderr is never affected.
- **No merge step.** The slog buffer is a strict superset of the access log format (because `AccessLogMiddleware` uses `slog.Info`). Display reads from the slog buffer only. Stats still read from the EventBus.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Tee handler + ring sink | `proxy/logtee.go` with `ringHandler` + `LogSink`; `main.go` constructs the sink; 5 new tests; stderr behavior unchanged | Goroutine safety of `formatH.Handle` and the buffer pool under concurrent emit |
| 2. TUI consumer + plain-text rendering | `Dashboard` consumes slog entries; `renderLogTab` is plain text; `windowStyle` stripped from Log tab only; tab label shows level | Removing `eventLog` breaks the old `TestRenderLogTab_*` tests; rewritten tests must cover plain-text + filter |
| 3. Level filter (L key) | `L` cycles `All → Debug → Info → Warn → Error → All`; filter applied at render time; help modal + README updated | User expectation that the filter is "live" (no Apply button) — `d.logScroll = 0` on each press is the only state mutation beyond the level itself |

**Prerequisites:** None — this is a self-contained feature on top of the existing TUI.
**Estimated effort:** ~3 small PRs across 1-2 sessions (solo dev, all files are <500 lines, no migration).

## Open Risks & Assumptions

- **Assumption**: `slog.NewMultiHandler` is not used (Go 1.26.4 ships it; we choose `ringHandler` for fewer indirections). If a future need for *N* heterogeneous handlers arises, `MultiHandler` is the upgrade path. The tee's contract is small enough that swapping in `MultiHandler` is local.
- **Assumption**: `slog.Record.Clone()` performs an acceptable amount of work. With 20 production call sites and modest request rates, the per-record allocation is negligible. If profiling shows otherwise, the alternative is a `bytes.Buffer` that buffers the format output, owned by the format handler (single-threaded access by virtue of `slog.NewTextHandler`'s internal lock).
- **Risk**: A future code change adds a new `slog` call site that emits a level we did not consider. The 5-state filter cycle is exhaustive (`All, Debug, Info, Warn, Error`); any new level (e.g. `slog.Level(-2)`) would be treated as `Debug` or `Info` based on numeric comparison. Documented in `LogFilter.Matches`.
- **Risk**: The `eventLog` removal is a small breaking change to the `TestRenderLogTab_Format` test contract — the rewritten tests assert against `LogEntry.Line` (a free-form string) rather than the structured `RequestEvent` field assertions. The new contract is documented in the Phase 2 `Changes Required` section.

## Success Criteria (Summary)

- The TUI's Log tab shows every `slog` line emitted by the process, plain text, no padding; the other tabs are visually unchanged.
- Pressing `L` cycles the level filter; the active level is visible in the tab label; stderr is never affected.
- The `?` help modal and README document the `L` shortcut.
- `go test -race ./...` and `gofumpt -l .` are clean.
