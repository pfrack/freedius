# Dashboard log view duplicate entries — Plan Brief

> Full plan: `context/changes/logs-ui-duplicate-entries/plan.md`
> Frame brief: `context/changes/logs-ui-duplicate-entries/frame.md`

## What & Why

The `/logs` dashboard shows every historical log line twice. The cause is not a
logging bug: the page server-renders the ring snapshot *and* the SSE stream
replays the full ring on connect, stacking identical entries. We remove the
server-rendered snapshot so the SSE replay is the sole source of history.

## Starting Point

The web dashboard already has a working live tail. `logs.html` both bakes
`{{range .Entries}}` into the page and opens `sse-connect="/v1/logs"`, whose
handler (`internal/eventstream/handlers.go:154-166`) replays the whole ring on
connect. The frame proved the backend holds a single correct copy
(`total_logs: 2`); only the client double-renders.

## Desired End State

On a clean start, `/logs` shows each historical line exactly once, begins with a
"Waiting for log events…" placeholder that the SSE replay replaces, and keeps
appending new live lines without duplication.

## Key Decisions Made

| Decision              | Choice                                  | Why (1 sentence)                                                       | Source |
| --------------------- | --------------------------------------- | ---------------------------------------------------------------------- | ------ |
| Fix mechanism         | Drop server-rendered snapshot; SSE owns `#log` | Definitively removes duplication, one template edit, no backend change | Plan   |
| Filter scope          | Fix duplication only; view becomes uniformly unfiltered | SSE live stream is already unfiltered, so matching it keeps scope tight | Plan   |
| Empty-state           | Keep "Waiting for log events…" placeholder | Avoids an empty flash before SSE connects                              | Plan   |
| Verification          | Manual browser check + existing `log_filter_test.go` | Regression is a client-render interaction, not a Go unit case          | Plan   |

## Scope

**In scope:** Remove `{{range .Entries}}` from `logs.html`; keep a placeholder;
clear it on first SSE `log` event (tiny JS addition).

**Out of scope:** `proxy/logtee.go`, `internal/eventstream/handlers.go` replay
logic, `cmd/freedius/main.go`, `log-entries.html`/`renderLogEntries` (filter
fragment), making the SSE stream honor filters, new automated tests.

## Architecture / Approach

Single-file web change. The `#log` container keeps its `sse-connect` attributes;
its initial body becomes just the placeholder. The existing `htmx:sseMessage`
listener removes the placeholder on the first real `log` event, then appends as
today. No data flows or handlers change.

## Phases at a Glance

| Phase | What it delivers                          | Key risk                                |
| ----- | ----------------------------------------- | --------------------------------------- |
| 1     | Template+JS edit removing the double-render | SSE-down edge case no longer shows history (pre-existing live-tail dependency) |

**Prerequisites:** None — change builds on the existing live-tail wiring.
**Estimated effort:** ~1 short session (one template edit + 2-line JS).

## Open Risks & Assumptions

- With the snapshot removed, historical logs now depend on the SSE connection
  succeeding. In the default (empty `AuthToken`) setup SSE works; if `AuthToken`
  is set, the browser SSE connect lacks the token and history *and* live tail
  would both be empty. This is a pre-existing live-tail limitation, not
  introduced here, and out of scope.
- The filter dropdown still only affects the initial/fragment render; the live
  stream remains unfiltered. Intentional per the agreed scope.

## Success Criteria (Summary)

- Each historical log line appears exactly once in the `/logs` UI.
- Live tail continues appending new lines without duplicates.
- `mage build`, `mage test`, and `mage lint` all pass.
