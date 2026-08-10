# Provider Badge Drawer (+ Logs Filter Fix) Implementation Plan

## Overview

Two distinct defects, planned together per the frame brief's "Reframed Problem
Statement":
- **(A)** The dashboard has no provider detail interaction — its only provider
  affordance is a full-page `<a href="/logs?provider=…">` navigation, whereas
  mapping rows open an in-place drawer. We build a provider detail drawer
  (new `GET /v1/providers/{name}/detail` endpoint + fragment) and rewire the
  dashboard badge to open it, reusing the existing generic drawer plumbing
  (`openDrawer`/`closeDrawer`, `.drawer__*` shell, overlay, focus trap).
- **(B)** The logs page filters (at minimum `?provider=`) do not hold up under
  the live SSE tail — the initial server-render filters correctly, but the
  streamed replay + live log lines are appended unfiltered, so the operator
  perceives "filters not working."

## Current State Analysis

- The dashboard renders a **provider health summary** with `provider-badge`
  elements (`providerHealthBadge`, `proxy/web/types.go:71`) — name, status,
  mapping count, last-checked. Each badge is currently a full-page link to the
  logs page: `proxy/web/templates/index.html:123` (`<a href="/logs?provider=…">`).
- The **mapping drawer** is the established in-place pattern: dashboard routing
  rows carry `hx-get="/v1/mappings/{{.Name}}/detail" hx-target="#mapping-drawer"`
  (`index.html:86`); the endpoint `handleMappingDetail`
  (`proxy/web/handlers.go:905`) renders `mapping-drawer.html`
  (`loadFragmentTemplate`); `openDrawer`/`closeDrawer` live in
  `proxy/web/static/app.js:41`/`:55`.
- No `GET /v1/providers/{name}/detail` route exists — provider routes are
  `POST/PUT/DELETE /v1/providers…` plus `/models/refresh` and `/test`
  (`handlers.go:347-386`). Provider details on `/providers` use an *inline*
  `<details>`, not a drawer.
- **(B) root cause confirmed**: the SSE logs handler
  (`internal/eventstream/handlers.go:136`) streams *all* logs — both the buffered
  replay (`:159-166`) and the live subscription (`:170-185`) — and reads only the
  `since` query param; it ignores filters. The client's `htmx:sseMessage` handler
  (`proxy/web/templates/logs.html:158-181`) appends every `log` event via
  `appendLogLine` with no filter check. The HTMX form filter
  (`handleLogs`, `handlers.go:398`, predicate at `:422-448`) works for the initial
  render and manual filter changes, but the stream bypasses it. Server-side filter
  tests pass (`handlers_phase2_test.go:16`), confirming the breakage is purely in
  the SSE tail path.

### Key Discoveries:

- The drawer machinery is **generic** — `openDrawer(drawer)` takes the swapped
  element, `closeDrawer()` is the only thing hardcoded to `#mapping-drawer`
  (`app.js:58`); the keydown Escape/Tab trap and overlay-click handlers also
  reference `#mapping-drawer` by id (`app.js:92-110`). Supporting a second drawer
  means generalizing these id checks, not rewriting the mechanism.
- Both SSE replay and live lines arrive as SSE `log` events through the single
  `htmx:sseMessage` listener — so **one client-side filter at that point covers
  both**, with no change to the SSE endpoint or reconnect logic.
- `deriveProviderStatus(ps)` (`handlers.go:570`) is the single source of truth for
  the provider status slug, already used by the dashboard health summary and the
  providers page — reuse it for the drawer badge.

## Desired End State

- Clicking a provider badge on the dashboard opens a right-side drawer showing
  that provider's name, status badge, protocol, base URL, and API-key env
  presence, with an "Edit" link to `/providers?provider=<name>`. The badge no
  longer navigates away.
- On the logs page, applying any filter (provider, mapping, level, outcome,
  fallback) holds for both the buffered replay and the live tail — streamed lines
  that don't match the active filters are dropped client-side.

## What We're NOT Doing

- Not changing the SSE `/v1/logs` endpoint to honor filters server-side (the
  shared stream is used by other consumers; filtering client-side is sufficient
  and lower-risk). Documented as an explicit non-goal so a future reader doesn't
  duplicate it.
- Not adding live/SSE-updating stats to the provider drawer (static snapshot, like
  the mapping drawer).
- Not showing models or mapping count inside the provider drawer (decided scope:
  identity + config + edit link only).
- Not rebuilding the `/providers` `<details>` view.

## Implementation Approach

Mirror the mapping drawer end-to-end for providers, then fix the logs SSE tail
client-side. Keep the provider detail handler self-contained: it reads
`ProvidersSnapshot()` and `ProviderSnapshot()` (for status) directly, so it needs
no new field on `providerHealthBadge`.

## Critical Implementation Details

- **Drawer JS must be generalized, not duplicated.** `closeDrawer()` (app.js:58)
  and the Escape/Tab/overlay handlers hardcode `#mapping-drawer`. Extend them to
  operate on whichever drawer matches `.drawer--open` (or accept the id list
  `["mapping-drawer","provider-drawer"]`), and the `htmx:beforeRequest` /
  `htmx:afterSwap` listeners (app.js:75-86) to also treat `provider-drawer` as a
  valid swap target. Do not fork `openDrawer`/`closeDrawer`.
- **(B) is a client-side fix only.** The SSE handler (`eventstream/handlers.go:136`)
  intentionally streams all logs and is shared; do **not** add filter params to it.
  Filter inside `htmx:sseMessage` against the current `.log-filters` input values,
  reusing the exact predicate semantics from `handleLogs` (`handlers.go:422-448`:
  case-insensitive substring match on the line for provider/mapping; outcome by
  level; fallback by `"fallback"` substring; level by `min`).

## Phase 1: Provider detail backend

### Overview

Add the `GET /v1/providers/{name}/detail` endpoint and its data struct, mirroring
`handleMappingDetail` / `drawerData`.

### Changes Required:

#### 1. Provider drawer data struct

**File**: `proxy/web/types.go`

**Intent**: Add a `providerDrawerData` struct (read-only view of one provider,
enriched with live status + API-key env presence) rendered by the drawer fragment.

**Contract**: New exported type alongside `drawerData` (`types.go:167`):

```go
type providerDrawerData struct {
    Name        string // provider name
    StatusLabel string // "Healthy"/"Degraded"/"Error"/"Unknown" (badge text)
    StatusSlug  string // deriveProviderStatus output → badge--status-<slug>
    Protocol    string
    BaseURL     string
    EnvDeclared bool // provider declares DefaultAPIKeyEnv
    EnvPresent  bool // that env var is set in the process
    EditLink    string // "/providers?provider=" + url.QueryEscape(Name)
}
```

#### 2. Provider detail handler

**File**: `proxy/web/handlers.go`

**Intent**: Render the provider drawer fragment for a named provider; return 404
JSON when the provider is unknown (mirrors `handleMappingDetail`'s not-found
behavior at `handlers.go:918-921`).

**Contract**: New `handleProviderDetail(w, r, h, logger)` modeled on
`handleMappingDetail` (`handlers.go:905`). Resolve `r.PathValue("name")`; look up
`h.Cfg.ProvidersSnapshot()[name]`; if missing, `writeJSONError(w, 404,
"not_found", "provider not found")`. Otherwise compute:
- `statusSlug := deriveProviderStatus(h.Stats.ProviderSnapshot()[name])`,
  `statusLabel` from a small map {healthy→Healthy, degraded→Degraded, error→Error,
  unknown→Unknown};
- `envDeclared`/`envPresent` from `p.DefaultAPIKeyEnv` + `os.Getenv`;
- `EditLink := "/providers?provider=" + url.QueryEscape(name)`.
Then `loadFragmentTemplate("provider-drawer.html")` →
`tmpl.ExecuteTemplate(w, "provider-drawer", data)` (same nil/err handling as
`handlers.go:972-981`).

#### 3. Route registration

**File**: `proxy/web/handlers.go`

**Intent**: Mount the provider drawer endpoint next to the mapping drawer route.

**Contract**: Add beside `handlers.go:376`:
`mux.HandleFunc("GET /v1/providers/{name}/detail", func(w, r) {
handleProviderDetail(w, r, h, logger) })`. The pattern `GET /v1/providers/{name}/detail`
does not collide with `GET /v1/providers` (page) or `POST/PUT/DELETE
/v1/providers/`.

### Success Criteria:

#### Automated Verification:

- `mage test` (or `go test ./proxy/web/...`) passes — new handler tests compile and pass
- `mage vet` / `mage lint` clean

#### Manual Verification:

- Direct `GET /v1/providers/<known>/detail` returns HTML fragment with name, status, protocol, base URL, env presence
- `GET /v1/providers/does-not-exist/detail` returns 404 JSON

**Implementation Note**: After this phase's automated checks pass, pause for
manual confirmation before proceeding.

---

## Phase 2: Provider drawer frontend

### Overview

Convert the dashboard provider badge into a drawer trigger, add the
`provider-drawer.html` fragment, add a `#provider-drawer` aside, and generalize
the drawer JS.

### Changes Required:

#### 1. Badge becomes a drawer trigger

**File**: `proxy/web/templates/index.html`

**Intent**: Replace the full-page `<a href="/logs?provider=…">` badge
(`index.html:123`) with an HTMX trigger that loads the provider drawer fragment
into `#provider-drawer`, so clicking a badge opens the drawer in place.

**Contract**: The badge element gains `hx-get="/v1/providers/{{.Name | urlPath}}/detail"`
`hx-target="#provider-drawer"` `hx-swap="innerHTML"` (mirroring the mapping row at
`index.html:86`). Prefer a `<button class="provider-badge provider-badge--{{.Status}}">`
for accessibility; preserve the existing inner spans
(`.provider-badge__icon/__name/__count`) and `title` tooltip. Drop the `href` so
no navigation occurs.

#### 2. Provider drawer fragment template

**File**: `proxy/web/templates/provider-drawer.html`

**Intent**: Render the provider detail drawer, reusing the `.drawer__*` shell and
`closeDrawer()` so behavior matches `mapping-drawer.html`.

**Contract**: `{{define "provider-drawer"}}` mirroring `mapping-drawer.html`
structure: `.drawer__header` (title = `.Name`, close button → `closeDrawer()`),
`.drawer__body` with a Status field using `badge--status-{{.StatusSlug}}` +
`{{.StatusLabel}}`, fields for Protocol, Base URL, and an API-key env line that
reads `EnvDeclared`/`EnvPresent` ("Declared · Set" / "Declared · Missing" /
"Not required"), and a `.drawer__actions` anchor `href="{{.EditLink}}"`
class `btn btn--primary` labeled "Edit on Providers page".

#### 3. Drawer container

**File**: `proxy/web/templates/index.html`

**Intent**: Add a second drawer aside so the provider drawer has its own container
beside `#mapping-drawer` (`index.html:177`).

**Contract**: Add `<aside id="provider-drawer" class="drawer" aria-label="Provider details"></aside>`
adjacent to the existing `#mapping-drawer` aside (the shared `#drawer-overlay`
already covers both).

#### 4. Generalize drawer JS

**File**: `proxy/web/static/app.js`

**Intent**: Make the drawer open/close/focus-trap/overlay handlers container-agnostic
so `#provider-drawer` is treated identically to `#mapping-drawer`.

**Contract**: In `htmx:beforeRequest` and `htmx:afterSwap` (app.js:75-86) accept
both `mapping-drawer` and `provider-drawer` as swap targets. Change `closeDrawer`
(app.js:55) and the keydown/overlay handlers (app.js:92-110) to operate on the
drawer carrying `.drawer--open` (or a `["mapping-drawer","provider-drawer"]` id
list) instead of the literal `#mapping-drawer`. Preserve overlay show/hide and
focus-return-to-`drawerOpener` exactly.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (template renders; no regression in existing drawer tests)
- `mage lint` clean

#### Manual Verification:

- Clicking a dashboard provider badge opens the right-side drawer with correct status/protocol/base URL/env state
- "Edit on Providers page" links to `/providers?provider=<name>` with the table pre-filtered
- Escape, overlay-click, and focus trap all work for the provider drawer; closing returns focus to the badge
- Mapping drawer behavior is unchanged

**Implementation Note**: After this phase's automated checks pass, pause for
manual confirmation before proceeding.

---

## Phase 3: Logs filter fix (defect B)

### Overview

Filter the live SSE tail client-side against the active filter inputs so the
`?provider=` (and other) filters hold for replayed and live log lines.

### Changes Required:

#### 1. Client-side SSE filtering

**File**: `proxy/web/templates/logs.html`

**Intent**: Drop streamed `log` lines that don't match the currently selected
filters, before they reach `appendLogLine`, fixing the perceived "filters not
working" on the live tail.

**Contract**: Inside the `htmx:sseMessage` handler (`logs.html:158`), before
creating/appending the `<pre>`, read the current `.log-filters` input values
(`#provider-filter`, `#mapping-filter`, `#level-filter`, `#outcome-filter`,
`#fallback-filter`) and apply the **same** predicate as `handleLogs`
(`handlers.go:422-448`): case-insensitive substring match on `data.line` for
provider and mapping; outcome by `data.level >= error`; fallback by `"fallback"`
substring in `data.line`; level via the selected `min` threshold. Skip
`appendLogLine` (and the `log-empty` removal) when the line fails. Only the
`log` SSE event is filtered; `replay` control events pass through untouched.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (add a focused test asserting `handleLogs` predicate parity if one doesn't already cover all five filters)

#### Manual Verification:

- Open `/logs?provider=openai`; the initial view is filtered AND newly streamed lines are all openai-related
- Change the provider filter live; the tail immediately starts dropping non-matching lines
- Level/outcome/fallback filters also hold on the live tail
- No duplicate/replay pollution when the SSE reconnects

**Implementation Note**: After this phase's automated checks pass, pause for
manual confirmation before proceeding.

---

## Phase 4: Tests & verification

### Overview

Lock in both defects with table-driven tests mirroring the existing dashboard /
provider-status / logs suites.

### Changes Required:

#### 1. Provider drawer handler tests

**File**: `proxy/web/handlers_provider_detail_test.go` (new)

**Intent**: Cover the new endpoint with the same shape as
`handlers_provider_status_test.go` and `handlers_dashboard_test.go`.

**Contract**: Table tests for `GET /v1/providers/{name}/detail`:
- known provider → 200, body contains `provider-drawer`, `.Name`, status slug
  class `badge--status-<slug>`, protocol, base URL, and the edit link
  `/providers?provider=<name>`;
- provider with `DefaultAPIKeyEnv` set/unset in process → env line reflects
  Set/Missing/Not required;
- unknown provider → 404 JSON with `not_found` code;
- unknown provider does NOT render a drawer fragment.

#### 2. Logs filter parity test

**File**: `proxy/web/handlers_phase2_test.go` (extend) or new `handlers_logs_filter_test.go`

**Intent**: Assert the server-side filter predicate rejects the cases the SSE tail
was leaking, guarding against regression.

**Contract**: Cases spanning all five filters (provider substring, mapping
substring, level `min`, outcome success/error, fallback true/false), asserting
`handleLogs` output honors each (mirror `TestHandleLogs_ProviderFilter` at
`handlers_phase2_test.go:16`).

#### 3. Frontend drawer wiring check

**File**: `proxy/web/handlers_dashboard_test.go` (extend)

**Intent**: Assert the dashboard badge now carries the drawer trigger, not the logs
link.

**Contract**: Dashboard body contains `hx-get="/v1/providers/` and `hx-target="#provider-drawer"` on the badge, and NO `href="/logs?provider=` on the badge element.

### Success Criteria:

#### Automated Verification:

- `mage test` (full suite, race detector) passes
- `mage lint` and `mage vet` clean
- `mage build` produces a working binary

#### Manual Verification:

- Full manual pass of Phases 1-3 scenarios in a running instance
- No regressions in mappings drawer, providers page, or logs page filtering

## Testing Strategy

### Unit Tests:

- Provider detail handler: status slug, env presence states, 404 case, edit-link shape
- Logs filter predicate: all five dimensions, parity with what the SSE tail must drop
- Dashboard render: badge trigger attributes present, logs `href` removed

### Integration Tests:

- End-to-end: dashboard badge click → drawer opens → edit link pre-filters providers
- Logs: deep-link filter + live tail both honor filters

### Manual Testing Steps:

1. Start the dashboard, confirm providers show healthy/degraded/error/unknown badges
2. Click each badge type; verify drawer content + focus/escape/overlay behavior
3. Open `/logs?provider=<x>`; observe both buffered and live lines are filtered
4. Toggle each filter live; confirm tail respects it
5. Verify `GET /v1/providers/<bad>/detail` returns 404 JSON

## Performance Considerations

- The provider drawer is a one-shot HTMX swap of a tiny fragment — negligible.
- The logs SSE filter adds a cheap per-line substring check on already-rendered
  data; no DOM/network cost beyond what already runs. Client-side DOM cap
  (`MAX_LOG_LINES = 500`, `logs.html:116`) is unchanged.

## Migration Notes

- No data migration; dashboard badges change behavior (navigation → drawer) which
  is the intended fix. The logs deep-link (`/logs?provider=…`) remains valid and
  now also filters the tail.

## References

- Frame brief: `context/changes/provider-badge-drawer/frame.md` (defines defects A + B)
- Mapping drawer handler: `proxy/web/handlers.go:905`
- Mapping drawer route: `proxy/web/handlers.go:376`
- `deriveProviderStatus`: `proxy/web/handlers.go:570`
- `drawerData` struct: `proxy/web/types.go:167`
- Dashboard badge (current link): `proxy/web/templates/index.html:123`
- Drawer JS: `proxy/web/static/app.js:41` (`openDrawer`), `:55` (`closeDrawer`), `:75-110` (listeners)
- Logs SSE handler: `internal/eventstream/handlers.go:136`
- Logs server filter predicate: `proxy/web/handlers.go:422-448`
- Logs SSE client handler: `proxy/web/templates/logs.html:158`

## Progress

### Phase 1: Provider detail backend

#### Automated

- [x] 1.1 `providerDrawerData` struct added to types.go — b7de758
- [x] 1.2 `handleProviderDetail` renders fragment; 404 JSON for unknown provider — b7de758
- [x] 1.3 `GET /v1/providers/{name}/detail` route registered — b7de758
- [x] 1.4 `mage test ./proxy/web/...` passes — b7de758

#### Manual

- [ ] 1.5 Direct fragment fetch shows name/status/protocol/base URL/env; unknown → 404 JSON

### Phase 2: Provider drawer frontend

#### Automated

- [x] 2.1 Dashboard badge is an hx-get trigger to `#provider-drawer`; logs `href` removed — de16776
- [x] 2.2 `provider-drawer.html` fragment renders status/protocol/base URL/env/edit link — de16776
- [x] 2.3 `#provider-drawer` aside added to index.html — de16776
- [x] 2.4 Drawer JS generalized for both container ids — de16776
- [x] 2.5 `mage test` + `mage lint` pass — de16776

#### Manual

- [ ] 2.6 Badge opens drawer; edit link pre-filters /providers; escape/overlay/focus-trap work; mapping drawer unchanged

### Phase 3: Logs filter fix

#### Automated

- [x] 3.1 `mage test` passes (logs filter parity covered) — 158ddb8

#### Manual

- [ ] 3.2 Deep-link + live tail both honor all five filters; no replay pollution

### Phase 4: Tests & verification

#### Automated

- [x] 4.1 Provider detail handler table tests pass
- [x] 4.2 Logs filter parity test added
- [x] 4.3 Dashboard badge trigger assertion added
- [x] 4.4 Full `mage test` (race) + `mage lint` + `mage vet` + `mage build` clean

#### Manual

- [ ] 4.5 Full manual pass of Phases 1-3 with no regressions
