# Frame Brief: Provider badge → drawer + broken logs filters

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

1. Clicking any provider badge on the dashboard (`/`) navigates the browser to
   the logs page (`/logs?provider=<name>`) instead of opening an in-place view.
2. The logs **filters are not working** — at minimum the `?provider=` filter does
   not actually restrict the log view as expected.

(User: "more natural for providers would be to behave like mappings, but also
filters are not working in logs.")

## Initial Framing (preserved)

- **User's stated cause or approach**: The provider badge should open the same
  right-side modal/drawer that mapping rows already open from the dashboard.
- **User's proposed direction**: Make provider clicks open the mapping-style
  drawer instead of routing to logs.
- **Pre-dispatch narrowing**: User dismissed the structured questions but supplied
  the decisive signal that the `?provider=` filter on `/logs` is currently broken,
  and confirmed *both* issues should be in scope.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Dashboard provider affordance** — the badge is a full-page `<a>` link, not a
   drawer trigger.  ← user's primary framing
2. **Missing provider detail view** — there is **no** provider drawer or detail
   endpoint; the "same modal" the user references is mapping-specific.
3. **Logs filter defect** — `?provider=` (and possibly other filters) do not
   restrict the rendered logs. Separate from the badge, but it is the badge's
   current destination.
4. **Drawer is hardcoded to mappings** — `#mapping-drawer` id, `mapping-drawer`
   template, and `GET /v1/mappings/{name}/detail` are all mapping-scoped; supporting
   providers means generalizing or adding a parallel drawer.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Badge navigates away (full-page link) | `index.html:123` `<a href="/logs?provider={{.Name}}" class="provider-badge …">` | STRONG |
| Mapping rows open a drawer in place | `index.html:86` `hx-get="/v1/mappings/{{.Name}}/detail" hx-target="#mapping-drawer"` + `app.js:41` `openDrawer` | STRONG |
| No reusable provider modal exists | No `GET /v1/providers/{name}/detail` in `handlers.go` (only `POST/PUT/DELETE /v1/providers…` and `/models/refresh`, `/test`); provider details live only as an expandable `<details>` on `/providers` (`providers-table.html`, `handlers_provider_status_test.go:100`) | STRONG |
| Logs `?provider=` filter broken | User reports broken. Counter-evidence: `handlers_phase2_test.go:16` `TestHandleLogs_ProviderFilter` asserts server-side filtering works. Discrepancy suggests the breakage is in the **live SSE tail / deep-link UX**, not the initial server render — not locally reproduced | WEAK / PARTIAL |
| Inconsistent dashboard interaction model | mappings = in-place drawer; providers = navigation away — confirmed by above | STRONG |

## Narrowing Signals

- User confirmed **both** issues are in scope: provider drawer behavior *and* the
  broken logs filters. They are distinct defects; do not collapse them into one.
- The "reuse the same modal" assumption is only partially true: the drawer
  *mechanism* (overlay, focus trap, Escape/overlay-close in `app.js`) is generic,
  but the *content/endpoint* is mapping-only — a provider drawer must be built.

## Cross-System Convention

- Mappings already establish the right-side drawer pattern (`#mapping-drawer` +
  `hx-get …/detail` + `openDrawer`). The dashboard's in-place interaction model is
  the established convention for row-level detail.
- Providers on the dedicated `/providers` page use an inline expandable `<details>`
  rather than a drawer — so the dashboard would be the *first* place a provider
  drawer appears. Building it reuses the existing drawer plumbing.
- Logs filtering has server-side tests proving the query param works, so the
  reported breakage most likely lives in the SSE live-tail path or the deep-link
  not re-applying filters on navigation — a different layer than the badge.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is two distinct defects:**
> **(A)** the dashboard has no provider detail interaction — its only provider
> affordance is a full-page navigation, whereas mappings open an in-place drawer;
> **(B)** the logs page filters (at minimum `?provider=`) are not functioning as
> expected.

(A) is a *build* task: no provider drawer/endpoint exists, so this is not "reuse
mapping's modal" but "create a provider detail drawer and wire the badge to it,
reusing the generic drawer plumbing." (B) is a *bug* in a different layer (likely
the SSE tail / deep-link) and should be scoped explicitly — possibly its own
change. The initial framing (open a drawer like mappings) is correct for defect A;
it simply did not account for the missing provider view or for defect B.

## Confidence

- **MEDIUM** — Defect A is strongly evidenced (no provider drawer exists; mapping
  drawer is the clear pattern to follow). Defect B is reported by the user but
  contradicts the passing server-side filter test, so its exact root (SSE tail vs
  deep-link vs. other filters) is unverified and should be reproduced before a fix
  is planned.

## What Changes for /10x-plan

The plan is **not** "reuse mapping's modal." It is: **(A)** build a provider detail
drawer — new `GET /v1/providers/{name}/detail` endpoint + fragment template, wire
the dashboard badge from `<a href="/logs?provider=…">` to an `hx-get` drawer
trigger (generalize `#mapping-drawer` or add a parallel drawer), preserving
keyboard/focus/overlay behavior; and **(B)** scope the broken logs filters as a
separate investigation (reproduce: does `?provider=` fail on initial load, on SSE
tail, or only certain filters?). Decide explicitly whether B rides along in the
same change or gets its own.

## References

- Dashboard badge: `proxy/web/templates/index.html:123`
- Mapping drawer trigger: `proxy/web/templates/index.html:86`
- Drawer plumbing: `proxy/web/static/app.js:41` (`openDrawer`), `:55` (`closeDrawer`)
- Mapping detail endpoint: `proxy/web/handlers.go:376` (`GET /v1/mappings/{name}/detail`)
- Provider routes (no detail endpoint): `proxy/web/handlers.go:347-386`
- Provider `<details>` on /providers: `proxy/web/templates/providers-table.html`, `handlers_provider_status_test.go:100`
- Logs filter server test (passing): `handlers_phase2_test.go:16`
- Logs filter UI: `proxy/web/templates/logs.html:25-39`
