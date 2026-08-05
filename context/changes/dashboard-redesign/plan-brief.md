# Dashboard GUI Redesign — Plan Brief

> Full plan: `context/changes/dashboard-redesign/plan.md`

## What & Why

Redesign the Freedius web dashboard from a configuration-heavy card UI (with inline Edit/Delete) into an operator-friendly monitoring dashboard. The current UI duplicates the Mappings page and doesn't surface routing health, failure rates, or attention-worthy issues. The goal: answer 5 key operator questions in under 10 seconds without navigating away from the dashboard.

## Starting Point

The web UI is Go `html/template` + HTMX + vanilla JS with a sidebar navigation (Dashboard, Mappings, Providers, Logs). The dashboard currently shows stat cards, large route-cards (same template as Mappings page), and provider chips. The EventBus stores raw request events in a 10k ring buffer but has no aggregation layer. No provider health checking exists.

## Desired End State

The dashboard shows a health strip (router state, uptime, error/fallback counts), a conditional attention panel (alerts for failing providers, missing keys), a compact routing table (one row per mapping with inline stats), provider-health badges, and a live SSE activity feed. Clicking a mapping opens a read-only detail drawer. Configuration actions (Edit/Delete) live only on the Mappings page, which uses a compact table with filters and row-action menus. All status indicators use icon + text, are keyboard-navigable, and deep-link to filtered Logs.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
|----------|--------|-------------------|
| Telemetry aggregation | In-memory StatsCollector (EventBus subscriber) | Mirrors existing EventBus/LogSink pattern; stats reset on restart is acceptable for a local proxy. |
| Provider health | Passive (derived from traffic) + manual "Test Connection" button | Avoids rate-limiting free-tier providers; reuses existing models-refresh endpoint pattern. |
| Attention panel logic | Rule-based thresholds computed on render | No alert storage needed; deterministic and testable; rules visible in code. |
| Activity feed | Server-rendered snapshot + SSE live updates | Matches existing Logs page pattern; instant initial render plus live tail. |
| Mapping detail view | Slide-in drawer (HTMX-loaded fragment) | User stays on dashboard; progressive disclosure without page navigation. |
| Routing table format | Compact table with inline primary + first fallback, "+N more" indicator | Preserves routing-at-a-glance without the space cost of cards. |
| Provider summary | Counts strip + status-colored badges | Glanceable health for all providers; fits "10-second answer" goal. |
| Attention panel visibility | Conditional render — hidden when no issues | Clean dashboard when healthy; absence of panel IS the good-news signal. |
| Template migration | Clean break — delete old, create new in one PR | Solo dev, no external consumers, current UI is 1 day old. |
| Zero-traffic state | Explicit "awaiting first request" indicators | Honest UX; user knows router is ready but idle. |
| Testing strategy | Go handler tests + template assertions + Playwright E2E (3-5 tests) | Go tests for bulk coverage; Playwright for SSE/drawer/HTMX interactions. |
| Log deep-links | Query params extending existing ?min=&provider=&mapping= pattern | Zero new infrastructure; bookmark-friendly; HTMX already syncs URL. |

## Scope

**In scope:**
- New dashboard layout (health strip, attention panel, routing table, provider badges, activity feed)
- Mapping detail drawer component
- Mappings page table refactor (cards → table + filters + row actions)
- Providers page enhancement (status, last-error, test button)
- Logs page new filters (?outcome=, ?fallback=) and visual distinction
- StatsCollector backend (per-mapping/per-provider counters)
- CSS/accessibility polish (keyboard nav, focus management, status badges)
- Playwright E2E test suite (3-5 tests)

**Out of scope:**
- Background active health checks (no provider polling)
- Persistent telemetry (no SQLite/files)
- Multi-user auth / RBAC
- Mobile-first layout
- TUI changes
- New JS dependencies

## Architecture / Approach

New `proxy.StatsCollector` subscribes to EventBus → maintains per-mapping/per-provider counters in memory (sync.RWMutex + maps). Web handlers read snapshots on render. Dashboard template consumes enriched data types (`dashboardData`, `attentionAlert`, `routingTableRow`, etc.). Drawer loaded via HTMX fragment endpoint. All pages use query-param deep-links for cross-page navigation. No new Go dependencies.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|------------------|----------|
| 1. StatsCollector | Backend telemetry aggregation | Concurrent access correctness |
| 2. Dashboard Redesign | New dashboard UI (health, table, badges, feed) | Largest change; template/handler rewrite |
| 3. Mapping Drawer | Slide-in detail component | Focus management and accessibility |
| 4. Mappings Table | Compact table + filters + modals on config page | Existing Edit/Delete dialog compatibility |
| 5. Providers Enhancement | Status badges, test button, expandable details | Passive health derivation accuracy |
| 6. Logs Deep-Links | New filters + cross-page navigation | Filter heuristics for unstructured log lines |
| 7. CSS & Accessibility | Polish, keyboard nav, responsive | Drawer animation + focus trap interactions |
| 8. Playwright E2E | Browser-level verification | CI setup + test server lifecycle |

**Prerequisites:** None — builds on existing codebase. Branch `dashboard-redesign` created.
**Estimated effort:** ~4-6 sessions across 8 phases.

## Open Risks & Assumptions

- Log filtering for `?outcome=` and `?fallback=` relies on substring matching in pre-rendered slog text lines — may produce false positives/negatives if log format changes.
- StatsCollector stats reset on process restart — assumption is this is acceptable for a local dev tool.
- Passive provider health can't detect a down provider until traffic fails through it — acceptable given the "no background polling" decision.
- Playwright E2E requires Node.js in CI — GitHub Actions has it by default but adds ~30s to CI.

## Success Criteria (Summary)

- Dashboard answers the 5 key operator questions within 10 seconds of page load (health, routing, fallback, attention, config location)
- All Go tests pass (`mage test`), lint clean (`mage lint`)
- Playwright E2E suite green (SSE, drawer, attention, deep-links)
