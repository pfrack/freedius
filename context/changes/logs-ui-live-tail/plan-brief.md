# Logs UI Live-Tail Restoration — Plan Brief

> Full plan: `context/changes/logs-ui-live-tail/plan.md`
> Frame brief: `context/changes/logs-ui-live-tail/frame.md`

## What & Why

The `/logs` page is already a live SSE viewer, but its tail is silently dead
because the htmx SSE **extension** script is never loaded — `layout.html`
includes only `htmx.min.js` core, so `hx-ext="sse"` on the logs container is
a no-op. The report's "rebuild the logs page" framing is wrong: level badges,
server-side filtering, and scrolling already exist. We restore the tail with
a one-dependency fix plus minimal tail UX, and deliberately avoid a rebuild.

## Starting Point

`logs.html` already wires `sse-connect="/v1/logs"` and a `htmx:sseMessage`
handler (`logs.html:90-146`); the server streams `{level,line}` via a tested
`LogSink` (`internal/eventstream/handlers.go:136`). Static assets are embedded
(`proxy/web/embed.go:5`), and the vendored `htmx.min.js` header even names
the intended companion `htmx-ext-sse@2.2.2`. The only missing piece is that
extension file + its `<script>` tag.

## Desired End State

Loading `/logs` shows proxy log lines appear in real time, with a small
"live" dot indicating connection state and autoscroll that pauses when the
operator scrolls up to read. The fix is locked by an e2e test asserting lines
stream on load.

## Key Decisions Made

| Decision              | Choice                          | Why (1 sentence)                                                              | Source |
| --------------------- | ------------------------------- | ----------------------------------------------------------------------------- | ------ |
| Restore mechanism     | Vendor `htmx-ext-sse@2.2.2` locally | Reuses existing `hx-ext`/`sse-connect` markup + JS with zero rewrite; matches no-CDN vendoring convention | Plan (from frame) |
| Scope                 | Tail-only                       | Smallest blast radius; directly fixes root cause; honors "don't rebuild"     | User  |
| Live-tail UX          | Pause-on-scroll + connection dot | Expected tail behavior, minimal code, no new buttons                        | User  |
| Out of scope          | Search, count, footer pin, filter-bypass | Either already exist or owned by `misleading-inactive-filter`            | Frame |

## Scope

**In scope:** vendor SSE extension + `<script>` tag; pause-on-scroll; connection dot; e2e test.

**Out of scope:** logfmt→table parsing, polling/refresh button, message search, result count, footer pin, live-filter-bypass (separate change), level-row restyling.

## Architecture / Approach

Purely additive: drop `htmx-sse.min.js` into the embedded `static/` dir (auto-embedded), register it in `layout.html` after `htmx.min.js`, then add two small behaviors to the existing `logs.html` script block. No Go handler or data-model changes.

```
layout.html:  … htmx.min.js → htmx-sse.min.js (NEW) → app.js
logs.html:    sse-connect (existing) → now opens EventSource → htmx:sseMessage → appendLogLine
               + tailPaused (scroll) + log-live-dot (htmx:sseOpen/Error/Close)
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
| ----- | ---------------- | -------- |
| 1. Vendor & wire SSE extension | Live tail works again | Wrong extension version / script order breaks `hx-ext` |
| 2. Tail UX | Pause-on-scroll + live dot | Accidental regression in existing `appendLogLine` trim logic |
| 3. Verification | e2e test + full gates | e2e harness setup friction |

**Prerequisites:** ability to fetch `htmx-ext-sse@2.2.2/sse.min.js` once (build itself stays offline).
**Estimated effort:** ~1 short session across 3 phases.

## Open Risks & Assumptions

- With `AuthToken` set, `EventSource` can't send the auth header, so the tail stays dead — this plan targets the default (no-auth) config; the auth-gated case is a known limitation, not fixed here.
- Assumes `htmx-ext-sse@2.2.2` is API-compatible with htmx core 2.0.4 (the pin in `htmx.min.js` already asserts this pairing).

## Success Criteria (Summary)

- `/logs` streams new lines live without a page reload (proven by e2e).
- Connection dot reflects live/down; autoscroll pauses on scroll-up.
- `mage lint`, `mage test`, `mage govulncheck` all green; no sibling-page regressions.
