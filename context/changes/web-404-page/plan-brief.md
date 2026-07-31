# Custom 404 Page — Plan Brief

> Full plan: `context/changes/web-404-page/plan.md`

## What & Why

The dashboard's `GET /` route is a Go 1.22 `ServeMux` catch-all, so every unknown GET path (a typo'd link, `/logz`, a stale bookmark) silently renders the full dashboard with **200 OK** — a dead end that misreports the resource as existing. This change makes `/` a real boundary: exactly `/` serves the dashboard; everything else returns a branded, helpful **404** page. Missing `/static/` assets get the same treatment instead of the FileServer's plain-text error.

## Starting Point

A working, already-redesigned embedded dashboard (`proxy/web/`): Go `html/template` + htmx, one shared `layout.html` with a sidebar keyed off `pageData.Active`, a cached per-page render helper (`renderPage`), and `httptest`-based handler tests. No custom 404 exists anywhere.

## Desired End State

`GET /` still shows the dashboard (200). Any other unknown GET path shows a full-layout 404 — sidebar intact, an oversized `404` numeral, a "Page not found" headline, a "Back to dashboard" button, and quick links to Mappings/Providers/Logs — with a real **404** status. A missing `/static/*` file returns that same branded HTML (status 404, no long-cache header), while real assets still return 200 with `max-age=300`.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Page shell | Full layout + sidebar | Gives an instant "way back" and visual consistency for free. | Plan |
| Coverage | Unknown GET pages **+** static-asset misses | User wanted static misses branded too, not just navigation. | Plan |
| Copy & CTA | Headline + quick links to sections | Multiple exits, no dead end; avoids echoing the raw path. | Plan |
| Styling | Dedicated `.not-found` block, large `404` numeral | Distinct from `.empty-state`; typographic presence, on-brand tokens. | Plan |
| Method mismatch | Left as stdlib default | Broadening to non-GET adds routing logic for little real gain. | Plan |

## Scope

**In scope:**
- Status-aware `renderPageStatus` (refactor `renderPage` onto it) + `renderNotFound` helper.
- New `templates/404.html` and a `.not-found` CSS block.
- Branch the `GET /` handler on `r.URL.Path != "/"`.
- Intercept `http.FileServerFS`'s 404 for missing `/static/` assets.
- Tests for both 404 paths + `/`-still-200 and asset-still-200 regressions.

**Out of scope:**
- Non-GET / 405 method-mismatch pages; custom 401/403/500 pages.
- Any routing-library or auth changes; echoing the attempted path.

## Architecture / Approach

Two small, additive changes in `proxy/web/`. (1) A status-aware renderer lets a page emit `WriteHeader(404)` before its body; `renderNotFound` centralizes the branded page so both entry points render identically. (2) The `/` catch-all guards on the exact path; a thin `ResponseWriter` wrapper watches for the FileServer's `WriteHeader(404)`, diverts to `renderNotFound`, and swallows the plain body — passing 200/304/206 straight through.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Page-route 404 | Branded 404 for unknown GET paths + helper/template/CSS + tests | Breaking the `/` → dashboard path (covered by regression test) |
| 2. Static-asset 404 | Branded 404 for missing `/static/*` via response interceptor | Corrupting 200/304/206 or the `max-age` header (covered by tests + "only divert 404" rule) |

**Prerequisites:** None — all within `proxy/web/`, no new deps (stdlib `net/http` only, per `AGENTS.md`).
**Estimated effort:** ~1 session, 2 phases.

## Open Risks & Assumptions

- Assumes Go 1.22 `ServeMux` precedence keeps `/health`, `/logs`, `/providers`, `/mappings`, `/static/`, `/v1/...` ahead of the `/` catch-all (verified in `SetupMux`).
- Under `RequireAuth` (when `AuthToken` set), unknown paths are auth-gated first — unauthenticated requests still get 401, not the 404; this is intended.
- Rendering the full sidebar layout for a missing `.css`/`.js` is slightly unusual but harmless (status 404 is what matters to loaders; the HTML only shows on direct navigation).

## Success Criteria (Summary)

- Unknown GET path and missing static asset both return status **404** with the branded HTML page.
- `GET /` and real static assets are unchanged (200; assets keep `max-age=300`).
- `mage test` (race) and `mage lint` stay green.
