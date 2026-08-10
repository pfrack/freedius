# Provider Badge Drawer (+ Logs Filter Fix) — Plan Brief

> Full plan: `context/changes/provider-badge-drawer/plan.md`
> Frame brief: `context/changes/provider-badge-drawer/frame.md`

## What & Why

The dashboard's provider badges navigate away to the logs page, while mapping
rows open an in-place drawer — an inconsistent interaction model — and the logs
page filters don't hold under the live tail. Per the frame's Reframed Problem
Statement, this is **two distinct defects**: (A) no provider detail view exists,
so we build one; (B) the logs `?provider=` (and other) filters are bypassed by the
SSE live stream.

## Starting Point

The dashboard renders provider health badges as full-page `<a href="/logs?provider=…">`
links (`index.html:123`). The mapping drawer already establishes the in-place
pattern (`hx-get …/detail` → `openDrawer`, `app.js:41`). The logs page filters
server-side correctly (`handleLogs`, `handlers.go:398`) but the SSE tail
(`eventstream/handlers.go:136` → `htmx:sseMessage`, `logs.html:158`) appends every
streamed line unfiltered.

## Desired End State

Clicking a dashboard provider badge opens a right-side drawer with that provider's
status, protocol, base URL, and API-key env state, plus an Edit link to the
pre-filtered providers page. On the logs page, filters hold for both the buffered
replay and the live tail.

## Key Decisions Made

| Decision                       | Choice                          | Why (1 sentence)                                              | Source  |
| ------------------------------ | ------------------------------- | ------------------------------------------------------------ | ------- |
| Trigger surface                | Dashboard badges only           | Mirrors the established mapping-drawer interaction          | Plan    |
| Drawer content                 | Identity + config + edit link   | Read-only peek, like mapping-drawer; no stats/models        | Plan    |
| Edit link target               | `/providers?provider=<name>`    | Reuses existing `?provider=` filter, pre-focuses the table   | Plan    |
| Unknown provider               | 404 JSON (mirror mapping)        | Consistent error contract for drawer endpoints              | Plan    |
| Logs filter fix location       | Client-side in `htmx:sseMessage`| One filter covers replay+live; SSE endpoint stays shared     | Plan    |
| (B) scope                      | Included in this change          | User chose to fix both defects together                     | Plan    |

## Scope

**In scope:** provider detail endpoint + fragment + dashboard badge rewire +
drawer-JS generalization; client-side SSE log filtering for all five filters.

**Out of scope:** server-side SSE filter params; live/SSE-updating drawer stats;
drawer models/mapping-count; `/providers` page `<details>` rework.

## Architecture / Approach

Reuse the generic drawer plumbing (`.drawer__*` shell, `openDrawer`/`closeDrawer`,
overlay, focus trap) by adding a parallel `#provider-drawer` aside and generalizing
the id-hardcoded JS. The provider handler reads `ProvidersSnapshot()` +
`ProviderSnapshot()` itself (no new badge field). The logs fix filters streamed
`log` events in `htmx:sseMessage` against the current `.log-filters` inputs, using
the same predicate as the server.

## Phases at a Glance

| Phase | What it delivers                          | Key risk                                |
| ----- | ----------------------------------------- | --------------------------------------- |
| 1     | `GET /v1/providers/{name}/detail` + struct | Status slug/label mapping correctness   |
| 2     | Badge→drawer UI + generalized drawer JS    | Breaking mapping drawer's JS by mistake |
| 3     | Logs SSE client-side filtering             | Filter parity with server predicate      |
| 4     | Table-driven tests for A + B               | Coverage gaps in edge cases             |

**Prerequisites:** existing `proxy/web` dashboard + logs pages; `deriveProviderStatus`
and `loadFragmentTemplate` available.
**Estimated effort:** ~1 session across 4 phases (mostly mechanical, pattern-matched).

## Open Risks & Assumptions

- SSE `sse-connect` does not need filter params because filtering is client-side;
  if reconnect behavior differs in practice, revisit.
- `closeDrawer` focus-return relies on `drawerOpener` captured per-container;
  generalization must preserve it for both drawers.

## Success Criteria (Summary)

- Dashboard provider badges open an in-place drawer (no navigation to logs).
- Logs filters (provider/mapping/level/outcome/fallback) hold for replay + live tail.
- No regressions in the mapping drawer, providers page, or logs initial render.
