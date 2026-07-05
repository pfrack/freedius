---
date: 2026-07-05T12:00:00Z
researcher: MiMoCode
git_commit: $(git rev-parse --short HEAD)
branch: $(git branch --show-current)
repository: freedius
topic: "Web UI Modernization - Full Redesign"
tags: [research, web-ui, css, htmx, templates, dark-mode]
status: complete
last_updated: 2026-07-05
last_updated_by: MiMoCode
---

# Research: Web UI Modernization

**Date**: 2026-07-05
**Researcher**: MiMoCode
**Repository**: freedius

## Research Question

The user wants to modernize the freedius web dashboard from its current minimal state to a modern admin panel with dark mode, status indicators, animations, and mobile responsiveness.

## Summary

The current UI is functional but visually dated — basic CSS custom properties, no visual hierarchy, plain tables, minimal spacing. The redesign will replace the entire CSS layer and update all 8 HTML templates while preserving the Go template system, htmx integration, and SSE streaming contract.

**Key constraints discovered:**
- No custom `FuncMap` in Go templates — only built-in `| js` escaper
- Template caching via `sync.Map` — binary rebuild required for any template change
- SSE handler in `internal/eventstream/handlers.go` hardcodes `<pre class="log-{level}">` — must update Go code AND templates together
- All 15 HTML IDs have HTMX/JS consumers — must preserve them exactly
- Static assets embedded via `//go:embed` — no build tools needed

## Detailed Findings

### Current Template System

**Files involved:**

| File | Purpose |
|---|---|
| `proxy/web/templates/layout.html` | Base layout with nav, main, scripts blocks |
| `proxy/web/templates/index.html` | Dashboard page (empty) |
| `proxy/web/templates/providers.html` | Provider CRUD with dialog form |
| `proxy/web/templates/mappings.html` | Mapping CRUD with dialog form |
| `proxy/web/templates/logs.html` | Live log viewer with SSE |
| `proxy/web/templates/providers-table.html` | HTMX-swappable provider table fragment |
| `proxy/web/templates/mappings-table.html` | HTMX-swappable mapping table fragment |
| `proxy/web/templates/models-fragment.html` | Model list fragment for suggestions |
| `proxy/web/static/app.css` | All styling (226 lines) |
| `proxy/web/static/htmx.min.js` | HTMX library |
| `proxy/web/embed.go` | Template loading + caching + rendering |
| `proxy/web/handlers.go` | All HTTP handlers |
| `proxy/web/types.go` | Template data structs |

**Template caching** (`embed.go`):
- `pageTemplates sync.Map` — caches `*template.Template` per page file
- `fragmentTemplates sync.Map` — caches self-contained fragment templates
- Templates parsed from `embed.FS` on first request, never reloaded
- Each page gets layout.html + page file parsed together as one set

**Safe to change freely:**
- All CSS classes, colors, layout properties
- HTML structure within `{{define "content"}}` blocks
- All JavaScript logic and inline handlers
- Adding new CSS custom properties or media queries
- Adding new HTMX attributes (as long as existing CRUD targets preserved)

**Breaking changes (avoid):**
- Renaming `{{define "..."}}` blocks (must match filename stem)
- Removing `{{block "content" .}}`, `{{block "title" .}}`, or `{{block "scripts" .}}`
- Removing `{{template "layout" .}}` from page files
- Removing HTML IDs used by HTMX targets or JS

### Current CSS Inventory

15 CSS classes currently in use:
- Navigation: `active`
- Buttons: `btn-primary`, `btn-danger`, `btn-sm`, `btn-cancel`
- Forms: `form-error`, `form-actions`
- Logs: `log-debug`, `log-info`, `log-warn`, `log-error`
- Model fragment: `model-list`, `text-muted`

10 CSS custom properties: `--bg`, `--text`, `--nav-bg`, `--nav-active`, `--border`, `--table-hover`, `--log-debug-bg`, `--log-info-bg`, `--log-warn-bg`, `--log-error-bg`

### SSE Contract (Critical)

The log SSE endpoint at `/v1/logs` (`internal/eventstream/handlers.go`) hardcodes HTML:

```go
fmt.Sprintf(`<pre class="log-%s">%s</pre>`, html.EscapeString(levelLabel(e.Level)), html.EscapeString(e.Line))
```

This means `log-debug`, `log-info`, `log-warn`, `log-error` CSS classes and `<pre>` element wrapper are **duplicated** between templates and Go code. A redesign must update both, or refactor the SSE handler to emit JSON and have client render HTML.

### All 15 HTML IDs and Their Consumers

| ID | Element | Referenced by |
|---|---|---|
| `providers` | `<table>` | hx-target (3 CRUD handlers), editProvider JS |
| `mappings` | `<table>` | hx-target (3 CRUD handlers), editMapping JS |
| `provider-dialog` | `<dialog>` | JS showModal, hx-on::after-request close |
| `provider-form` | `<form>` | JS editProvider field manipulation |
| `provider-dialog-title` | `<h2>` | JS editProvider textContent update |
| `provider-form-error` | `<div>` | (external error display) |
| `provider-form-error-dialog` | `<div>` | (in-dialog error display) |
| `mapping-dialog` | `<dialog>` | JS editMapping showModal |
| `mapping-form` | `<form>` | JS editMapping field manipulation, provider_name access |
| `mapping-dialog-title` | `<h2>` | JS editMapping textContent update |
| `mapping-form-error` | `<div>` | (external error display) |
| `mapping-form-error-dialog` | `<div>` | (in-dialog error display) |
| `model-suggestions` | `<div>` | hx-target for model refresh, JS click delegation |
| `level-filter` | `<select>` | `<label for="level-filter">` |
| `log` | `<div>` | hx-target for filter, sse-connect target |

### HTMX Usage Patterns

- CRUD: `hx-post`/`hx-put`/`hx-delete` with `hx-target` + `hx-swap="outerHTML"`
- Log filter: `hx-get` with `hx-trigger="change"`
- SSE: `hx-ext="sse"` with `sse-connect="/v1/logs"`, `sse-swap="log"`
- Dynamic method switching: JS calls `setAttribute('hx-put', ...)` and `removeAttribute('hx-post')`
- Dialog close: `hx-on::after-request="if(event.detail.successful) this.closest('dialog').close()"`

## Design Decisions

### Color Palette (Dark Mode First)

**Root variables (dark mode — default):**
- `--bg-root`: `#09090b` (zinc-950)
- `--bg-card`: `#18181b` (zinc-900)
- `--bg-surface`: `#27272a` (zinc-800)
- `--bg-elevated`: `#27272a` (zinc-800)
- `--bg-hover`: `rgba(255,255,255,0.05)`
- `--text-primary`: `#fafafa` (zinc-50)
- `--text-secondary`: `#a1a1aa` (zinc-400)
- `--text-muted`: `#71717a` (zinc-500)
- `--border-subtle`: `rgba(255,255,255,0.06)`
- `--border-default`: `rgba(255,255,255,0.1)`
- `--border-strong`: `rgba(255,255,255,0.15)`
- `--accent`: `#6366f1` (indigo-500)
- `--accent-hover`: `#818cf8` (indigo-400)
- `--accent-subtle`: `rgba(99,102,241,0.15)`
- `--color-success`: `#22c55e` (green-500)
- `--color-warning`: `#f59e0b` (amber-500)
- `--color-error`: `#ef4444` (red-500)

**Light mode override** via `@media (prefers-color-scheme: light)`.

### Layout: Sidebar Navigation

Replace horizontal `<nav>` with fixed sidebar (240px) + scrollable main content. Mobile: hamburger toggle with slide-in overlay.

### Component Patterns

1. **Tables**: Wrap in `.table-wrap` card with rounded borders, uppercase headers, monospace for URLs/env vars
2. **Badges**: `.badge` base with `.badge--openai`, `.badge--anthropic`, `.badge--protocol` modifiers; dot variant for status
3. **Buttons**: `.btn` base with `--primary`, `--secondary`, `--danger`, `--ghost` modifiers; `--sm`/`--lg` sizes; `--loading` spinner state
4. **Forms**: `.form-group` > `.form-label` + `.form-input`/`.form-select` with focus ring and error states
5. **Dialogs**: Rounded corners, backdrop blur, slide-in animation
6. **Animations**: fade-in, slide-up, shimmer (skeleton), pulse (live dot), htmx swap transitions

## Files to Modify

| File | Changes |
|---|---|
| `proxy/web/static/app.css` | Full rewrite with design system |
| `proxy/web/templates/layout.html` | Sidebar nav + shell wrapper + hamburger |
| `proxy/web/templates/index.html` | Add stat cards + page header |
| `proxy/web/templates/providers.html` | New button/form classes |
| `proxy/web/templates/providers-table.html` | Table wrap + badge + new button classes |
| `proxy/web/templates/mappings.html` | New button/form classes |
| `proxy/web/templates/mappings-table.html` | Table wrap + new button classes |
| `proxy/web/templates/logs.html` | Restructured log lines + badge dots |
| `proxy/web/templates/models-fragment.html` | New utility classes |
| `internal/eventstream/handlers.go` | Update hardcoded log HTML classes |

## Open Questions

1. Should the SSE handler emit JSON instead of pre-rendered HTML? (Would decouple template changes from Go code, but requires client-side rendering)
2. Should we add SVG icons to nav links and buttons?
3. Dashboard (index.html) is currently empty — what stats should it show?
