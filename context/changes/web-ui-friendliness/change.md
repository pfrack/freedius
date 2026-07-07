---
id: web-ui-friendliness
title: Web UI friendliness improvements (breadcrumbs polish + global UX gaps)
status: implemented
created: 2026-07-07
updated: 2026-07-07
---

# Change: Web UI friendliness improvements

Targeted follow-up to V-02d (mapping-graph-visualization, PR #30, commit d6f1930).

Scope:

- Polish the breadcrumb-chain `/mappings` UI (12 scoped proposals).
- Close the 4 PENDING impl-review findings (F1, F2, F3, F4).
- Fix the 5 highest-impact general UX gaps surfaced by codebase scan:
  silent CRUD error feedback, broken hx-confirm copy, missing loading indicators,
  missing empty states, missing client-side search/filter.
- Preserve all hard constraints (HTMX-only, design tokens, BEM, hx-target="#mappings" contract).
