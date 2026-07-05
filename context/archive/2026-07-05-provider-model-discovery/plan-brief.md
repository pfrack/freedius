# Provider Model Discovery UI — Plan Brief

> Full plan: `context/changes/provider-model-discovery/plan.md`
> Research: `context/changes/provider-model-discovery/research.md` (see "Follow-up Research 2026-07-05T19:08" section)

## What & Why

Consolidate model-fetching UI onto the mapping modal only, removing it from the Providers page entirely. The `model_string` field should work in one of two explicit modes: free typing (unchanged), or clicking "Fetch models" to reveal a clickable list to pick from — replacing the current design where a `<datalist>` auto-populated silently whenever a provider was selected.

## Starting Point

Phases 1-3 of the original plan are committed. Uncommitted local edits had already started bending the fragment template toward a dual-mode render (`DatalistMode` + `?target=datalist`) to serve both a Providers-page list and a mapping-form datalist from one template — that in-progress direction is replaced, not extended, by this revision.

## Desired End State

The Providers page has no model-fetching UI. The mapping modal has an explicit "Fetch models" button; clicking it always re-fetches upstream and shows a clickable list under the input, and clicking an entry fills the input (Save is still a separate click). Selecting a provider or opening Edit no longer fires any network call by itself.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| List widget | Custom clickable list, not native `<datalist>` | User wants an explicit, visible fetch-then-pick interaction, not silent autocomplete | Research (clarified) |
| Cache-read GET endpoint | Delete `GET /v1/providers/{name}/models` entirely | Nothing in the new design reads the cache without being willing to fetch | Research (clarified) |
| Re-click behavior | Always re-fetch upstream on every "Fetch models" click | Simple one-button-one-action model, matches existing refresh-button semantics | Plan |
| List dismissal | Picking an item hides the list; no separate close control | Modal's Save/Cancel already handles "I don't want this list anymore" | Plan |
| Click-to-select | Fills the field only, does not auto-submit | Consistent with every other field in this form — Save stays the only submit trigger | Plan |
| List positioning | Inline (not an overlay) | No absolute-positioning convention exists anywhere in `app.css`; inline reuses the existing `.model-list` style with zero new CSS | Research |
| Test rewrite | Delete GET-only tests, extend POST tests, keep a 404-on-unknown-provider check | Matches the actual route surface; no dead assertions against a deleted endpoint | Plan |

## Scope

**In scope:**
- Delete `GET /v1/providers/{name}/models` route + `handleFetchModels`
- Simplify `handleRefreshModels` (drop `DatalistMode`/`?target=datalist`)
- Collapse `models-fragment.html` to one clickable-list rendering shape
- Remove Providers-page Models column + "Fetch models" button
- Remove mapping modal's auto-fetch-on-select and auto-fetch-on-edit
- Add explicit "Fetch models" button + click-to-fill list to the mapping modal
- Rewrite the 3 tests that exercised the deleted GET route

**Out of scope:**
- Phase 1 core domain (`ModelsCache`, `deriveModelsURL`, `proxy.FetchModels`) — untouched
- Any read-only model view on the Providers page
- Auto-submitting the form on model click
- A dedicated close/collapse control on the list
- Client-side caching to avoid re-fetching on repeat clicks
- Loading/disabled button state, on-disk cache, TTL, SSE — all previously out of scope, still are

## Architecture / Approach

```
handlers.go (delete GET, simplify POST)  →  models-fragment.html (one render shape)  →  providers.html + mappings.html (strip / rewire)
```

Backend first (smallest, most mechanical), then the shared template, then the two UI surfaces — Providers page shrinks back to its pre-feature shape, mappings.html gains an explicit button + list + click handler following the codebase's existing inline-`onclick`-reads-sibling-fields style (same pattern as `editMapping`/`editProvider`).

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Backend Simplification | GET route/handler deleted, POST handler simplified, tests rewritten | Losing test coverage for edge cases if rewrites aren't equivalent — mitigated by keeping a 404-on-unknown-provider check |
| 2. Fragment Template Rework | Single clickable-list rendering shape | None significant — small, mechanical template collapse |
| 3. UI Wiring | Providers page reverted, mapping modal has explicit fetch+pick flow | Click-to-fill JS must correctly target the right form/provider without reintroducing an auto-trigger |

**Prerequisites:** Existing Phase 1-3 commits in place; a provider with a valid API key set in env for manual testing
**Estimated effort:** ~1 session across 3 small phases

## Open Risks & Assumptions

- Assumes the two rewritten cache-round-trip tests (`TestRefreshModels_AfterRefreshGetCached`, `TestFetchModels_CachedAfterFailedRefresh`) can validate cache persistence via two `POST` calls instead of `POST`+`GET` — this holds because `handleRefreshModels` already renders cached+error state on failure (`handlers.go:713-724`), so no new backend logic is needed to make the rewritten tests pass.
- Assumes no other code path (docs, scripts, other templates) references the `GET /v1/providers/{name}/models` route or `?target=datalist` — confirmed via repo-wide grep during research; only `providers.html`, `mappings.html`, `handlers.go`, and `handlers_models_test.go` touch this surface.

## Success Criteria (Summary)

- Providers page has zero model-fetching UI; Mappings modal is the only place to fetch models
- Clicking "Fetch models" shows a clickable list; picking an entry fills the field without submitting
- Free-text typing without ever fetching continues to work exactly as before
- Errors (no key, no base URL, unreachable upstream) show inline without crashing, in every case
