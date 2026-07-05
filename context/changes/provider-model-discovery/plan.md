# Provider Model Discovery UI — Implementation Plan

## Overview

Revise the already-implemented model-discovery feature so the "Fetch models" affordance lives **only** on the mapping modal, not on the Providers page. The `model_string` field keeps its two existing modes conceptually (free typing still works) but replaces the current auto-populated `<datalist>` with an explicit "Fetch models" button that reveals a custom clickable list; picking an entry fills the input. This removes the Providers-page button/column and the cache-only `GET` endpoint, since nothing in the new design reads the cache without being willing to fetch.

## Current State Analysis

Phases 1-3 of the original plan are committed (`e4d6f68`, `ffa1b13`, epilogue `09819d0`). On top of that, uncommitted local edits had already started bending the fragment toward a dual-mode render (`DatalistMode bool` + `?target=datalist` query param) in an attempt to support both surfaces from one template. That in-progress direction is being replaced, not extended, per this plan.

**What exists today:**

- **Providers page** (`proxy/web/templates/providers.html:8,31,34-41`) — an 8th `Models` table column (`<td id="models-{{.Name}}"></td>`) and a `btn-sm` "Fetch models" button (`hx-post=".../models/refresh" hx-target="#models-{{.Name}}" hx-swap="innerHTML"`) in the Actions cell, ahead of Edit/Delete.
- **Mapping modal** (`proxy/web/templates/mappings.html:56-96`) — the provider `<select>`'s `onchange` and the `editMapping()` JS function both fire `htmx.ajax('POST', '/v1/providers/'+name+'/models/refresh?target=datalist', {target:'#model-suggestions', swap:'innerHTML'})` automatically (no explicit user action), populating a hidden `<datalist id="model-suggestions">` behind the free-text `model_string` input (`list="model-suggestions"`).
- **Fragment template** (`proxy/web/templates/models-fragment.html`) — branches on `.DatalistMode`: `true` renders bare `<option>` elements (for the native datalist), `false` renders a `<ul class="model-list">` of `<li><strong>{{.ID}}</strong> — {{.DisplayName}}</li>` plus a "Fetched X ago" / error footer.
- **Handlers** (`proxy/web/handlers.go:74-78,649-745`) — `GET /v1/providers/{name}/models` → `handleFetchModels` (cache-only read, always `DatalistMode: true`); `POST /v1/providers/{name}/models/refresh` → `handleRefreshModels` (forces upstream fetch, branches render mode on `?target=datalist`).
- **View model** (`proxy/web/types.go:60-66`) — `modelsData{Provider, Models, FetchedAt, Error, DatalistMode}`.
- **Tests** (`proxy/web/handlers_models_test.go`, 6 tests) — 2 test the `GET` route directly (`TestFetchModels_ColdCache`, `TestFetchModels_NamedNonexistent`); 2 more (`TestRefreshModels_AfterRefreshGetCached`, `TestFetchModels_CachedAfterFailedRefresh`) call `POST` then assert against a follow-up `GET`.
- **Styling** (`proxy/web/static/app.css:204-225`) — `.model-list` (scrollable `<ul>`, max-height 130px, top-border between items) and `.text-muted`. No absolute-positioning/overlay convention exists anywhere in this stylesheet — dialogs and lists are laid out inline.

**Untouched by this change:** `proxy/models.go` (`ModelsCache`, `deriveModelsURL`, `fetchModels`/`proxy.FetchModels`) and its tests — the core domain logic is correct and unaffected by this UI-surface rework. `proxy/web/embed.go`'s `loadFragmentTemplate` plumbing (standalone-parse, bypasses `layout.html` and the missing-partials bug) is also unaffected.

## Desired End State

The Providers page has no model-fetching UI at all — it reverts to its pre-feature 7-column table. The mapping modal (both Add and Edit) shows a "Fetch models" button next to the `model_string` input. Clicking it always performs a fresh `POST …/refresh` and, on success, reveals a clickable list of model IDs beneath the input; clicking an entry fills `model_string` with that ID and hides the list (the user still clicks Save to submit — nothing auto-submits). Typing directly into `model_string` without ever clicking "Fetch models" continues to work exactly as before. Errors (missing API key, unreachable upstream, no base URL) render inline via the existing `.form-error` styling, in the same fragment slot the list would otherwise occupy. Selecting a provider or opening the Edit modal no longer triggers any network call by itself.

### Key Discoveries:

- Nothing in the new design reads the `ModelsCache` without also being willing to trigger a fetch — the mapping modal's only path is the explicit `POST …/refresh` — so `GET /v1/providers/{name}/models` and `handleFetchModels` have no remaining caller and should be deleted outright.
- The existing `<li>` markup in the fragment's non-datalist branch has no click affordance or identifying attribute; the new single rendering shape needs each `<li>` to carry the model ID (e.g. `data-model-id="{{.ID}}"`) plus a shared click handler (event delegation on the list container, or `onclick` per `<li>`) that reads it back into the form.
- No absolute-positioning/overlay CSS convention exists in `app.css` — the clickable list should render inline (matching the existing `.model-list` visual shape already used on the Providers page today), requiring no new CSS beyond what already exists.
- The codebase's inline-`onclick`-reads-sibling-form-fields pattern (`editProvider`, `editMapping` in the respective templates) is the established house style for this kind of small DOM interaction — the new click-to-fill handler should follow the same style rather than introducing a new JS pattern.
- `handleRefreshModels`'s `datalistMode` branch (`r.URL.Query().Get("target") == "datalist"`) becomes dead weight once there is only one caller and one rendering mode — it and the `DatalistMode` field should be deleted, not repurposed.

## What We're NOT Doing

- **Not touching Phase 1 core domain** — `ModelsCache`, `deriveModelsURL`, `proxy.FetchModels`, and their tests are unchanged.
- **Not adding a Providers-page view of fetched models in any form** — no read-only list, no button, no column. Model discovery is scoped entirely to the mapping modal.
- **Not auto-submitting the mapping form on model click** — picking a model only fills the input; Save remains a separate explicit action.
- **Not adding a dedicated close/collapse control on the model list** — picking an item is the only way the list disappears (short of closing the whole modal via Save/Cancel).
- **Not caching/toggling client-side to avoid re-fetching** — every "Fetch models" click re-hits the upstream `/models` endpoint via `POST …/refresh`, matching the existing refresh-button semantics.
- **Not adding a loading/disabled state to the button** — no existing template in this codebase uses `hx-indicator`/`hx-disabled-elt`; this stays consistent with that minimalism.
- **Not persisting the cache to disk, adding TTL, or adding SSE push** — these were out of scope in the original plan and remain so.

## Implementation Approach

Three phases, working from the network surface inward to the DOM: (1) simplify the backend by deleting the now-unused `GET` route/handler and the dual-mode branching it existed to serve, (2) collapse the fragment template to the single clickable-list rendering shape the mapping modal needs, (3) rewire the two templates — strip the Providers page down and give the mapping modal its explicit button + list + click-to-fill behavior. Each phase leaves the build and test suite green.

## Phase 1: Backend Simplification

### Overview

Remove the `GET /v1/providers/{name}/models` route and its handler, and strip the now-single-purpose `handleRefreshModels` of its `DatalistMode`/`?target=datalist` branching. Update the view model and rewrite the tests that exercised the deleted route.

### Changes Required:

#### 1.1 Delete the GET route and handler

**File**: `proxy/web/handlers.go`

**Intent**: Remove the cache-only read path — the mapping modal's only remaining call is the explicit `POST …/refresh`, so a route that reads the cache without fetching has no caller.

**Contract**: Delete the `mux.HandleFunc("GET /v1/providers/{name}/models", …)` registration (`handlers.go:74-76`) and the `handleFetchModels` function (`handlers.go:649-675`) in full. Keep the `POST /v1/providers/{name}/models/refresh` registration (`handlers.go:77-79`) and `handleRefreshModels`.

#### 1.2 Simplify `handleRefreshModels`

**File**: `proxy/web/handlers.go`

**Intent**: Drop the dual-rendering-mode branch now that there is exactly one caller and one rendering shape.

**Contract**: Remove `datalistMode := r.URL.Query().Get("target") == "datalist"` (`handlers.go:684`) and every `DatalistMode: datalistMode` field set in the two `modelsData{...}` literals inside this function (`handlers.go:698,711`). The function's control flow (base-URL check → `proxy.FetchModels` → cache `Set` → build `modelsData` → `renderModelsFragment`) is otherwise unchanged.

#### 1.3 Drop `DatalistMode` from the view model

**File**: `proxy/web/types.go`

**Intent**: The field no longer has any producer or consumer after 1.1/1.2.

**Contract**: Remove `DatalistMode bool` from the `modelsData` struct (`types.go:60-66`), leaving `Provider`, `Models`, `FetchedAt`, `Error`.

#### 1.4 Rewrite tests that exercised the deleted GET route

**File**: `proxy/web/handlers_models_test.go`

**Intent**: Delete assertions against a route that no longer exists; preserve equivalent coverage (cache round-trip, error round-trip, unknown-provider 404) against the sole remaining `POST` endpoint.

**Contract**:
- Delete `TestFetchModels_ColdCache` outright (it asserts on a `GET` cold-cache response with `DatalistMode` semantics that no longer apply — a first `POST …/refresh` against a provider with no base URL, or against `httptest.Server` returning an empty `data: []`, already covers "nothing fetched yet" rendering via the other tests).
- Rewrite `TestFetchModels_NamedNonexistent` to `POST /v1/providers/nonexistent/models/refresh` and keep the `404` assertion (per the "keep a 404-on-unknown-provider test against POST" decision).
- Rewrite `TestRefreshModels_AfterRefreshGetCached`: replace the second request (currently `GET /v1/providers/nim/models`) with a second `POST /v1/providers/nim/models/refresh` against the same `httptest.Server`, asserting the response still contains `"cached-model"` (the upstream is idempotent in this test, so a second `POST` reproduces the same observable result the `GET` was checking).
- Rewrite `TestFetchModels_CachedAfterFailedRefresh` similarly: its final assertion step (currently a `GET` after the failed second refresh) becomes an assertion on the **second `POST …/refresh` response itself** — per `handleRefreshModels`'s existing behavior (`handlers.go:713-724`), when `fetchErr != nil` but `len(models) > 0` (stale cache present), the response already renders both the cached models and the error string in the same fragment, so the existing test's core assertion (`"persistent-model"` present) can be checked directly on that response instead of a follow-up read.
- `TestRefreshModels_WithUpstream` and `TestRefreshModels_UpstreamError` are unaffected — leave as-is.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/web/ -run "TestFetchModels|TestRefreshModels" -race -count=1` passes
- `go test ./proxy/... ./proxy/web/... -race -count=1` — no regressions
- `mage lint` passes

#### Manual Verification:

- N/A (backend-only; covered by automated tests)

---

## Phase 2: Fragment Template Rework

### Overview

Collapse `models-fragment.html` from its current two-branch (`DatalistMode` true/false) shape into a single clickable-list rendering, carrying over the existing empty/error-state messaging verbatim.

### Changes Required:

#### 2.1 Single clickable-list rendering shape

**File**: `proxy/web/templates/models-fragment.html`

**Intent**: Render one `<ul>` of models where each item is clickable and identifiable by model ID, replacing both the old `<option>` branch and the plain (non-clickable) `<li>` branch.

**Contract**: Remove the `{{if .DatalistMode}}...{{else}}...{{end}}` split. Keep a single `<ul class="model-list">` whose `<li>` elements carry `data-model-id="{{.ID}}"` (escaped by the template engine's default HTML escaping) and a display label (`{{.ID}}` plus `— {{.DisplayName}}` when it differs, exactly as today). Keep the existing footer states verbatim: "Fetched X ago" when `.FetchedAt` is set, `.form-error` div when `.Error` is set, "No models fetched yet" when both `.Models` is empty and `.Error` is empty, and the >1000-truncation notice. The outer wrapper stays `<div id="models-{{.Provider}}">` (unchanged) so the mapping modal's `hx-target`/`hx-swap` continues to work against the same container id convention already used elsewhere in this template.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/web/... -race -count=1` passes (existing fragment-rendering assertions in the Phase 1 tests continue to pass against the new markup shape)
- `mage lint` passes

#### Manual Verification:

- N/A (template-only; verified end-to-end in Phase 3's manual steps)

---

## Phase 3: UI Wiring — Strip Providers Page, Wire the Mapping Modal

### Overview

Remove the Providers-page Models column and button entirely. Add an explicit "Fetch models" button and a hidden-until-populated list container to the mapping modal, remove the auto-fetch triggers, and add the click-to-fill handler.

### Changes Required:

#### 3.1 Revert Providers page to 7 columns, no fetch UI

**File**: `proxy/web/templates/providers.html`

**Intent**: Model fetching no longer has any presence on this page.

**Contract**: Remove the `<th>Models</th>` header (`providers.html:8`), the `<td id="models-{{.Name}}"></td>` cell (`providers.html:31`), and the "Fetch models" button block (`providers.html:34-41`) from the Actions cell. The table returns to its original 7 columns (Name, Behavior, Base URL, API Key Env, Protocol, Mappings, Actions) with just Edit/Delete in the Actions cell.

#### 3.2 Remove auto-fetch triggers from the mapping modal

**File**: `proxy/web/templates/mappings.html`

**Intent**: Selecting a provider or opening the Edit modal must not perform any network call on its own — fetching becomes an explicit action.

**Contract**: Remove the `onchange` handler on the provider `<select>` (`mappings.html:59-66`) that currently calls `htmx.ajax(...)`. Remove the equivalent `htmx.ajax('POST', '/v1/providers/' + provider + '/models/refresh?target=datalist', ...)` call inside `editMapping()` (`mappings.html:85-96`) — `editMapping` keeps setting `f.elements.model_string.value = model` (the existing value) but no longer triggers a fetch.

#### 3.3 Add "Fetch models" button + list container to the mapping modal

**File**: `proxy/web/templates/mappings.html`

**Intent**: Give the modal an explicit, always-available fetch action tied to whichever provider is currently selected in the form, and a container for the resulting clickable list.

**Contract**: Replace the `<datalist>`-based markup (`mappings.html:67-70`) with: the `model_string` input (no longer needs `list="model-suggestions"`), a `btn-sm` button labeled "Fetch models" whose click handler reads the currently-selected provider from the sibling `<select name="provider_name">` and issues `htmx.ajax('POST', '/v1/providers/' + providerName + '/models/refresh', {target: '#model-suggestions', swap: 'innerHTML'})` (dropping the now-removed `?target=datalist` param), and a `<div id="model-suggestions"></div>` target replacing the old `<datalist>` element. If no provider is selected yet, the button click is a no-op (guard clause, mirroring the existing `if (!name) return;` guard already present in the code being replaced).

#### 3.4 Click-to-fill handler

**File**: `proxy/web/templates/mappings.html`

**Intent**: Clicking a model in the fetched list fills `model_string` and hides the list, without submitting the form.

**Contract**: Add a small function (e.g. `selectModel(li)`) following the same inline-script, sibling-form-field-access style as `editMapping`/`editProvider`: reads `li.dataset.modelId`, sets `document.getElementById('mapping-form').elements.model_string.value = ...`, and clears the list container's contents (`document.getElementById('model-suggestions').innerHTML = ''`). Wire it via a single delegated `onclick` on the `#model-suggestions` container (checking `event.target.closest('li[data-model-id]')`) rather than one inline `onclick` per rendered `<li>`, so the fragment template doesn't need to embed JS calls per item.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `go vet ./...` passes
- `go test ./proxy/... ./proxy/web/... -race -count=1` passes

#### Manual Verification:

- Providers page shows no Models column and no "Fetch models" button; only Edit/Delete remain in Actions
- Opening "Add Mapping" and selecting a provider triggers no network request (confirm via browser dev tools) until "Fetch models" is clicked
- Clicking "Fetch models" with a valid provider shows the clickable model list inline below the input
- Clicking a model in the list fills `model_string` and hides the list; the form is not submitted
- Typing a custom model ID directly, without ever clicking "Fetch models", still works and can be saved
- Clicking "Fetch models" again re-issues the request and refreshes the list (confirm via dev tools / changed "Fetched X ago" text)
- Opening "Edit Mapping" on an existing mapping shows its current model value in the input and triggers no automatic fetch
- Fetch models against a provider with no API key set, no base URL configured, and an unreachable upstream — all three show an inline `.form-error` message in the same slot the list would occupy, without crashing

---

## Testing Strategy

### Unit Tests:

- `proxy/web/handlers_models_test.go`: `TestRefreshModels_WithUpstream`, `TestRefreshModels_UpstreamError` (unchanged); rewritten `TestFetchModels_NamedNonexistent` (POST, 404), `TestRefreshModels_AfterRefreshGetCached` (POST twice), `TestFetchModels_CachedAfterFailedRefresh` (POST twice, second call's own response carries cached+error)
- `proxy/models_test.go`: unaffected, no changes

### Integration Tests:

- Full flow: `POST …/refresh` → fragment contains clickable `<li data-model-id>` entries → (manually) click fills input

### Manual Testing Steps:

1. `mage run` → open dashboard in browser with `FREEDIUS_UI_TOKEN` set
2. Navigate to Providers → confirm no Models column, no "Fetch models" button
3. Navigate to Mappings → "Add Mapping" → select a provider with a valid API key → click "Fetch models" → verify list appears
4. Click a model in the list → verify it fills the input and the list disappears → Save
5. Reopen "Add Mapping", type a custom model ID without clicking "Fetch models" → Save → verify it's accepted
6. Edit an existing mapping → verify the current model shows in the input and no fetch fires automatically
7. Test a provider with no API key, no base URL, and an unreachable upstream → verify inline error in each case

## Performance Considerations

Unchanged from the original plan — the fetcher's short-timeout `*http.Client`, the in-memory RWMutex-guarded cache, and the absence of background goroutines/timers all carry over untouched.

## Migration Notes

No data migration — this is a UI/handler-surface change only. Any provider whose models were fetched via the old Providers-page button remains in the in-memory cache until the next process restart or explicit re-fetch from the mapping modal; there is no on-disk state to migrate.

## References

- Research: `context/changes/provider-model-discovery/research.md` (original research + "Follow-up Research 2026-07-05T19:08" section)
- Change identity: `context/changes/provider-model-discovery/change.md`
- Original plan history: this file's own `## Progress` section below records what was already implemented under the prior (Providers-page + datalist) design before this revision
- Lessons: `context/foundation/lessons.md` (Adapter Return Contract, x-api-key requirement — inherited by `proxy.FetchModels`, unchanged in this revision)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Backend Simplification

#### Automated

- [x] 1.1 `go test ./proxy/web/ -run "TestFetchModels|TestRefreshModels" -race -count=1` passes — 1f5dc10
- [x] 1.2 `go test ./proxy/... ./proxy/web/... -race -count=1` — no regressions — 1f5dc10
- [x] 1.3 `mage lint` passes — 1f5dc10

### Phase 2: Fragment Template Rework

#### Automated

- [x] 2.1 `go test ./proxy/web/... -race -count=1` passes — 1f5dc10

### Phase 3: UI Wiring

#### Automated

- [ ] 3.1 `mage lint` passes
- [ ] 3.2 `go vet ./...` passes
- [ ] 3.3 `go test ./proxy/... ./proxy/web/... -race -count=1` passes

#### Manual

- [ ] 3.4 Providers page shows no Models column/button
- [ ] 3.5 Selecting a provider in the mapping modal triggers no network request
- [ ] 3.6 "Fetch models" shows a clickable list inline
- [ ] 3.7 Clicking a model fills the input and hides the list without submitting
- [ ] 3.8 Custom typed model IDs still save correctly without ever fetching
- [ ] 3.9 Re-clicking "Fetch models" re-fetches and refreshes the list
- [ ] 3.10 Editing a mapping shows its current model and triggers no automatic fetch
- [ ] 3.11 Error cases (no API key, no base URL, unreachable upstream) show inline `.form-error`, no crash

### Superseded (prior design, for history)

> These items belonged to the pre-revision plan (Providers-page button + auto-populated datalist). Superseded by the phases above — kept here only as a record of what was previously verified before this UI direction changed.

- [x] Phase 1 (original) — `go test ./proxy/ -run "TestDeriveModelsURL|TestModelsCache|TestFetchModels" -race -count=1` passes — 28 tests pass
- [x] Phase 2 (original) — endpoint tests, no regressions, `mage lint` — e4d6f68
- [x] Phase 3 (original) — `mage lint`, `go vet ./...` — ffa1b13
- [ ] Phase 2 (original) manual — `mage run` loads dashboard, direct curl to GET endpoint — superseded, GET endpoint removed in this revision
- [ ] Phase 3 (original) manual — Providers-page button + mapping datalist manual checks — superseded, UI surfaces redesigned in this revision
