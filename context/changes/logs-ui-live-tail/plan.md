# Logs UI Live-Tail Restoration — Implementation Plan

## Overview

The `/logs` dashboard is already a live SSE viewer, but its live tail is
silently dead: `logs.html` uses `hx-ext="sse"` + `sse-connect="/v1/logs"`,
yet `layout.html` never loads the htmx SSE *extension* script (only
`htmx.min.js` core is included). Without the extension, `sse-connect` does
nothing, so the page renders its initial server snapshot and never updates.
This plan restores the tail with a one-dependency fix (vendor the matching
SSE extension locally), adds minimal tail UX (pause-on-scroll + a live
connection dot), and verifies streaming end-to-end. It deliberately does
**not** rebuild the viewer — level badges, server-side filtering, and
scrolling already exist.

## Current State Analysis

- `proxy/web/templates/logs.html:90-146` already wires the SSE tail
  (`sse-connect="/v1/logs"`, `hx-ext="sse"`, and a `htmx:sseMessage`
  handler that appends `<pre class="log-{level}">` lines and auto-scrolls).
- `proxy/web/templates/layout.html:65-66` loads only `htmx.min.js` and
  `app.js`. The htmx SSE extension is absent, so `hx-ext="sse"` is a no-op.
- `proxy/web/static/htmx.min.js` header documents the intended companion:
  `htmx-ext-sse@2.2.2 — SSE extension (https://unpkg.com/htmx-ext-sse@2.2.2/sse.js)`.
  The vendored htmx core is **2.0.4**.
- Static assets are embedded via `//go:embed templates static`
  (`proxy/web/embed.go:5`) and served from `proxy/web/static/`. Dropping a
  file there requires no build-step change.
- A `.sha256` file (`htmx.min.js.sha256`) exists for drift detection; the
  new extension file should follow the same convention.
- The live-stream-bypasses-active-filter gap is **already tracked**
  separately in `context/changes/misleading-inactive-filter/` — out of scope here.

## Desired End State

After this plan, loading `/logs` shows log lines appear in real time as the
proxy emits them (via SSE replay + live subscription). A small "live" dot
reflects connection state, and auto-scroll pauses when the operator scrolls
up to read. The fix is verifiable by an e2e test asserting lines stream on
load.

## Key Discoveries:

- The root cause is a **missing client dependency**, not missing architecture
  (server SSE is real and tested: `internal/eventstream/handlers.go:136`).
- The htmx version pairing (core 2.0.4 ↔ ext-sse 2.2.2) is already recorded
  in the vendored core file's header — vendor that exact extension version.
- `app.js`/`logs.html` already consume the `htmx:sseMessage` event with
  `e.detail.eventName === 'log'` and `e.detail.data` (JSON `{level,line}`),
  so no JS rewrite of the append path is needed.

## What We're NOT Doing

- No logfmt→table parsing — level badges + raw-line tail is the intended design.
- No second polling/refresh mechanism — it would duplicate the SSE design.
- No message search, result count/total, or footer-pin changes (separate, lower-priority work).
- No live-filter-bypass fix — owned by `misleading-inactive-filter`.
- No restyling of level rows — `.log-*` backgrounds are intentional (`app.css:863-895`).

## Implementation Approach

Vendor the htmx SSE extension that the codebase already expects (matching the
version pinned in `htmx.min.js`), register it in `layout.html` *after* the
core script, then add two small, self-contained tail-behavior enhancements
(pause-on-scroll, connection dot) in the existing `logs.html` script block.
Close with an e2e test proving the tail streams and the standard lint/test
gates.

## Critical Implementation Details

- **Script load order is load-bearing**: the SSE extension must be included
  *after* `htmx.min.js` and *before* `app.js` in `layout.html`, or
  `hx-ext="sse"` stays unregistered. Insert between the two existing tags.
- **AuthToken caveat**: `EventSource` cannot send an `Authorization` header,
  and `eventstream.Handlers.requireAuth` only reads the header (not a query
  token). With `AuthToken` set, the live tail will 401 and stay dead. This
  plan targets the default (no `AuthToken`) config; the auth-gated case is a
  known limitation, not fixed here.

## Phase 1: Vendor & wire the htmx SSE extension

### Overview

Restore the live tail by providing the SSE extension the markup already
depends on, and registering it in the page.

### Changes Required:

#### 1. Vendor the SSE extension file

**File**: `proxy/web/static/htmx-sse.min.js`

**Intent**: Add the htmx SSE extension the page already references via
`hx-ext="sse"`, so `sse-connect="/v1/logs"` actually opens the stream.

**Contract**: Vendor `htmx-ext-sse@2.2.2` (matching the pin in
`proxy/web/static/htmx.min.js`'s header, compatible with htmx core 2.0.4).
Source from `https://unpkg.com/htmx-ext-sse@2.2.2/sse.min.js`. Must define
`htmx.defineExtension('sse', …)` so `hx-ext="sse"` resolves. Placed in
`proxy/web/static/` it is auto-embedded by the existing `//go:embed`.

#### 2. Add drift-detection checksum

**File**: `proxy/web/static/htmx-sse.min.js.sha256`

**Intent**: Match the existing vendoring convention so supply-chain drift is
detectable.

**Contract**: A single line `<sha256hex>  htmx-sse.min.js` (same format as
`htmx.min.js.sha256`), produced from the vendored file.

#### 3. Register the extension in the layout

**File**: `proxy/web/templates/layout.html`

**Intent**: Load the SSE extension after the htmx core so `hx-ext="sse"` is
registered before `app.js` runs.

**Contract**: Insert `<script src="/static/htmx-sse.min.js"></script>`
between the existing `htmx.min.js` tag (line 65) and the `app.js` tag
(line 66). No other markup change.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes (vet/staticcheck/golangci-lint clean on the embed change).
- `mage build` produces a binary; `mage test` passes (no Go behavior change, but the embed must compile with the new asset).
- `sha256sum -c proxy/web/static/htmx-sse.min.js.sha256` succeeds.

#### Manual Verification:

- `mage run`, open `/logs`; within ~1s new proxy activity appears at the bottom without a page reload.
- Browser devtools Network tab shows an open `v1/logs` EventStream connection.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the tail streams before adding UX polish.

---

## Phase 2: Tail UX — pause-on-scroll + connection dot

### Overview

Add the minimal live-tail behaviors the report implied (knowing it's live,
control autoscroll) without building the heavier pause/resume button set.

### Changes Required:

#### 1. Pause autoscroll when reading

**File**: `proxy/web/templates/logs.html` (the `<script>` block in `{{define "scripts"}}`)

**Intent**: Stop yanking the view to the bottom when the operator scrolls up
to read; resume following new lines once they return to the bottom.

**Contract**: Introduce a `tailPaused` flag toggled by a scroll listener on
`#log` (paused when `scrollTop + clientHeight < scrollHeight - threshold`).
`appendLogLine` only performs the `scrollTop = scrollHeight` step when not
paused. Preserve existing DOM-trimming (`MAX_LOG_LINES`).

#### 2. Connection-state dot

**File**: `proxy/web/templates/logs.html` (markup + script block)

**Intent**: Give a visible signal that the tail is connected/live.

**Contract**: Add a small status element near the filters (e.g.
`<span class="log-live-dot" data-state="connecting">`). Listen on `document`
for `htmx:sseOpen` → `data-state="live"`, and `htmx:sseError` /
`htmx:sseClose` → `data-state="down"`. Add matching CSS for the three states
(small colored dot) in `app.css` near the `.log-*` rules.

### Success Criteria:

#### Automated Verification:

- `mage lint` and `mage test` pass.
- `mage build` succeeds with the updated embedded templates.

#### Manual Verification:

- Scroll up in the log view; new lines no longer force the view to the bottom.
- Scroll back to the bottom; following resumes automatically.
- The dot shows "live" shortly after load and "down" if the SSE connection drops.

---

## Phase 3: Verification — e2e + gates

### Overview

Prove the tail streams with an automated e2e test and run the full gate set.

### Changes Required:

#### 1. Add logs live-tail e2e test

**File**: `e2e/tests/logs-tail.spec.ts` (new; follow the pattern in `e2e/tests/activity-feed.spec.ts`)

**Intent**: Lock in the fix so a future regression (e.g. dropping the script
tag) fails CI.

**Contract**: Launch the app, navigate to `/logs`, wait for the SSE replay to
deliver buffered lines, and assert `#log` contains at least one
`pre.log-*` element with non-empty text within a timeout. Reuse the existing
e2e harness bootstrap from sibling specs.

#### 2. Run full gates

**File**: repo root (CI-equivalent commands)

**Intent**: Confirm no regressions from the embed + template changes.

**Contract**: Run `mage lint`, `mage test`, `mage govulncheck` (per AGENTS.md),
and the new e2e spec.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes.
- `mage test` passes (Go unit/race suite).
- `mage govulncheck` reports no new vulnerabilities.
- `npx playwright test logs-tail` (or the repo's e2e runner) passes.

#### Manual Verification:

- Final visual check of `/logs`: live lines stream, dot reflects state, autoscroll pause works.
- No regressions in the dashboard, mappings, or providers pages.

## Testing Strategy

### Unit Tests:

- No new Go unit tests required (the change is asset + template); rely on existing `eventstream` and `web` handler suites for regression safety.

### Integration Tests:

- e2e: `logs-tail.spec.ts` asserts SSE replay delivers lines to `#log` on load.

### Manual Testing Steps:

1. `mage run`, open `/logs`, trigger proxy traffic, confirm lines appear live.
2. Scroll up → autoscroll pauses; scroll to bottom → resumes.
3. Observe the connection dot transitions live → down on disconnect.
4. Sanity-check sibling pages (dashboard, mappings, providers) for no breakage.

## Performance Considerations

Negligible. The SSE stream and 500-line DOM cap already bound browser cost;
the dot/scroll changes are O(1) per event.

## Migration Notes

None. Purely additive asset + template change; no config or data migration.

## References

- Frame brief: `context/changes/logs-ui-live-tail/frame.md`
- Root cause: `proxy/web/templates/layout.html:65-66` (missing extension)
- Wiring: `proxy/web/templates/logs.html:90-146` (sse-connect + handler)
- Server SSE: `internal/eventstream/handlers.go:136` (`handleLogs`)
- Embed: `proxy/web/embed.go:5` (`//go:embed templates static`)
- Vendoring convention: `proxy/web/static/htmx.min.js.sha256`
- Out-of-scope filter gap: `context/changes/misleading-inactive-filter/`

## Progress

### Phase 1: Vendor & wire the htmx SSE extension

#### Automated

- [x] 1.1 Vendor `htmx-ext-sse@2.2.2/sse.min.js` into `proxy/web/static/htmx-sse.min.js` — 1a33be8
- [x] 1.2 Add `proxy/web/static/htmx-sse.min.js.sha256` checksum — 1a33be8
- [x] 1.3 Insert `<script src="/static/htmx-sse.min.js">` in `layout.html` between htmx core and app.js — 1a33be8
- [x] 1.4 `mage lint` passes — 1a33be8
- [x] 1.5 `mage build` + `mage test` pass — 1a33be8
- [x] 1.6 `sha256sum -c htmx-sse.min.js.sha256` succeeds — 1a33be8

#### Manual

- [ ] 1.7 `/logs` shows live lines without reload; Network shows open `v1/logs` stream

### Phase 2: Tail UX — pause-on-scroll + connection dot

#### Automated

- [x] 2.1 `mage lint` + `mage test` pass — e1e9ca3
- [x] 2.2 `mage build` succeeds with updated templates — e1e9ca3

#### Manual

- [ ] 2.3 Scrolling up pauses autoscroll; returning to bottom resumes
- [ ] 2.4 Connection dot shows live/down states correctly

### Phase 3: Verification — e2e + gates

#### Automated

- [x] 3.1 Add `e2e/tests/logs-tail.spec.ts` asserting streamed lines in `#log`
- [x] 3.2 `mage lint` passes
- [x] 3.3 `mage test` passes
- [x] 3.4 `mage govulncheck` clean
- [x] 3.5 `logs-tail` e2e spec passes

#### Manual

- [ ] 3.6 Final `/logs` visual + sibling-page regression check
