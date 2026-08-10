# Dashboard log view shows each historical entry twice — Implementation Plan

## Overview

The `/logs` dashboard renders every historical log line twice: once from the
server-rendered ring snapshot baked into the page HTML, and again when the
`#log` div's SSE connection replays the full ring on connect. This plan removes
the server-rendered snapshot from the initial page so the SSE replay is the
*sole* source of history in `#log`, eliminating the duplication. Scope is
strictly the web layer — no backend, logging, or SSE-handler changes.

## Current State Analysis

- `logs.html:91-103` — the `#log` div server-renders the ring via
  `{{range .Entries}}<pre …>{{.Line}}</pre>{{end}}` **and** opens an SSE
  connection (`sse-connect="/v1/logs"`).
- `handlers.go:154-166` (`internal/eventstream`) — `handleLogs` replays the
  *entire* ring on every new SSE connection (unfiltered; reads only `?since=`).
- `logs.html:161-182` — the `htmx:sseMessage` listener appends each replayed
  `log` event into `#log`. Snapshot + replay = each historical entry twice.
- Live (post-connect) lines do **not** double — consistent with a client
  double-render, not a backend emit bug (proven in the frame: `total_logs: 2`,
  single replay returns 2 distinct entries).
- The `{{if not .Entries}}` wrapper around the placeholder is effectively dead:
  `handleLogs` always passes a non-nil `filtered` slice, so the "Waiting for
  log events…" text never currently shows.

### Key Discoveries:

- Backend is correct (single emit, single sink entry) — the bug is purely a
  client/server rendering overlap (`logs.html:102` + `handlers.go:154-166`).
- The SSE `/v1/logs` handler ignores `min`/`provider`/`mapping`/`outcome`/
  `fallback` filters — the live stream is already unfiltered. The filter
  dropdown only ever affected the initial snapshot.
- `log-entries.html` (the HTMX filter fragment returned by `renderLogEntries`,
  `web/handlers.go:844`) ranges over `.Entries` and must stay untouched — it is
  how filter changes re-render `#log`.

## Desired End State

On a clean `mage run`, the `/logs` page shows each historical log line exactly
once. The `#log` container starts with a "Waiting for log events…" placeholder,
which the SSE replay replaces with the full history (once), and live tail
continues appending new lines without duplicates. `mage test` and `mage lint`
stay green.

## What We're NOT Doing

- No changes to `proxy/logtee.go`, `internal/eventstream/handlers.go` replay
  logic, or `cmd/freedius/main.go`.
- No change to `log-entries.html` or `renderLogEntries` — the filter HTMX
  re-render keeps working as before.
- Not making the SSE live stream honor filters (out of scope per decision:
  fix duplication only; the view becomes uniformly unfiltered, matching the
  already-unfiltered live stream).
- Not adding new automated tests; verification is manual + existing
  `log_filter_test.go`.

## Implementation Approach

Adopt the "drop server snapshot" fix: remove the `{{range .Entries}}` from the
initial page render so the SSE replay is the only thing that populates `#log`.
Keep a static placeholder as `#log`'s initial content and clear it in the
existing `htmx:sseMessage` handler the first time a real `log` event arrives.
This is a template-only change plus a ~2-line JS addition; no Go code changes.

## Phase 1: Remove server-rendered snapshot from logs.html

### Overview

Stop the initial page from emitting log `<pre>` elements; let SSE own `#log`.
Keep a placeholder and remove it on first live log so there is no empty flash.

### Changes Required:

#### 1. Drop the `{{range .Entries}}` from the `#log` container

**File**: `proxy/web/templates/logs.html`

**Intent**: The initial page must not render historical entries into `#log`;
the SSE replay already does. Keep the placeholder as the div's default content
so the brief pre-connect moment shows "Waiting for log events…".

**Contract**: Replace the current `#log` body (currently lines 99-102):

```html
    {{if not .Entries}}
    <small class="text-muted">Waiting for log events…</small>
    {{end}}
    {{range .Entries}}<pre class="log-{{.Level}}">{{.Line}}</pre>{{end}}
```

with a single static placeholder carrying a stable id so JS can remove it:

```html
    <small class="text-muted" id="log-empty">Waiting for log events…</small>
```

Do **not** remove the `sse-connect`/`sse-swap`/`hx-swap="none"` attributes or
the `hx-ext="sse"` on the div — SSE is now the only population path.

#### 2. Clear the placeholder when the first real log arrives

**File**: `proxy/web/templates/logs.html`

**Intent**: The static `#log-empty` must disappear as soon as SSE history lands,
so it doesn't sit above the log lines forever. Tie removal to actual log data
arriving (not `sseOpen`) to avoid any race with replay ordering.

**Contract**: In the `htmx:sseMessage` listener (currently `logs.html:161-182`),
inside the branch where `sseEventName === 'log'` and before `appendLogLine(pre)`,
remove the placeholder. The `replay`/`event` non-log messages already `return`
early, so only real `log` events reach this point:

```js
   if (sseEventName !== 'log') return;
   var ph = document.getElementById('log-empty');
   if (ph) ph.remove();
   var pre = document.createElement('pre');
```

The `Entries` field on `logsData` (set in `web/handlers.go:471-479`) is left
untouched — it is still required by the `log-entries.html` filter fragment.
No change to `handleLogs`, `renderLogEntries`, or `log-entries.html`.

> **Implementation addendum**: `proxy/web/log_filter_test.go`'s
> `TestHandleLogs_OutcomeFilter` and `TestHandleLogs_FallbackFilter` were updated
> to send `HX-Request: true` so they exercise the `log-entries` fragment (the
> full page no longer server-renders entries). This was an approved scope
> adaptation surfaced during implementation, preserving filter-logic coverage.

### Success Criteria:

#### Automated Verification:

- `mage build` succeeds (embedded template compiles with the edited `logs.html`).
- `mage test` passes — `proxy/web/log_filter_test.go` (page title "Logs" still
  present, status 200) and all other suites stay green.
- `mage lint` passes (gofmt/golangci-lint; no Go code changed, but confirm
  embed/asset pipeline is clean).

#### Manual Verification:

- On `mage run`, open the dashboard `/logs`: each historical startup line
  ("freedius listening on …", "web dashboard on …") appears **exactly once**.
- The live-tail dot reaches `live` state and new log lines append without
  duplication.
- Applying a filter (e.g. Level=Error) still re-renders `#log` via the fragment
  and live lines keep appending (unfiltered, as accepted).

**Implementation Note**: After automated verification passes, pause for manual
browser confirmation before closing the change.

---

## Testing Strategy

### Unit Tests:

- No new unit tests (decision: manual + existing). `log_filter_test.go` already
  exercises `handleLogs` page render and must remain green.

### Integration Tests:

- None added; the regression is a client-render interaction not covered by the
  current Go harness.

### Manual Testing Steps:

1. `mage run`, open `http://127.0.0.1:<port>/logs`.
2. Confirm each historical line shows once (no doubling) in both the page and
   the browser console/network SSE frames.
3. Trigger live traffic (e.g. a proxied request) and confirm new lines append
   once and the live dot is `live`.
4. Change Level/Provider filters and confirm `#log` re-renders and live tail
   continues.

## Performance Considerations

None. The ring buffer, SSE replay, and DOM cap (`MAX_LOG_LINES = 500` in
`logs.html:119`) are unchanged; removing the server snapshot only deletes
redundant HTML, it does not add work.

## Migration Notes

None. The change is purely presentational; no data model, config, or persisted
state is affected.

## References

- Frame brief: `context/changes/logs-ui-duplicate-entries/frame.md`
- Bug site: `proxy/web/templates/logs.html:91-103`, `:161-182`
- SSE replay: `internal/eventstream/handlers.go:154-166`
- Untouched fragment: `proxy/web/templates/log-entries.html`,
  `proxy/web/handlers.go:844` (`renderLogEntries`), `:398` (`handleLogs`)

## Progress

### Phase 1: Remove server-rendered snapshot from logs.html

#### Automated

- [x] 1.1 `mage build` succeeds with edited `logs.html` — 75d925b
- [x] 1.2 `mage test` passes (incl. `log_filter_test.go`) — 75d925b
- [x] 1.3 `mage lint` passes — 75d925b

#### Manual

- [x] 1.4 `/logs` shows each historical line exactly once; live tail duplicates none — 75d925b
