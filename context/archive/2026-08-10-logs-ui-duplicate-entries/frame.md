# Frame Brief: Dashboard log view duplicates historical entries

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

On `mage run` (and also running the binary directly), the startup log lines
("freedius listening on …", "web dashboard on …") appear **twice** — both in
the console and in the web dashboard's log view. Per the user: duplication is
**always** present, happens on a clean single start (no leftover process), and
the two copies of each line have **identical timestamps**. The user later
clarified the duplication is visible "in the ui" / "in the logs".

## Initial Framing (preserved)

- **User's stated cause or approach**: It's a logging bug — the startup lines
  are emitted/duplicated somewhere in the logging code and should be
  de-duplicated.
- **User's proposed direction**: Fix the duplicate log emission in the code.
- **Pre-dispatch narrowing**: User confirmed — always doubled, clean single
  start, identical timestamps on both copies; duplication also seen in UI.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Backend LogSink emits/duplicates entries** — ringHandler pushes each
   record twice into the sink, so both console (stderr) and UI double.
   ← investigated, rejected.
2. **Fan-out handler writes to stderr twice** — a second slog handler targets
   stderr. Rejected: only one stderr handler exists (`proxy/logtee.go:200`).
3. **Two `freedius` processes share the terminal's stderr** — a leftover
   instance. Rejected: user confirmed clean single start; identical timestamps
   are impossible across two processes.
4. **Dashboard renders history twice** — the `/logs` page server-renders the
   snapshot AND the SSE stream replays the full ring on connect, stacking
   duplicates. ← leading hypothesis, confirmed.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Backend LogSink duplicates each record | Built + ran binary: `total_logs: 2`; a single `GET /v1/logs?since=0` replay returns exactly 2 `event: log` entries (listening + web dashboard). One emit per line in source (`main.go:157`, `:211`); `ringHandler.Handle` pushes once (`logtee.go:200`). | NONE |
| Second stderr handler (fan-out) | Only one stderr handler constructed in the whole app (`main.go:113`/`newLogger`, `logtee.go:179`); all other `slog.New` calls are in tests or discard buffers. | NONE |
| Two processes sharing stderr | User: clean single start; identical ms timestamps cannot occur across two processes. | NONE |
| Dashboard double-renders history | `/logs` server-renders `.Entries` into `#log` (`proxy/web/templates/logs.html:102`); `handleLogs` replays the *entire* ring on every new connection (`internal/eventstream/handlers.go:153-166`); the `htmx:sseMessage` handler appends each replayed entry (`logs.html:161-182`). Server HTML snapshot + SSE replay = each historical entry shown twice. | STRONG |

## Narrowing Signals

- `total_logs: 2` proves the backend holds a single correct copy — the sink is
  not the source of duplication.
- The two copies have identical timestamps: impossible from two processes, only
  possible from one record being displayed twice by the client (snapshot +
  replay both contain the same `Seq`-keyed history).
- Only *historical* lines (startup) double; live lines don't — consistent with
  "snapshot in HTML + replayed SSE" and inconsistent with a backend emit bug
  (which would double live lines too).

## Cross-System Convention

The SSE replay-then-live pattern is intentional design (`handleLogs`: replay
buffered history, then stream new). The convention's missing half: the client
must not *also* render that same history in the initial HTML, or it must clear
`#log` before the replay lands. The bug is a client/server rendering
interaction gap, not a break in the SSE contract. (Prior change
`2026-08-09-logs-ui-live-tail` restored the live tail but left this overlap
unaddressed.)

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: the `/logs` dashboard displays
> each historical log line twice because the page server-renders the current
> snapshot *and* the SSE `/v1/logs` stream replays the full ring buffer on
> connect, stacking the same entries.

This is a dashboard rendering/integration bug. The backend logging path is
correct (single emit, single sink entry). Fixing it belongs in the web layer:
either stop server-rendering the snapshot when SSE will replay it, or clear
`#log` (or apply a `since=` cursor) before the replay is appended. The initial
framing (a logging-code duplication bug) is **incorrect** — there is nothing to
de-duplicate in the logging code.

## Confidence

- **HIGH** — backend single-copy proven by direct measurement (`total_logs: 2`,
  single replay = 2 distinct entries); frontend double-render path identified
  with file:line evidence; narrowing signals all consistent.

## What Changes for /10x-plan

The plan is about the **dashboard log view**, not logging. It should: (a) avoid
double-counting history — e.g. drop the server-rendered `{{range .Entries}}`
when SSE is active, or clear `#log` on `htmx:sseOpen` before replay, or open
the SSE with `?since=<maxSeq already rendered>`; (b) keep live-tail working. No
change to `proxy/logtee.go`, `internal/eventstream/handlers.go` replay logic,
or `main.go`.

## References

- Source files: `cmd/freedius/main.go:157`, `:211`; `proxy/logtee.go:200`,
  `:179`; `internal/eventstream/handlers.go:136-190`;
  `proxy/web/templates/logs.html:91-103`, `:161-182`; `proxy/web/handlers.go:147`.
- Measurement: built `/tmp/freedius-test`, `GET /v1/logs?since=0` → 2 `event: log`
  entries; `GET /v1/stats` → `total_logs: 2`.
- Prior related change: `context/archive/2026-08-09-logs-ui-live-tail/`.
- Investigation tasks: read-only verification (no sub-agent tasks created).
