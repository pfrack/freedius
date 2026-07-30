# Frame Brief: Mapping-First UI Refactor

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The freedius Web UI gives providers and mappings equal visual weight. Users cannot quickly answer "what mappings do I have and how are they routed?" because provider names dominate the visual hierarchy, the sidebar puts Mappings last, and the Dashboard treats Providers as a co-equal section.

## Initial Framing (preserved)

- **User's stated cause or approach**: The UI lacks a mapping-first information architecture. Provider and mapping are treated as peer entities when mappings should be primary.
- **User's proposed direction**: Redesign all four screens (Dashboard, Mappings, Providers, Logs) to make mapping the dominant entity and provider a subordinate support detail.
- **Pre-dispatch narrowing**: The observation is unified — it's a systemic hierarchy inversion across all screens, not a single-screen issue.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Navigation hierarchy** — Sidebar order puts Mappings last → users find it last
2. **Dashboard composition** — Equal sections for Providers and Mappings → neither dominates
3. **Visual weight in mapping cards** — "ProviderName / Model" is the strongest text → provider is the identity
4. **Missing mapping-centric metrics** — No stats about mapping health → can't assess system at a glance
5. **Information density** — Cards too sparse, wasted space → scanning is slow

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Navigation hierarchy (sidebar order) | layout.html:30-55 — Mappings is literally the last item | STRONG |
| Dashboard composition (equal sections) | index.html — two `dashboard-section` elements with equal `h2` headings | STRONG |
| Visual weight in cards (provider dominant) | mappings-table.html — `{{.ProviderName}} / {{.Model}}` is the route-step content | STRONG |
| Missing mapping metrics (no overview) | index.html — only Uptime + Host:Port in stats-strip | STRONG |
| Information density (sparse cards) | app.css — route-card uses space-4 padding, flexbox with gaps; no compact mode | MODERATE |

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: The entire UI information architecture treats providers as co-equal to mappings, when mappings should be the primary operational entity at every level (navigation, dashboard, card rendering, metrics).

The initial framing was correct — this is a systemic hierarchy inversion. All five dimensions show strong evidence. The fix is not a tweak to one screen; it requires a coordinated redesign of navigation order, dashboard composition, card visual hierarchy, stats presentation, and information density across all four screens.

## Confidence

- **HIGH** — strong evidence at every dimension + clear user requirement + all data already available in templates (no backend changes)

## What Changes for /10x-plan

The plan should define a mapping-first UI system with: (1) reordered sidebar, (2) dashboard with mapping-centric stats grid + compact mapping summary + demoted provider section, (3) redesigned mapping cards where model/route is the dominant visual and provider is metadata, (4) improved logs page with mapping/provider filters exposed, (5) providers page slightly demoted but functional. All changes are HTML template + CSS only.

---

## Mapping-First UI System Definition

### Page Shell

```
┌─────────────────────────────────────────────────────────┐
│ [Sidebar 240px]  │  [Main content area]                 │
│                  │  ┌─ Page Header ──────────────────┐  │
│  ☰ freedius      │  │ h1 + primary action button     │  │
│                  │  └────────────────────────────────┘  │
│  ■ Dashboard     │  ┌─ Content ─────────────────────┐  │
│  ■ Mappings      │  │ (page-specific content)        │  │
│  ■ Providers     │  │                                │  │
│  ■ Logs          │  └────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Sidebar Structure (new order)

1. Dashboard (overview/home)
2. **Mappings** (primary operational page — moved to position 2)
3. Providers (secondary infrastructure)
4. Logs (observability)

### Page Title Conventions

- `h1`: Page name (Dashboard, Mappings, Providers, Logs)
- Primary action button immediately after h1 (e.g., "Add Mapping")
- No subtitle or description text in the header area

### Section Header Pattern

```html
<section class="page-section">
  <div class="section-header">
    <h2>Section Title</h2>
    <span class="section-count">N items</span>
  </div>
  <!-- content -->
</section>
```

### Mapping Card/Row Pattern (redesigned)

```
┌────────────────────────────────────────────────────────┐
│ [●] mapping-name          [family]  [N fallbacks] [⋯] │
│     model-name                                         │
│     via provider-name  •  protocol                     │
│                                                        │
│  Primary ──→ Fallback 1 ──→ Fallback 2                 │
│  model        model          model                     │
│  ᵥᵢₐ prov    ᵥᵢₐ prov      ᵥᵢₐ prov                  │
└────────────────────────────────────────────────────────┘
```

Key changes:
- **Mapping name** is the strongest visual (largest, boldest)
- **Model** is the second line (what it routes to)
- **Provider** is third line, smaller text, prefixed with "via"
- **Status indicator** has a text label (not just a dot)
- **Actions** are in a `⋯` menu or inline but visually subordinate

### Provider Metadata Treatment

Across all screens, provider appears as:
- Smaller font-size (0.75rem)
- Muted color (`var(--text-muted)`)
- Prefixed with "via" for context
- Never the first or largest text in a card

### Status / Badge / Chip System

| Badge | Use | Style |
|-------|-----|-------|
| `badge--family` | Model family (gpt, claude, etc.) | Accent subtle |
| `badge--protocol` | Protocol (openai, anthropic) | Accent subtle, uppercase |
| `badge--status-ok` | API key present | Green bg, small |
| `badge--status-warn` | API key missing | Yellow bg, small |
| `badge--fallback` | Fallback count | Border only, muted |

### Action Hierarchy

1. **Primary action** (page-level): "Add Mapping" / "Add Provider" — `.btn--primary`
2. **Row action** (edit): `.btn--ghost btn--sm` — visible on hover or always
3. **Destructive action** (delete): `.btn--ghost btn--sm` with `color: var(--color-error)` — NOT `.btn--danger` (too loud)

### Desktop / Tablet / Mobile Responsive Rules

| Breakpoint | Behavior |
|-----------|----------|
| Desktop (>1024px) | Full sidebar + compact card list, all metadata visible |
| Tablet (768-1024px) | Sidebar visible, hide BaseURL in provider metadata, compress padding |
| Mobile (<768px) | Sidebar collapses (hamburger), cards stack vertically, hide protocol badges, provider metadata wraps below |

### Empty / Loading / Error States

- **Empty**: Centered illustration-free message + CTA button (existing pattern is good)
- **Loading**: HTMX indicator (existing `htmx-indicator` pattern) — no additional loading states needed
- **Error**: Inline `.form-error` for dialogs, toast for mutations (existing pattern)

### Dashboard Stats Grid

```
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│ Mappings │ │ Active   │ │ Fallback │ │ Providers│
│    12    │ │    10    │ │     8    │ │     4    │
└──────────┘ └──────────┘ └──────────┘ └──────────┘
```

Stats to show:
- Total mappings
- Active mappings (API key present)
- Mappings with fallbacks
- Providers connected

Below stats: compact "Recent Mappings" (last 5) using mapping-first card pattern, then a demoted "Providers" row (inline badges, not a full section).

## References

- Source files: `proxy/web/templates/`, `proxy/web/static/app.css`, `proxy/web/types.go`
- Related research: `context/changes/mapping-first-ui-refactor/research.md`
