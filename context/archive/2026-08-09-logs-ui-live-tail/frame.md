# Frame Brief: Logs UI "rewrite"

> Framing step before /10x-plan. Captures what is *actually* at issue,
> separated from what was initially assumed.

## Reported Observation

The user supplied a 5-point bug report + 5-point fix list for the `/logs`
dashboard page: (1) logs never refresh / no live tail, (2) lines are
truncated raw `logfmt` with buried level, (3) inconsistent "accidental"
row styling (gray pills vs no background), (4) filters exist but no
message search / counter / pagination and unclear if they hit the server,
(5) small fixed-height container with a footer floating mid-page. The
proposed fixes assume the page is essentially a static, non-live,
unparsed dump that needs to be **rebuilt** (add SSE/polling, parse logfmt
into a table, add search/counter, restyle, pin footer).

## Initial Framing (preserved)

- **User's stated cause or approach**: The logs page is static/non-live and
  unparsed; the right move is to add auto-refresh/SSE, a refresh button,
  logfmt parsing into a table with level badges, search/time-range/counter,
  and layout fixes.
- **User's proposed direction**: A from-scratch log-viewer build covering
  all 5 fix items.
- **Pre-dispatch narrowing**: Not provided inline — the report bundles five
  categories as one "the logs page is broken" scope.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Live-update mechanism** — is the tail wired at all, or wired-but-broken?
2. **Line rendering / readability** — truncation, wrapping, level visibility.
3. **Row styling consistency** — CSS class application.
4. **Filter capability** — server vs client filtering, search, counter, live-stream interaction.
5. **Page layout** — container height, footer positioning.

   ← The user's framing implicitly assumes dimension 1 lacks architecture
     (needs SSE built). Evidence shows dimension 1 is *wired-but-broken*.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| **H1**: Live tail is broken by a missing client SSE dependency, not missing architecture | `logs.html:93` uses `sse-connect="/v1/logs"` + `hx-ext="sse"`; `layout.html:65-66` loads only `htmx.min.js` (+ `app.js`) — **no `htmx-sse.min.js`**; `htmx.min.js` contains only the `'sse'` name string (1 match), not `defineExtension('sse',…)`; server SSE is real and tested (`handlers.go:136` `handleLogs` streams `{level,line}` via `LogSink`, `LevelLabel` badges exist). htmx needs the extension script for `hx-ext="sse"` to function. | **STRONG** |
| **H2**: Lines are truncated, unparsed, level buried | `app.css` `pre{overflow-x:auto}` (wrap/scroll exists); `.log-debug/info/warn/error` backgrounds + `color` all defined (`app.css:863-886`); level already a colored class via `log-{level}` (logs.html:134). Raw `logfmt` text *is* the streamed `e.Line` (intentional live-raw-tail design). | **WEAK / PARTIAL** |
| **H3**: Inconsistent "accidental" row styling | All four level classes have deliberate backgrounds/badges; `.log-system` adds a muted left-border (intentional, `app.css:886`). No rendering bug found. | **NONE** |
| **H4a**: Filters don't reach the server | `logs.html` filters use `hx-get="/logs"` → `handleLogs` server-side filters (`handlers.go:336`, `?min/?provider/?mapping/?outcome/?fallback`). Filters **do** hit the server. | **NONE (claim false)** |
| **H4b**: No message search / no counter / live stream bypasses active filter | No `message`/`q` filter param in `handleLogs`; no count/total returned; SSE stream sends **all** lines unfiltered, so a filtered view still receives unfiltered live lines. The live-filter-bypass is already tracked in `context/changes/misleading-inactive-filter/`. | **STRONG (these sub-issues)** |
| **H5**: Layout broken (tiny box, footer mid-page) | `.log-container{max-height:70vh;overflow-y:auto}` (`app.css:849`) — intentional, not tiny; `.footer` is normal flow (not pinned) so it can sit mid-page when content is short. | **WEAK (footer pin is a real, minor gap)** |

## Narrowing Signals

- The single decisive signal: `hx-ext="sse"` is present in markup but the
  htmx SSE **extension script is absent** from `layout.html`. This alone
  explains observation #1 (loads once, never updates) with a 1-line fix.
- `context/changes/misleading-inactive-filter/` already exists — the
  filter-bypass sub-issue (H4b) is a *known, separate* change, not new work.
- Lessons file (SSE encoding rules) shows SSE is a deliberate, tested
  capability — so the architecture is not missing; something is broken.

## Cross-System Convention

Live SSE monitoring is a first-class, tested feature of this project
(`internal/eventstream` + `proxy.LogSink` + `RingHandler`). The convention
is: server streams structured `{level,line}` over SSE; the client appends
with a `log-{level}` class. Adding a *second* polling/refresh mechanism
would duplicate an existing, intentional design. The right response to a
dead tail is to repair the existing wiring, not build a parallel one.

## Reframed (or Confirmed) Problem Statement

> **The actual problem is**: the `/logs` page is already a live SSE viewer,
> but its live tail is **silently dead** because the htmx SSE extension
> script is missing from `layout.html` — a one-line dependency fix, not a
> rebuild. The remaining reported items are either overstated (styling,
> truncation) or are small, already-partially-tracked UX gaps (message
> search, count/total, live-filter bypass, footer pin).

The initial framing ("rebuild a static, unparsed viewer") is **not**
supported: the architecture, live transport, level badges, and
server-side filtering already exist. Rebuilding per the proposed fixes would
discard working infrastructure and risk regressions (see
`context/changes/misleading-inactive-filter/` for the filter work already in
flight).

## Confidence

**HIGH** for the central reframe (H1: missing SSE extension → dead tail;
STRONG evidence, matches project SSE convention). The *scope* of the
secondary UX items is MEDIUM — several claims in the report are inaccurate,
so a plan should re-verify each gap rather than trust the report verbatim.

## What Changes for /10x-plan

The plan should be **not** "build a live log viewer." It should be:
1. Restore the live tail — add the htmx SSE extension include (or replace
   `sse-connect` with a tiny hand-rolled `EventSource` consumer) and verify
   streaming end-to-end.
2. A scoped UX pass covering only the *verified* gaps: optional message
   search, result count/total, live-stream honoring the active filter (link
   to `misleading-inactive-filter`), and footer pinning.
3. Explicitly **exclude** redundant work: logfmt→table parsing (level badges
   already exist), a second polling/refresh mechanism, and restyling that is
   already intentional.

## References

- Source: `proxy/web/templates/logs.html:90-146` (SSE wiring + handler)
- Source: `proxy/web/templates/layout.html:65-66` (missing extension script)
- Source: `proxy/web/static/htmx.min.js` (no `defineExtension('sse')`)
- Source: `internal/eventstream/handlers.go:136-190` (`handleLogs` SSE)
- Source: `proxy/web/handlers.go:336-481` (`/logs` server-side filtering)
- Source: `proxy/web/static/app.css:849-895` (log container + level styles)
- Related change: `context/changes/misleading-inactive-filter/`
- Investigation tasks: read-only exploration (no TaskCreate sub-agents;
  single-thread verification sufficient given conclusive evidence).
