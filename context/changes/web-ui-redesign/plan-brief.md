# Web UI Modernization — Plan Brief

> Full plan: `context/changes/web-ui-redesign/plan.md`
> Research: `context/changes/web-ui-redesign/research.md`

## What & Why

The freedius web dashboard is functional but visually dated — plain white/blue theme, horizontal nav bar, no visual hierarchy, empty dashboard page. This redesign transforms it into a modern dark-mode-first admin panel with sidebar navigation, SVG icons, badge system, subtle animations, and mobile responsiveness.

## Starting Point

226-line CSS file with light-mode-first custom properties, 22-line horizontal nav layout, plain tables, and an empty dashboard. The SSE handler hardcodes `<pre class="log-{level}">` HTML in Go code, tightly coupling backend to CSS class names.

## Desired End State

A zinc-toned dark-mode-first admin panel with: fixed 240px sidebar (hamburger on mobile), inline SVG icons on nav links and action buttons, badge system for protocols, card-based dashboard showing proxy stats (uptime, events, logs, port), subtle CSS transitions, and SSE handler emitting JSON with client-side rendering. All existing CRUD and HTMX functionality works unchanged.

## Key Decisions Made

| Decision | Choice | Why |
|---|---|---|
| SSE handler approach | Emit JSON, render client-side | Decouples Go code from CSS class names — CSS changes never touch Go |
| Navigation icons | Inline SVG icons | No external deps, crisp at any size, dark-mode safe via currentColor |
| Dashboard content | Proxy status cards | Immediate value using existing `/v1/stats` endpoint |
| Animation level | Subtle transitions | Polished feel without heavy motion or accessibility concerns |
| Mobile nav | Hamburger overlay | Standard pattern, sidebar slides in on tap |
| Phasing | 3 phases | Each phase independently testable and shippable |

## Scope

**In scope:**
- Full CSS rewrite with dark-mode-first design system
- Sidebar navigation with SVG icons
- All 8 HTML template updates
- SSE handler refactoring (JSON emission)
- Client-side log renderer
- Dashboard status cards
- Subtle CSS transitions/animations
- Mobile responsive layout

**Out of scope:**
- New backend features or API endpoints
- JavaScript framework (vanilla JS + HTMX only)
- CSS preprocessor or build tools
- Accessibility audit
- i18n or localization

## Architecture / Approach

Three incremental phases: (1) CSS design system + layout shell, (2) page templates + SSE refactor, (3) dashboard + polish. Each phase is independently shippable. Phase 1 modifies only CSS and layout.html — all existing pages continue working. Phase 2 touches all templates plus one Go file (SSE handler). Phase 3 is CSS-only polish.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. CSS Design System + Layout Shell | Complete new CSS, sidebar layout, hamburger mobile | Old template classes must still work alongside new ones |
| 2. Page Templates + SSE Refactor | Updated templates, JSON SSE, client log renderer, dashboard data | Go + template changes in same phase — highest risk |
| 3. Dashboard + Polish | Transitions, animations, visual refinements | Low risk — CSS-only changes |

**Prerequisites:** `mage build`, `mage test`, `mage lint` all pass on current main
**Estimated effort:** ~2-3 sessions across 3 phases

## Open Risks & Assumptions

- The SSE JSON refactoring changes the wire format — any external SSE clients would break (but the only consumer is the web UI)
- Template caching means CSS/HTML changes require binary rebuild — no live reload during development
- The `indexData` struct extension requires access to `eventstream.Handlers` fields from the page handler — already available in `SetupMux`

## Success Criteria (Summary)

- All pages render with new dark-mode design, sidebar nav works on desktop and mobile
- Dashboard shows live proxy stats (uptime, events, logs, port)
- All CRUD operations (create/edit/delete providers and mappings) work unchanged
- Live SSE log streaming works with JSON emission and client-side rendering
- `mage build && mage test && mage lint` all pass
