---
date: 2026-07-30T21:39:00+02:00
researcher: kiro
git_commit: 806e573
branch: main
repository: freedius
topic: "Mapping-first UI refactor: audit current screens and define problems"
tags: [research, codebase, web-ui, mappings, ux]
status: complete
last_updated: 2026-07-30
last_updated_by: kiro
---

# Research: Mapping-First UI Refactor

**Date**: 2026-07-30T21:39:00+02:00
**Researcher**: kiro
**Git Commit**: 806e573
**Branch**: main
**Repository**: freedius

## Research Question

Audit the current Web UI to identify where providers are given equal or greater visual emphasis than mappings, and produce a prioritized problem list for a mapping-first redesign.

## Summary

The current UI treats providers and mappings as co-equal entities. The sidebar places Mappings last. The Dashboard gives Providers its own section with equal visual weight. Mapping cards display "ProviderName / Model" as the strongest visual element in the routing chain, making provider the dominant identity. The UI does not answer "what mappings do I have?" before "what providers exist?"

## Prioritized Problem List

### P1 — Critical (must fix for mapping-first)

1. **Sidebar order inverted** — Current: Dashboard → Logs → Providers → Mappings. Mappings is LAST. Should be: Dashboard → Mappings → Providers → Logs.
2. **Dashboard gives Providers equal section weight** — `dashboard-section` for "Providers" takes ~50% of the page. Should be a subtle subordinate summary.
3. **Route-step chips show "ProviderName / Model"** — Provider name is the first/strongest text in each `route-step`. Model is appended after `/`. The mapping card's visual mass is dominated by provider references.
4. **Dashboard lacks mapping-centric stats** — Only Uptime and Listening On. Missing: total mappings, active (key present), fallback-equipped, providers connected.

### P2 — High (significantly improves mapping-first UX)

5. **Status dot is context-free** — 6px circle (`.status-dot`) with no label. Users can't interpret it without hovering.
6. **No mapping type/family badge prominence** — Family badge exists but is tiny and positioned after the status dot.
7. **Logs page has minimal filtering** — Only a level dropdown. Backend supports `?provider=` and `?mapping=` filters but UI doesn't expose them.
8. **Provider table on Providers page lacks "used by which mappings" visibility** — Only shows a count, not names.

### P3 — Medium (polish and consistency)

9. **route-card__header is cramped** — Name, meta, dot, badge, depth, and actions in one flex row. On narrow screens elements collide.
10. **Inconsistent arrow rendering** — `route-step::after` renders `▶` but `first-child::after` is hidden. Chain reads as disconnected.
11. **No responsive collapse strategy** — Cards just wrap; no progressive disclosure of less-important info (e.g., hide base URL on mobile).
12. **Delete buttons visually loud** — Red `.btn--danger` next to ghost edit creates unbalanced visual weight.

## Detailed Findings

### Sidebar (layout.html)

- Order: Dashboard, Logs, Providers, Mappings
- All items have equal visual treatment
- Active state uses accent color properly
- File: `proxy/web/templates/layout.html:30-55`

### Dashboard (index.html)

- Stats strip: only Uptime + Host:Port — no mapping-related metrics
- Two equal sections: "Mappings" (renders mappings-table fragment) and "Providers" (renders providers-overview)
- `providers-overview` section takes significant visual space, giving providers co-equal importance
- File: `proxy/web/templates/index.html:1-55`

### Mappings Table Fragment (mappings-table.html)

- Route cards with `route-card__header` (name + meta + dot + badge + actions) and `route-chain` (step chips)
- Each `route-step` renders: `{{.ProviderName}} / {{.Model}}`
- Provider name is literally the first text the user reads in each step
- Family badge is present but small and positionally weak
- File: `proxy/web/templates/mappings-table.html:1-63`

### Providers Table Fragment (providers-table.html)

- Standard table with columns: Name, Behavior, Base URL, API Key Env, Protocol, Mappings (count), Actions
- Clean and functional; appropriate for a secondary infrastructure view
- File: `proxy/web/templates/providers-table.html:1-50`

### Logs Page (logs.html)

- Single level-filter dropdown
- SSE stream with `log-{{.Level}}` pre elements
- No mapping or provider filter exposed in UI (but handler supports them)
- No search/text filter
- File: `proxy/web/templates/logs.html:1-45`

### CSS Design System (app.css)

- Well-structured with design tokens (zinc palette, spacing scale, radii, shadows)
- Dark/light mode support via media query
- Responsive breakpoint at 768px (sidebar collapse)
- Route-step, badge, card primitives exist — can be extended
- File: `proxy/web/static/app.css` (580+ lines)

### Handler Data Structures (types.go, handlers.go)

- `indexData` includes both `Mappings []mappingRow` and `Providers []providerRow`
- `mappingRow` already has: Name, ProviderName, Model, Protocol, BaseURL, Fallbacks, Family, EnvPresent, HasResponder
- All data needed for a mapping-first redesign is already available in the template context
- File: `proxy/web/types.go:1-87`, `proxy/web/handlers.go:42-100`

## Architecture Insights

1. **No backend changes needed** — all required data (mapping count, family, env status, fallbacks, last responder) is already computed in `buildMappingRows` and `indexData`.
2. **Template reuse is simple** — `mappings-table.html` is a shared fragment used by both Dashboard and Mappings page. Changing it changes both.
3. **HTMX swap targets** — `#mappings` and `#providers` are the swap targets for CRUD operations. IDs must be preserved.
4. **CSS is a single file** — changes to `app.css` affect everything; no component isolation. Careful naming with BEM-style prefixes is the convention.

## Code References

- `proxy/web/templates/layout.html:30-55` — Sidebar navigation order
- `proxy/web/templates/index.html:1-55` — Dashboard page (stats + sections)
- `proxy/web/templates/mappings-table.html:1-63` — Mapping card rendering
- `proxy/web/templates/mappings.html:1-170` — Mappings page with dialog
- `proxy/web/templates/providers.html:1-90` — Providers page with dialog
- `proxy/web/templates/providers-table.html:1-50` — Provider table fragment
- `proxy/web/templates/logs.html:1-45` — Logs page with SSE
- `proxy/web/static/app.css` — Full design system
- `proxy/web/types.go:1-87` — Data types for template rendering
- `proxy/web/handlers.go:42-100` — Dashboard handler with data construction

## Open Questions

None — the refactor is straightforward HTML/CSS/template work with no backend changes.
