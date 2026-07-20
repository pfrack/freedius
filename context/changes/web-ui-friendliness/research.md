---
date: 2026-07-07T17:56:04Z
researcher: opencode/10x-research
git_commit: d6f1930a8fc38c08c0f4c02cf493ee07125dd428
branch: main
repository: pfrack/freedius
topic: "Better breadcrumbs and UI friendliness improvements across the freedius web UI"
tags: [research, web-ui, ux, breadcrumbs, mappings, htmx, accessibility]
status: complete
last_updated: 2026-07-07
last_updated_by: opencode/10x-research
---

# Research: Better breadcrumbs and UI friendliness improvements

**Date**: 2026-07-07T17:56:04Z
**Researcher**: opencode/10x-research
**Git Commit**: [`d6f1930`](https://github.com/pfrack/freedius/commit/d6f1930a8fc38c08c0f4c02cf493ee07125dd428) (`d6f1930a8fc38c08c0f4c02cf493ee07125dd428`)
**Branch**: main
**Repository**: pfrack/freedius

> **GitHub permalinks** below use the commit SHA prefix `d6f1930a`. Click any `file.ext:line` link to open at the exact line in the upstream `pfrack/freedius` repo.

## Research Question

> "better breadcrumbs sth beter from ui user friendlines"

The user requested a polish of the breadcrumb-chain mapping visualization shipped in V-02d, plus broader UI friendliness improvements across all web UI pages. The query is interpretive: span the breadcrumb visual upgrade, the open PENDING findings from the V-02d impl-review, and the cross-page UX gaps surfaced by a fresh scan.

## Summary

The breadcrumb-chain mapping cards on [`/mappings`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html) **already exist** (PR #30, `d6f1930`). They replaced a flat `<table>` with role-colored steps — primary green / fallback amber — but the implementation is currently a *minimal first cut*: identical pill content, color-only role signal, no protocol disclosure per step, no metadata-driven affordances (latency, last responder, health), and four open PENDING findings from the impl-review that block sign-off.

Beyond the breadcrumb page, a fresh codebase scan surfaced **30+ concrete UX friction points** across all 4 pages (Dashboard, Logs, Providers, Mappings) that the user would experience immediately. The biggest are: **silent CRUD failures** (form errors never reach the user), **broken `hx-confirm` copy** (literal `Delete mapping 'foo?` with no closing quote, visible to every user), **zero loading indicators**, **zero success feedback**, **zero empty-state copy**, and **no client-side search on providers/mappings/logs**.

The codebase has a mature design system (zinc dark palette, indigo accent, BEM-ish `.btn--*`/`.badge--*`/`.card`/`.form-*` classes, embedded HTMX, two fixed ports) and clear hard constraints from V-02 (no JS framework, no CSS preprocessor, no SVG graph view, no a11y audit). Every improvement below stays inside those rails and reuses the existing tokens (`--color-success`, `--color-warning`, `--bg-card`, `--space-*`, `--radius-*`, etc.) — never introduces new hex values.

**TL;DR for `/10x-plan`**:

1. **Close the 4 PENDING impl-review findings** — XS/S effort, all already verified still present (F1 `data-fallbacks` double-escape, F2 unbounded model-list DOM, F3 unbalanced confirm-quote on both pages, F4 in-flight fetch banner). These are the highest signal-to-noise first PR.
2. **Wire silent-error feedback** to the empty `#*-form-error` divs that already exist in [`providers.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L9) / [`mappings.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L9) — JS already gets `event.detail.xhr.response` for free; one listener per dialog closes the worst UX bug in the codebase.
3. **Polish the breadcrumb itself** — add a per-step protocol badge, an aria-label/visually-hidden role label (color is not enough for WCAG 1.4.1), a fallback-depth count in the card header, and a click-through to `/logs?provider=…&mapping=…`. Optionally — only if a backend mutation budget is approved — surface the "last responder" step via an SSE-driven highlight.
4. **Add empty-state copy** to all 3 list pages (Providers, Mappings, Logs) and a **loading indicator** (`htmx-indicator` span + `hx-disabled-elt` on save buttons) to all write flows.
5. **Defer** any client-side search/filter, dashboard drill-downs, dashboard live-feed, theme toggle, and accessibility audit (already V-02b out-of-scope) until after the above ships.

## Detailed Findings

### A. The breadcrumb-chain cards — already shipped, but minimal

The current implementation lives in 4 files. Render path (direct visit):

1. Route registered at [`handlers.go:60-62`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L60).
2. Handler [`handleMappings`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L193) snaps `cfg.MappingsSnapshot()` + `cfg.ProvidersSnapshot()`, builds `[]mappingRow` and `[]providerRow`, and calls `renderPage(..., "mappings-table.html")`.
3. [`renderPage`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L96) loads the page template via [`loadPageTemplate`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L64), which parses `templates/{layout.html, mappings.html, mappings-table.html}` together.
4. The layout defines `{{block "content"}}`; [`mappings.html:3`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L3) overrides it and calls `{{template "mappings-table" .}}` at [:10](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L10).
5. The fragment template [`mappings-table.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html) renders `<div id="mappings" class="mappings-grid">` containing one `.route-card` per mapping.

HTMX path: each `<form>` and Delete `<button>` carries `hx-target="#mappings" hx-swap="outerHTML"` ([`mappings.html:16-17`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L16), [`mappings-table.html:22-23`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L22)). The dialog markup (lines 12-62) and inline `<script>` live OUTSIDE `#mappings`, so card re-renders never unmount them.

What each `.route-step` renders today ([`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30)):

```html
<div class="route-step route-step--primary">
  {{.ProviderName}} / {{.Model}}
</div>
{{range .Fallbacks}}
<div class="route-step route-step--fallback">
  {{.ProviderName}} / {{.Model}}
</div>
{{end}}
```

**What is missing or wrong**:

| Gap | file:line | Detail |
| --- | --- | --- |
| No protocol per step | [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30) | `fallbackEntry` ([`types.go:57-60`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L57)) carries only `ProviderName` + `Model`. The provider's `Protocol` (openai/anthropic/auto) is never copied into the card. The whole point of the breadcrumb — chain visibility — disappears the moment an Anthropic request routes to a Zen/Opencode-Go endpoint with `protocol: openai`. |
| Color-only role signal | [`app.css:800-806`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L800) | The step text never says "Primary" / "Fallback 1", "Fallback 2". WCAG 1.4.1 (Use of Color) violation. Deuteranopes cannot distinguish. |
| Leading "▶" arrow appears after the *first* step | [`app.css:808-816`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L808) | Only `.route-step:last-child::after { display: none }` is hidden; the **first** step also has a chevron with nothing preceding it (reads as "this came from somewhere"). |
| No fallback depth in the card header | [`mappings-table.html:5-7`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L5) | A mapping with 0 fallbacks looks identical to one with 5 — only the chevron spacing differs. |
| No `aria-label` / role text | [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30) | Whole `.route-card` has no accessible name; nested `.route-step`s are pure `<div>`s. |
| Click does nothing | [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30) | A step pill is a `<div>`, not a link. Users who want "why did the fallback fire?" must scroll the full log manually. |
| `FallbacksString()` and `fallbackEntry.String()` are dead code | [`types.go:63-65, 76-82`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L63) | `grep FallbacksString` shows zero callers outside the definition site; both methods can be deleted in Phase 1 cleanup. |

### B. PENDING impl-review findings — all 4 still present

Source: [`context/changes/mapping-graph-visualization/reviews/impl-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/impl-review.md). Re-verified against the current commit `d6f1930`; all 4 are still in the code untouched.

| ID | Severity | Status | Verification file:line | Minimal fix |
| --- | --- | --- | --- | --- |
| **F1** | WARNING | UNFIXED | [`mappings-table.html:14`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L14) | Drop `\| js` from `data-fallbacks` AND switch to double quotes: `data-fallbacks="{{.Fallbacks | jsonMarshal}}"`. Then convert `addFallbackRow` ([`mappings.html:108-138`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L108)) from `innerHTML` string-concat to `createElement` + `.value` assignment. Confidence HIGH (Go html/template docs confirm context-aware attribute escaping). |
| **F2** | WARNING | UNFIXED | [`handlers.go:697-717`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L697), [`models-fragment.html:7-9`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/models-fragment.html#L7) | Truncate in handler: `const modelCap = 1000; if len(models) > modelCap { models = models[:modelCap]; data.Truncated = true }`. Add `Truncated bool` to [`modelsData`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L91). Switch template to `{{if .Truncated}}` — drop the misleading "Truncated at 1000 models" message that currently appears even when full list is rendered. |
| **F3** | OBSERVATION | UNFIXED | [`mappings-table.html:21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L21), [`providers-table.html:34`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers-table.html#L34) | One-character fix. Change `Delete mapping '{{.Name}}?` → `Delete mapping '{{.Name}}'?`; same on providers. |
| **F4** | OBSERVATION | UNFIXED | [`handlers.go:687-693`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L687) | Add `FetchInProgress bool` to `modelsData`. Set true on the `TryLock()`-failed branch. Render `<small class="text-muted">Refresh already in progress…</small>` in [`models-fragment.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/models-fragment.html) when set. |

Plus, an additional bug surface found by the deep-dive agent that wasn't in the impl-review:

| Bonus | Severity | Status | Verification file:line | Minimal fix |
| --- | --- | --- | --- | --- |
| **Stale `hx-put` after Edit → Add cycle** | MEDIUM | UNFIXED | [`mappings.html:65-89`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L65) | `editMapping()` sets `hx-put` and removes `hx-post`, but the `Add Mapping` button ([line 5](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L5)) only calls `showModal()` — no reset path. After Edit → Cancel → Add → Save, the form POSTs to `PUT /v1/mappings/<last-edited-name>` with `name=` of the new (empty) input, silently failing. Add a `openAddMapping()` helper that always re-sets the form to POST mode before `showModal()`. Same shape as `editMapping()`. |

### C. Broader UX gaps surfaced by codebase scan

30+ specific friction points across the 4 pages. The most user-visible (must-fix) cluster is below; minor items moved to "Open Questions" at the end.

#### C.1 — Silent failure on every CRUD error (P1, affects all 4 pages)

**Symptom**: Enter an invalid base URL on the Provider dialog, click Save → dialog stays open with **no message visible anywhere on the page**.

**Why**: [`writeJSONError`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L756) and [`writeValidationError`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/forms.go#L25) return structured JSON. HTMX 2.0's default `responseHandling` ([htmx.min.js:8](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/htmx.min.js)) drops error responses and does **not swap**. The empty `#provider-form-error` ([`providers.html:9,21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L9)) and `#mapping-form-error` ([`mappings.html:9,21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L9)) `<div class="form-error">` slots were scaffolded for exactly this purpose and are never written to.

**Fix sketch**: Add a global `htmx:afterRequest` listener that, on `!event.detail.successful`, parses `event.detail.xhr.response`, populates the nearest `#*-form-error-dialog` (or shows a toast for non-form swaps). Reuse the existing IDs — zero template restructuring required.

#### C.2 — `hx-confirm` strings are grammatically broken (P1, users see every delete)

[`providers-table.html:34`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers-table.html#L34):

```
hx-confirm="Delete provider '{{.Name}}?"
```

Renders literally as `Delete provider 'openai?` — unterminated open-quote. Same bug at [`mappings-table.html:21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L21). This is also **impl-review F3** repeated in the UX-broader scan — fix as one-line edit.

#### C.3 — No loading indicator on any write (P1)

No `htmx-indicator` span, no `aria-busy`, no submit-button disable. Provider save at [`providers.html:51`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L51), mapping save at [`mappings.html:56`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L56), every delete button — all give zero feedback during the (occasionally slow) `cfg.SaveData` cycle.

#### C.4 — No success toast/notification (P1)

Only feedback is dialog auto-close (`hx-on::after-request="if(event.detail.successful) this.closest('dialog').close()"` at [`providers.html:18`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L18), same at [`mappings.html:18`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L18)). On a successful Edit, the table re-renders but the user has no way to confirm *what* they just saved. Suggested: `<div id="toast-region" aria-live="polite">` in [`layout.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L46); a `showToast()` helper called from a global `htmx:afterRequest` listener.

#### C.5 — Empty-state handling is entirely missing (P1)

- `providers-table.html:15-43` renders empty `<tbody></tbody>` with **no onboarding hint**. The only "Add" button lives at the top of [`providers.html:5`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L5), disconnected from the empty grid.
- `mappings-table.html:1-41` renders empty `<div class="mappings-grid"></div>` when there are 0 mappings — invisible empty space.
- `logs.html:21-29` renders empty `<div id="log">` — no "Waiting for log events…" copy.
- `models-fragment.html:19` has `<small class="text-muted">No models fetched yet.</small>` — the *only* template with an empty-state branch.

#### C.6 — Logs filter has no "active" affordance and resets on reload (P2)

[`logs.html:5-20`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L5) renders a `<select id="level-filter">` driven by HTMX `hx-get="/logs" hx-target="#log" hx-trigger="change"`. There is:

- no `hx-push-url="true"`, so reload → filter gone;
- no chip/caption near the heading showing `Showing errors+ only × clear`;
- no client-side filter on the providers or mappings tables at all (zero `type="search"` inputs anywhere in `proxy/web/templates/`).

Plus a quiet bug: the **server-side** `?min=` filter ([`handlers.go:107-118`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L107)) is applied to the initial server-rendered batch only; incoming SSE events appended at [`logs.html:31-52`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L31) compare nothing — the `<select>` value is read but `e.level` is never filtered against it. User on `/logs?min=error` sees all SSE messages, not just errors.

#### C.7 — Dashboard is bare and read-only (P2)

[`index.html:5-42`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/index.html#L5) renders 4 non-interactive stat cards (`Uptime`, `Total Events`, `Total Logs`, `Listening On`) and a tagline. Compare-card drill-downs (Total Logs → /logs, etc.) would be the obvious affordance. Stats go stale on every reload — there is no SSE-driven live update. The "Live proxy monitoring and configuration" subtitle ([`index.html:43`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/index.html#L43)) promises "configuration" but the page itself has zero configuration links.

#### C.8 — Inline styles bypass the design system (P2)

[`mappings.html:47`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L47):

```html
<fieldset id="fallback-fieldset"
  style="border:1px solid #555;border-radius:4px;padding:8px;margin-top:8px;">
  <legend style="font-size:0.85em;color:#aaa;">Fallback Chain</legend>
```

Hard-codes `#555` and `#aaa` literal hex that contradict the dark zinc palette. The global reset at [`app.css:580-584`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L580) is then overridden inline. Same pattern in each generated fallback row at [`mappings.html:113-125`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L113). Replaces `--space-*`, `--radius-*`, `--border-*`, `--text-muted` with non-tokens.

#### C.9 — Cancel button duplicates `.btn--secondary` (P3)

`.btn-cancel` ([`app.css:473-490`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L473)) is a one-off parallel to `.btn .btn--secondary` ([`app.css:434-442`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L434)). Used by [`providers.html:52`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L52) and [`mappings.html:57`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L57). No documented reason for the divergence. Plus the Cancel `onclick` does **not** call `form.reset()` — closing after a partial edit leaks form state into the next Add cycle (related to the `hx-put` stale bug in §B).

#### C.10 — A11y gaps (P3, mixed priority)

- Icon-only buttons without `aria-label`: hamburger ([`layout.html:10`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L10), has it ✓), `+ Add provider` / `+ Add mapping` (text + icon ✓), fallback-row remove `×` ([`mappings.html:123`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L123)) — **no `aria-label`**, screen-readers say "times".
- Per-row "Fetch models" button at [`mappings.html:120-122`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L120) — icon-only, no aria-label.
- Color-only role distinction for primary/fallback — full WCAG 1.4.1 violation (see §A).
- No `<dialog>` `aria-labelledby` linking to the dialog title — `dialog[aria-labelledby="mapping-dialog-title"]` is the standard fix at [`mappings.html:12`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L12), same for provider.
- `name.readOnly = true` set at [`mappings.html:72`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L72) — no `aria-readonly="true"`, no visual cue.
- Required-marker inconsistency (some fields have `required`, others depend on server validation only) — mixed strategy at [`providers.html:23-49`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L23), [`mappings.html:23-44`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L23).

(Accessibility audit is explicitly **out of scope** for V-02b per [`roadmap.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/roadmap.md); revisit only if user demand emerges — but the WCAG 1.4.1 color-only fix is well-scoped and should ride along with the breadcrumb improvements.)

#### C.11 — Decorative chevron appears before the first step (P3, sub-fix inside C.10)

[`app.css:808-816`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L808):

```css
.route-step::after           { content: "▶"; ... }
.route-step:last-child::after { display: none; }
```

Only `:last-child` is targeted — `.route-step:first-child::after` is not hidden, so the *primary* step shows a chevron with nothing preceding it. Reads as "this came from somewhere" and breaks the visual continuity. Pair this fix with the breadcrumb proposals in §E.

#### C.12 — `route-step--fallback` palette respects dark/light via existing tokens; no new colors needed

Verified: every existing CSS rule for `.route-*` uses the dark zinc palette ([`app.css:7-70`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L7)) and the `@media (prefers-color-scheme: light)` overrides ([`app.css:74-107`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L74)). No new color tokens should be introduced for the polish work; use `--accent` for active highlight, `--color-error` for toast, `--text-muted` for depth badge. Confirmed by [`app.css:800-806`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L800).

### D. Concrete, well-scoped improvement proposals (12)

Each proposal is grounded in a specific file:line and respects the constraints in §F. Effort: XS/S/M/L. Risk: low/medium/high.

#### 1. Provider-protocol badge on each step pill — reuse existing `.badge--*` — Effort S — Risk low

**Why**: Right now a user cannot tell at a glance that their "anthropic-primary" mapping may dispatch through a Zen endpoint with `protocol: openai` (per `proxy/mix.go`). The protocol badge makes the inferred routing visible at the point of configuration.

**Where**:
- Add `Protocol string` to [`fallbackEntry`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L57) and [`mappingRow`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L67).
- Populate in handlers at [`handlers.go:196-211`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L196) and [`handlers.go:308-323`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L308) by looking up `providers[p].Protocol`.
- Render at [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30):
  ```html
  <div class="route-step route-step--primary">
    {{if .Protocol}}<span class="badge badge--protocol route-step__protocol">{{.Protocol}}</span>{{end}}
    <span class="route-step__name">{{.ProviderName}} / {{.Model}}</span>
  </div>
  ```

#### 2. Fallback depth indicator in card header — Effort XS — Risk low

**Where**: [`mappings-table.html:5-7`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L5). No Go change. Sketch:

```html
<h3 class="route-card__name">{{.Name}}</h3>
{{if .Fallbacks}}
  <span class="route-card__depth">{{len .Fallbacks}} fallback{{if ne (len .Fallbacks) 1}}s{{end}}</span>
{{end}}
```

CSS at the end of the `.route-card*` block in [`app.css`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L758) — reuses `--text-muted`, `--border-subtle`, `--radius-sm`, `--space-*`.

#### 3. Aria-label + visually-hidden role text on each step — Effort XS — Risk low

**Why**: WCAG 1.4.1 (Use of Color). Encodes "Primary" / "Fallback N" as text alongside the green/amber border.

**Sketch**:

```html
<div class="route-step route-step--primary"
     role="listitem"
     aria-label="Primary provider {{.ProviderName}} model {{.Model}}">
  <span class="visually-hidden">Primary: </span>
  {{.ProviderName}} / {{.Model}}
</div>
{{range $i, $fb := .Fallbacks}}
<div class="route-step route-step--fallback"
     role="listitem"
     aria-label="Fallback {{add 1 $i}} provider {{$fb.ProviderName}} model {{$fb.Model}}">
  <span class="visually-hidden">Fallback {{add 1 $i}}: </span>
  {{$fb.ProviderName}} / {{$fb.Model}}
</div>
{{end}}
```

Add the canonical `.visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0,0,0,0); }` to [`app.css`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css) near the existing `.text-muted` utility.

#### 4. Per-step click → `/logs?provider=…&mapping=…` filtered log view — Effort M — Risk low

**Why**: Operators want "why did my fallback fire?". Today they have to scroll the full log. With one new query-param branch in `handleLogs` ([`handlers.go:96-155`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L96)) and turning each `.route-step` into `<a href="/logs?provider=...&mapping=...">`, the breadcrumb becomes a navigation surface.

**Constraint**: The empty `<div class="route-step">` becomes `<a class="route-step">`. Reuses existing CSS classes unchanged. Browser default `:focus` ring on the new `<a>`s satisfies the a11y actionability gap surfaced in §C.10.

#### 5. Hover tooltip with provider URL + protocol — Effort S — Risk low

**Where**: Pass `BaseURL` through `mappingRow` ([`types.go:67-72`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L67)) at handlers [`handlers.go:196-211`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L196); set `title` attribute on each step pill in [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30). Zero CSS change.

#### 6. Last-responder step highlight (SSE-driven, opt-in) — Effort M — Risk medium

**Why**: A chevron animation marks the step that actually answered on the most recent request. Eliminates "which step fired?" guesswork when fallbacks are routing live.

**Where**:

- New endpoint near `renderMappingsTable` in [`handlers.go:305-355`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L305): `GET /v1/mappings/last-responders` returning `map[string]int{ mappingName: responderIndex }`.
- Source the data from the existing log path at [`proxy.go:326-335`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/proxy.go#L326) which already logs `"fallback succeeded", "attempt", i, "provider", …`. Wrap with a small `sync.Map[providerName]int` with a 60-second TTL and a goroutine eviction.
- Render in [`mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30): `<span class="route-step--responder">` on the active index, styled with `@keyframes pulse { ... }` reusing `--accent`. `@media (prefers-reduced-motion: reduce)` already global at [`app.css:715-722`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L715).

**Risk**: per-request scan of `LogSink` would be O(N) — must pre-aggregate, never scan in the request path. See risk note §G.

#### 7. Empty-state card + global error fallback (P1, crosses all list pages) — Effort S — Risk low

**Where**:

- [`mappings-table.html:2`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L2): wrap with `{{if .Mappings}}…{{else}}…{{end}}` showing an empty-state card.
- [`providers-table.html:15`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers-table.html#L15): insert `<tr><td colspan="7" class="empty-state">No providers configured. <a href="#" onclick="document.getElementById('provider-dialog').showModal()">Add your first provider</a>.</td></tr>` when len==0.
- [`logs.html:21-29`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L21): show "Waiting for log events…" until SSE delivers one.

Add `.empty-state` rule at the end of [`app.css`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css) — center, padding `--space-6`, color `--text-muted`, button is the existing `.btn--primary`.

#### 8. Silent-error feedback wiring (P1, crosses all CRUD pages) — Effort S — Risk low

**Where**: add one inline `<script>` in each dialog host page (`providers.html`, `mappings.html`) that listens for `htmx:afterRequest` on the dialog's `<form>`, parses `event.detail.xhr.response` JSON, and writes `.textContent` into the relevant `#*-form-error-dialog` div. **Reuses** the empty `.form-error` divs already at [`providers.html:21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L21) and [`mappings.html:21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L21).

Pairs naturally with a toast helper for non-form swaps (deletes, fetches). See §C.4.

#### 9. Success toast on create/edit + visual indicator on the changed card — Effort S — Risk low

**Where**: new `<div id="toast-region" aria-live="polite" role="status">` in [`layout.html:46`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L46); `.toast` class appended to [`app.css`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css) with positions fixed bottom-right, styled with `--color-error` (error) / `--color-success` (success) / existing `--radius-md` and `--shadow-lg`.

Optional micro-follow-on: HTMX can swap in a `hx-swap-oob` style attribute to temporarily highlight the affected `.route-card` (300ms ring) — uses `@keyframes` similar to §6.

#### 10. Robustness: drop `| js` on `data-fallbacks` + remove `innerHTML` concat (impl-review F1 + adjacent XSS-shaped surface) — Effort S — Risk low

**Where**: exactly as described in §B / fix sketch. Touches [`mappings-table.html:14`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L14) and [`mappings.html:108-138`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L108).

#### 11. Reset-dialog-state helper (closes stale `hx-put` after Edit → Add cycle + form stale values) — Effort XS — Risk low

**Where**: new `openAddMapping()` helper alongside [`editMapping()`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L65) at [`mappings.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L64). Replace the inline `onclick` of the `+ Add Mapping` button at [line 5](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L5) to call `openAddMapping()`. Mirror for `openAddProvider()` at [`providers.html:5`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L5).

#### 12. Decorative chevron cleanup + first-step ::after suppressed — Effort XS — Risk low

**Where**: at [`app.css:808-816`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L808), add `.route-step:first-child::after { display: none; }` next to the existing `:last-child` rule. One CSS line.

**Cross-benefit**: works with §3 (visually-hidden role text) and §4 (link wrapping) to make the breadcrumb a fully readable, fully accessible, fully interactive surface.

### E. Architectural constraints to respect

- **No new JS libraries** — every proposal stays inside `htmx.min.js` + inline `<script>` on the page.
- **HTMX swap contract preserved** — `hx-target="#mappings" hx-swap="outerHTML"` at [`mappings.html:16-17`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L16) and [`mappings-table.html:22-23`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L22) unchanged in every patch.
- **Design-system tokens only** — every new CSS rule reuses `--color-*`, `--space-*`, `--radius-*`, `--bg-*`, `--text-*`, `--border-*`, `--accent*`, `--shadow-*` declared at [`app.css:7-70`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L7). No new colors.
- **No changes to `config.Mapping`** — every Go change touches only `proxy/web/types.go` (`fallbackEntry`, `mappingRow`, `providerRow`, `modelsData`) and `proxy/web/handlers.go` (row assembly). YAML schema unchanged.
- **BEM-ish class conventions** — proposals 1/2/3/5/7/9 follow the existing `.btn--{primary,secondary,danger,ghost}`, `.badge--{openai,anthropic,protocol}`, `.card`, `.form-*`, `.route-*` patterns.
- **Chrome + Firefox parity** — `::after`, `:last-child`, `:first-child`, flex `gap`/`flex-wrap`, `@keyframes`, `@media (max-width: 768px)` all stable in evergreen; `prefers-reduced-motion` already wired at [`app.css:715-722`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L715).
- **Mobile (<768px) stacking preserved** — every CSS addition either lives inside the existing `@media (max-width: 768px)` block ([`app.css:818-826`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L818)) or doesn't touch `.route-chain`'s `flex-direction`.
- **`<dialog>`/input safety** — F1 fix (§D.10) removes a real HTML-injection vector in the dialog by moving from `innerHTML` concatenation to `createElement` + `.value` assignments.

### F. What's already decided (do NOT re-investigate)

These were established in earlier PRs/roadmap and confirmed by archive research:

- **Templating engine**: `html/template` + vendored htmx (`proxy/web/static/htmx.min.js`) — no Vite, no Alpine, no React. Single static binary preserved.
- **Two-port model**: proxy `:8082`, UI `:8083` ([`handlers.go:27-38`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L27)).
- **Layout**: 240px sidebar + hamburger overlay on mobile ([`layout.html:15-38`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L15)).
- **Color system**: dark-mode-first zinc with `--accent` indigo + `--color-success/warning/error` semantic tokens ([`app.css:7-70`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L7)).
- **No SVG graph view** — explicitly deferred from V-02d ([`mapping-graph-visualization/plan.md:38`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/plan.md#L38)).
- **No drag-and-drop fallback reordering** — explicitly out of scope ([`plan.md:39`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/plan.md#L39)).
- **No view-toggle table/graph** — V-02d was a full replacement.
- **No accessibility audit, no i18n** — explicit out-of-scope across V-02b and V-02d plans. The breadcrumb's WCAG 1.4.1 fix is well-scoped enough to ride along without re-opening the audit.
- **`hx-confirm` over custom dialogs** — confirmed; this is the right primitive, just needs F3 fixed.
- **Model-picker UX = explicit "Fetch models" button + clickable list inside mapping modal** — no `<datalist>`, no Providers-page button.

### G. Risks to surface during implementation

1. **Per-step latency/responder aggregation must NOT scan `LogSink` per-request** — the ring buffer holds up to 10 000 entries; an O(N) scan inside `renderMappingsTable` would blow up request latency. Either (a) pre-aggregate asynchronously into a `sync.Map` with 60s TTL, or (b) skip the proposal entirely until a background aggregation primitive exists.
2. **Concurrent CRUD + HTMX swap on the same `/mappings` grid** — the dialog lives outside `#mappings`, so cards swap underneath without resetting it. Safe today; should remain safe after every proposal above.
3. **Static-asset cache** — `serveStatic` ([`embed.go:89-93`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L89)) sends `Cache-Control: public, max-age=300`. CSS additions can be masked behind a stale cached file for up to 5 minutes during local testing — short-circuit with `?v=` query-string or force a reload. Not a correctness blocker; DX sharp edge only.
4. **Edit form's stale `hx-put` (§B bonus bug)** is a real production bug — user clicks Edit → Cancel → Add → Save → silent failure. Fix must land together with §D.11 (the helper) or in the same PR; do not ship §1-§10 first.
5. **SSE-driven logs bypass the `?min=` filter** (§C.6) — affecting [`logs.html:31-52`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L31). Worth wiring `level >= minLevel` check in the JS as part of the empty-state/active-filter work.

## Code References (curated, GitHub permalinks)

Format: `path:line` → click opens at the exact line on `main`. All refs at commit `d6f1930a`.

**Templates (HTMX-facing)**
- [`proxy/web/templates/mappings.html:5`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L5) — `+ Add Mapping` button (needs §D.11 reset)
- [`proxy/web/templates/mappings.html:9,21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L9) — empty `#*-form-error` slots (§D.8 silent-error feedback)
- [`proxy/web/templates/mappings.html:16-17`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L16) — `hx-target="#mappings"` (must not change)
- [`proxy/web/templates/mappings.html:47-53`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L47) — fallback fieldset with inline-styles antipattern
- [`proxy/web/templates/mappings.html:64-89`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L64) — `editMapping()` + the stale `hx-put` bug
- [`proxy/web/templates/mappings.html:108-138`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L108) — `addFallbackRow` (innerHTML concat → XSS-shaped, §D.10)
- [`proxy/web/templates/mappings-table.html:14`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L14) — `data-fallbacks` double-escape (F1)
- [`proxy/web/templates/mappings-table.html:21`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L21) — unbalanced `hx-confirm` (F3)
- [`proxy/web/templates/mappings-table.html:30-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L30) — `.route-step` rendering (target of §D.1/3/4/5/6/12)
- [`proxy/web/templates/providers-table.html:34`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers-table.html#L34) — provider F3 unbalanced confirm
- [`proxy/web/templates/providers-table.html:15`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers-table.html#L15) — empty `<tbody>` (no empty-state)
- [`proxy/web/templates/logs.html:5-20`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L5) — level filter (no `hx-push-url`, no chip)
- [`proxy/web/templates/logs.html:31-52`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/logs.html#L31) — SSE handler (filters by nothing)
- [`proxy/web/templates/index.html:5-42`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/index.html#L5) — non-interactive dashboard cards
- [`proxy/web/templates/layout.html:10-13`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L10) — sidebar toggle (mobile auto-close gap, see full report)
- [`proxy/web/templates/layout.html:20-37`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L20) — sidebar nav (no path-breadcrumb anywhere)
- [`proxy/web/templates/layout.html:46`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/layout.html#L46) — `</body>` close (target for §D.9 toast-region)
- [`proxy/web/templates/models-fragment.html:1-19`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/models-fragment.html#L1) — unbounded DOM (F2) + missing in-flight banner (F4)

**Go**
- [`proxy/web/handlers.go:60-62`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L60) — `/mappings` route
- [`proxy/web/handlers.go:96-155`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L96) — `handleLogs` (extend for `?provider=&mapping=`)
- [`proxy/web/handlers.go:193-242`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L193) — `handleMappings` (populate Protocol per step)
- [`proxy/web/handlers.go:305-355`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L305) — `renderMappingsTable` mirror (same Protocol population)
- [`proxy/web/handlers.go:687-717`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L687) — `handleRefreshModels` (F4 in-flight banner + F2 truncation)
- [`proxy/web/handlers.go:756`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L756) — `writeJSONError` (where errors come from)
- [`proxy/web/embed.go:89-93`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L89) — `serveStatic` (`Cache-Control: max-age=300` — DX sharp edge for CSS iteration)
- [`proxy/web/forms.go:25-69`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/forms.go#L25) — `writeValidationError` returns `{fields:{...}}` already
- [`proxy/web/types.go:57-82`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L57) — `fallbackEntry`, `mappingRow`, `FallbacksString()` (drop the dead helpers)
- [`proxy/web/types.go:91-97`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L91) — `modelsData` (add `Truncated`, `FetchInProgress`)

**CSS (the route-chain block lives at end-of-file)**
- [`proxy/web/static/app.css:7-70`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L7) — design tokens (`:root`)
- [`proxy/web/static/app.css:74-107`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L74) — `@media (prefers-color-scheme: light)` overrides
- [`proxy/web/static/app.css:132-258`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L132) — `.sidebar` + hamburger
- [`proxy/web/static/app.css:405-490`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L405) — `.btn` family + the duplicate `.btn-cancel`
- [`proxy/web/static/app.css:611-644`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L611) — `.badge--{openai,anthropic,protocol}` (reusable for §D.1)
- [`proxy/web/static/app.css:690`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L690) — `.text-muted` (only utility class today; suggest adding `.visually-hidden` near here)
- [`proxy/web/static/app.css:715-722`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L715) — `prefers-reduced-motion` global guard
- [`proxy/web/static/app.css:750-828`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L750) — breadcrumb-chain block (`.mappings-grid`, `.route-card`, `.route-chain`, `.route-step`, `::after` chevrons, mobile media query) — primary target for the proposals

**Proxies / dispatch (background context only — do not modify)**
- [`proxy/proxy.go:266-394`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/proxy.go#L266) — fallback dispatch (already logs `"fallback succeeded", "attempt", i, …`)
- [`proxy/mix.go:84-101`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/mix.go#L84) — `normalizeBaseURL` infers protocol from URL
- [`proxy/logtee.go:33-49`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/logtee.go#L33) — `LogSink` ring buffer (capped at 10 000)

## Architecture Insights

1. **The dashboard deliberately deferred to follow-ups.** The V-02b plan asked "what stats should the dashboard show?" and locked in 4 generic counters ([`index.html:5-42`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/index.html#L5)); the next natural step (request-stream feed, per-provider health) is explicitly parked in [`roadmap.md:233-238`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/roadmap.md#L233) under "single-user local tool". Any "richer dashboard" work belongs to a separate future change.

2. **`FallbacksString()` and `fallbackEntry.String()` are dead code carried over from the "Phase 1 backward-compat" requirement** of the V-02d plan. Verified via `grep` — zero callers outside the definition. Removal is purely cleanup; safe in the same PR as §D.1's Protocol addition.

3. **`json.Marshal` was chosen over `json.NewEncoder` for SSE framing** ([`proxy/translate/anthropic_openai.go`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/translate/anthropic_openai.go)) — explicit lesson in [`context/foundation/lessons.md:3-7`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/lessons.md#L3). **Directly relevant to §D.10** — `addFallbackRow`'s innerHTML concat is the JS-side analog of using the wrong encoder: silent framing-bug class. The patch that fixes F1 should reuse the same lesson.

4. **HTMX 2.0 syntax (`hx-on::after-request`)** at [`providers.html:18`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L18) and [`mappings.html:18`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L18) is the modern path — keep using it. `htmx:responseError` is the relevant event for §D.8.

5. **Layout is a true BEM-ish single-class system** — every visible element class on a page is namespaced under `.btn--*`, `.form-*`, `.route-*`, `.badge--*`, `.card`, etc. There is no `.d-flex`, `.stack`, or `.container` utility. Every future proposal MUST follow this discipline; introducing a utility-class layer would conflict with the established style (it's the OPPOSITE direction the V-02b team chose).

6. **The two servers design is intentional** — proxy on `:8082`, UI on `:8083` ([`handlers.go:27-38`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L27)). The UI can be turned off via env vars; nothing in the dispatcher needs the UI to be alive. Any "live update from inside the dashboard" needs explicit cross-port SSE/WS plumbing — no implicit upgrade path.

## Historical Context

- **V-02 (initial Web UI, PR #26, commit 461f61d, 2026-07-02)** — replaced TUI, set the two-port model, vendored `htmx.min.js`, scaffolded `proxy/web/{embed,handlers,forms,server,types}.go`. Light-mode CSS, plain tables, horizontal `<nav>`, `hx-confirm` for deletes. Full diff at [`git log -p --no-merges -1 461f61d`](https://github.com/pfrack/freedius/commit/461f61d). Archived at `context/archive/2026-07-02-web-ui/`.

- **V-02a (provider-model-discovery, PR #28, commit c248c83)** — added "Fetch models" button + click-to-fill list inside the mapping modal. The decision that the picker lives IN the modal (no Providers-page UI, no `<datalist>`) is locked in. Archived at `context/archive/2026-07-05-provider-model-discovery/`.

- **V-02b (Web UI redesign, PR #29, commit e8de2ef, 2026-07-05)** — complete CSS reset (226 → 828 lines), dark-mode-first zinc + indigo accent, fixed 240px sidebar + hamburger overlay, BEM component classes, `transition` + `prefers-reduced-motion`. Established every design token the current work reuses. Archived at `context/archive/2026-07-05-web-ui-redesign/`.

- **V-02c (provider-fallback-routing, PR)** — config schema `fallback:` array on mappings + dispatcher retry logic. Pure data-model + dispatcher work; no UI surface change. The breadcrumbs built on top of this data.

- **V-02d (mapping-graph-visualization, PR #30, commit d6f1930, 2026-07-07)** — replaced flat `<table>` with breadcrumb-chain cards. Decision deferred to V-02d's "What We're NOT Doing": no SVG graph view, no drag-and-drop, no view toggle. Live at `context/changes/mapping-graph-visualization/` — 5-phase plan completed for Phases 1, 2, 3, 4 and Phase 5 (model filter); impl-review at [`reviews/impl-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/impl-review.md) flagged F1-F4 PENDING (all still open per this research).

- **`context/foundation/roadmap.md` status mismatch** — line 44 marks V-02d as `planned`, but commit `d6f1930` shows the code is shipped. The change-id directory under `context/changes/` carries `status: impl_reviewed` (Phase 5 was the final phase per the plan). Treat the code as `done` and the roadmap line as stale; the next change is a follow-up to V-02d's review — not a re-implementation.

## Related Research

- [`context/changes/mapping-graph-visualization/plan.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/plan.md) — 5-phase implementation plan for the breadcrumb-chain cards (Phases 1-4 done; Phase 5 model filter done)
- [`context/changes/mapping-graph-visualization/reviews/plan-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/plan-review.md) — pre-implementation plan review (risks + scope)
- [`context/changes/mapping-graph-visualization/reviews/impl-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/impl-review.md) — PENDING findings F1-F4, all verified still in code at commit `d6f1930` (the primary input to this research)
- [`context/archive/2026-07-02-web-ui/research.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/archive/2026-07-02-web-ui/research.md) — initial Web UI research; established the two-port + HTMX + single-binary constraints
- [`context/archive/2026-07-05-web-ui-redesign/research.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/archive/2026-07-05-web-ui-redesign/research.md) — design-system research; locked in the zinc + indigo tokens
- [`context/archive/2026-07-05-provider-model-discovery/research.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/archive/2026-07-05-provider-model-discovery/research.md) — model picker UX research; settled the clickable-list-in-modal pattern
- [`context/foundation/lessons.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/lessons.md) — code-wide lessons; relevant items: "json.Marshal over json.NewEncoder for SSE" (applies to F1 / §D.10), "Adapter Return Contract" (apply to error-feedback JS in §D.8), "Embrace Extra Tests" (apply to new JS-handler tests)
- [`context/foundation/roadmap.md:44`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/roadmap.md#L44) — V-02d entry (planned status, stale wrt code)

## Open Questions

These need a human decision before `/10x-plan`:

1. **Scope of the first PR** — ship §B (the 4 PENDING fixes + stale `hx-put` bonus) alone as a quick bugfix, or roll §C.1/§C.2/§C.4 (silent errors + broken confirm + no toast) in with §D.1/§D.3 (protocol badge + a11y role text) as one larger "breadcrumb polish" PR? The bugfix-only path is XS risk; the polish path touches more files but stays within the design tokens.
2. **Per-step click-through to logs (§D.4)** — requires extending `handleLogs` to accept `?provider=&mapping=`. This is a small Go change but a *new* query-string contract (must be documented; should be `hx-push-url`-safe).
3. **Last-responder highlight (§D.6)** — needs a backend decision on the aggregation primitive. Three options: (a) skip for now (cheap), (b) add a `sync.Map[providerName]int` aggregator with 60s TTL inside `proxy/eventbus.go`, (c) push a `/v1/mappings/last-responders` SSE channel. Each carries different ops/observability implications.
4. **Filter/search on providers + mappings** — out-of-scope today; flagged here because users with 30+ providers will hit it in week 2. Decide whether `web-ui-friendliness` covers it or whether it's its own slice.
5. **Accessibility audit** — explicit out-of-scope in V-02b and V-02d. The breadcrumb WCAG 1.4.1 fix is small enough to ride along without re-opening; agree to keep a11y out-of-scope otherwise?
6. **Static asset cache busting** — keep `Cache-Control: max-age=300` or shorten during dev? Decision needed only if `/10x-tdd` or `/10x-implement` start hitting stale-CSS bugs.
7. **Decorative chevron after the first step (§C.11)** — pair with §D.3 (a11y aria-label) so the fix doesn't visually look wrong for a moment during the transition.
