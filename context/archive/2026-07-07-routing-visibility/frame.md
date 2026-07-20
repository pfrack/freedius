# Frame Brief: Routing Visibility Across the Web UI

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The entire web UI doesn't help the user understand routing logic. The
breadcrumb-chain visualization on /mappings, the isolated Providers page,
the empty Dashboard, and the lack of cross-page navigation all contribute
to routing being invisible as a concept.

## Initial Framing (preserved)

- **User's stated cause or approach**: "better breadcrumb design I want
  to rething webui maybe sth better for user more natural"
- **User's proposed direction**: Rethink the web UI, possibly a different
  visual paradigm for the breadcrumb chain.
- **Pre-dispatch narrowing**: User selected "The entire web UI" as scope
  and "Understanding routing logic" as the friction point. When asked
  which dimension to prioritize, answered "all of that a bit." When asked
  about depth, answered "Need runtime visibility too."

## Dimension Map

The observation could originate at any of these dimensions:

1. **Information architecture** — Routing info is split across Providers
   and Mappings pages with no cross-links. Users must mentally merge two
   tables to understand the full picture.
2. **Visualization paradigm** — The breadcrumb chain shows Provider/Model
   + Protocol (when set), but actual routing has ~7 decision points
   collapsed into 2 visible dimensions.
3. **Dashboard as landing page** — Zero routing context. Four generic stat
   counters. Roadmap always intended routing data here but it was deferred.
4. **Cross-page navigation** — Only one contextual cross-link exists
   (Mappings → Logs). Dashboard links to nothing. Providers and Mappings
   are isolated from each other.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Information architecture | Providers shows `MappingCount` as a number but not which mappings. Mappings shows provider names as text but not provider details (URL, protocol, behavior). No cross-links. Users must visit both pages and mentally merge. | STRONG |
| Visualization paradigm | Chain shows `Provider/Model` + conditional Protocol badge. Missing: Behavior class, effective Base URL (post-normalization via `mix.go:84-101`), family matching (`families.go:10-16` — invisible), fallback trigger conditions, timeout budget, adapter selection. 7 routing decision points collapsed into 2 visible dimensions. | STRONG |
| Dashboard | `index.html:5-42` — four stat cards (uptime, events, logs, port). Zero routing info. `indexData` struct (`types.go:13-20`) has no provider/mapping fields. Roadmap intended "live request stream, provider health, config summary" (V-02, `roadmap.md:244`) but implementation delivered only generic counters. | STRONG |
| Cross-page navigation | Only 1 contextual cross-link: Mappings route-step pills → Logs (`mappings-table.html:46,57`). Dashboard links to nothing. Providers `MappingCount` is not a link. Mappings provider names are plain text. Logs lines are plain `<pre>` text. | STRONG |

## Narrowing Signals

- User confirmed "the entire web UI" — not just the breadcrumb chain.
- User confirmed "understanding routing logic" — not just visual polish.
- User confirmed "all of that a bit" — no single dimension dominates.
- User wants runtime visibility: "Need runtime visibility too" — which
  step handled requests, error rates, fallback triggers.
- This aligns with the roadmap's original V-02 intent ("monitor live
  request stream, provider health") which was never fully implemented.

## Cross-System Convention

Routing/admin UIs typically present the routing table as the primary view
(not buried under a secondary page). Traefik, Nginx Proxy Manager, Caddy,
and similar tools make the routing rules the landing page or a prominent
top-level view, with drill-downs into upstreams/backends. The freedius UI
inverts this: the landing page (Dashboard) has no routing info, and the
routing config is split across two pages.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: routing logic is invisible
> across the entire web UI — not just the breadcrumb visualization. The
> information is scattered (Providers vs Mappings), under-surfaced
> (breadcrumb shows 2 of ~7 routing dimensions), absent from the landing
> page (Dashboard has zero routing context), and unlinked (no cross-page
> navigation between related concepts).

The initial framing ("breadcrumb design") was a symptom, not the root
cause. The breadcrumb chain itself is well-implemented (Phase 2 of
web-ui-friendliness added protocol badges, aria-labels, depth indicators,
click-through, last-responder highlighting). The problem is that the chain
sits in isolation — it's one piece of a routing puzzle that the UI never
assembles into a coherent picture.

## Confidence

- **HIGH** — strong evidence across all four dimensions; user confirmed
  all matter equally; runtime visibility requirement aligns with roadmap's
  original intent; conventional routing/admin UIs validate the reframe.

## What Changes for /10x-plan

The plan should address routing visibility holistically, not just the
breadcrumb chain. This likely means:

1. A routing-focused landing page or Dashboard section that shows the
   full routing picture in one place.
2. Cross-links between Providers ↔ Mappings ↔ Logs.
3. Richer per-step metadata in the chain (behavior, effective URL,
   family matching).
4. Runtime visibility (which step handled recent requests, error rates,
   fallback trigger history).

Scope is larger than a single plan — likely needs to be broken into
independently-shippable phases.

## References

- Source files:
  - `proxy/web/templates/mappings-table.html:14-68` (breadcrumb chain)
  - `proxy/web/templates/providers-table.html:26-51` (providers table)
  - `proxy/web/templates/index.html:5-42` (dashboard)
  - `proxy/web/templates/layout.html:20-37` (sidebar nav)
  - `proxy/web/handlers.go:193-268` (mappings handler)
  - `proxy/web/types.go:13-20` (indexData — no routing fields)
  - `proxy/mix.go:56-78` (protocol inference — invisible in UI)
  - `proxy/families.go:10-16` (family matching — invisible in UI)
  - `proxy/proxy.go:275-406` (fallback dispatch logic)
- Prior research: `context/changes/web-ui-friendliness/research.md`
- Prior plan: `context/changes/web-ui-friendliness/plan.md`
- Roadmap: `context/foundation/roadmap.md:244` (V-02 original intent)
