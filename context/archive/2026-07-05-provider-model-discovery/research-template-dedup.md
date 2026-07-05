---
date: 2026-07-05T22:00:00+02:00
researcher: MiMoCode
git_commit: 00dae00
branch: main
repository: freedius
topic: "Template duplication between page and fragment templates"
tags: [research, templates, htmx, deduplication, go-html-template]
status: complete
last_updated: 2026-07-05
last_updated_by: MiMoCode
---

# Research: Eliminating Template Duplication Between Page and Fragment Templates

**Date**: 2026-07-05T22:00:00+02:00
**Researcher**: MiMoCode
**Git Commit**: 00dae00
**Branch**: main
**Repository**: freedius

## Research Question

Can the duplicated table markup between page templates (`providers.html`, `mappings.html`) and their HTMX fragment counterparts (`providers-table.html`, `mappings-table.html`) be eliminated by restructuring the template loading system?

## Summary

Yes — the duplication can be eliminated with a minimal change to `loadPageTemplate`. The root cause is that page templates and fragment templates are parsed into separate template sets, so they can't share `{{define}}` blocks. The fix: parse the fragment file *alongside* the page template in a single template set, then have the page's `{{define "content"}}` call the fragment via `{{template "providers-table" .}}`. The fragment file continues to work standalone for HTMX requests. Total impact: ~6 lines changed across 3 files, zero new files.

## Detailed Findings

### Current Architecture

The template system has two loading paths (`proxy/web/embed.go`):

1. **Page templates** (`loadPageTemplate`): parses `layout.html` + one page file into a shared template set. Each page defines `{{define "content"}}` which overrides the layout's `{{block "content"}}`. Cached per page file in `pageTemplates sync.Map`.

2. **Fragment templates** (`loadFragmentTemplate`): parses a single file with no layout wrapping. Used for HTMX inline replacements. Cached in `fragmentTemplates sync.Map`.

The critical constraint: these are **separate template sets**. A `{{define}}` in a page template is invisible to a fragment template and vice versa.

### The Duplication

Four files are involved:

| File | Role | Defines |
|------|------|---------|
| `providers.html` | Full page (layout + table + dialog + script) | `{{define "providers"}}`, `{{define "title"}}`, `{{define "content"}}` |
| `providers-table.html` | HTMX fragment (table only) | Raw HTML, no `{{define}}` blocks |
| `mappings.html` | Full page (layout + table + dialog + script) | `{{define "mappings"}}`, `{{define "title"}}`, `{{define "content"}}` |
| `mappings-table.html` | HTMX fragment (table only) | Raw HTML, no `{{define}}` blocks |

The `<table>` markup (including `{{range}}` loops, button `onclick` handlers, and `hx-delete` attributes) is copy-pasted verbatim between each page/fragment pair. Lines 9–50 of `providers.html` duplicate lines 1–42 of `providers-table.html`. Same pattern for mappings.

### Why the Current Design Exists

The fragment templates were created to serve HTMX partial updates. When a user creates/updates/deletes a provider, the handler calls `renderProvidersTable` which loads `providers-table.html` as a standalone fragment and executes it — returning just the `<table>` HTML to swap into the DOM.

The page templates were created for full-page renders (direct browser navigation). They include the layout, page chrome (h1, buttons), the same table, plus dialogs and scripts.

The two paths evolved independently, leading to duplication.

### Proposed Solution

**Parse the fragment file alongside the page template.** Since `loadPageTemplate` already uses `template.ParseFS(assets, "templates/layout.html", "templates/"+pageFile)` with a variadic file list, we can add the fragment file:

```
template.ParseFS(assets, "templates/layout.html", "templates/"+pageFile, "templates/"+fragmentFile)
```

This puts both the page's `{{define "content"}}` and the fragment's table markup into one template set. The page's content block calls `{{template "providers-table" .}}` to render the shared table.

### Implementation Steps

**1. Add `{{define "providers-table"}}` wrapper to `providers-table.html`**

```html
{{define "providers-table"}}
<table id="providers">
  ...existing table markup...
</table>
{{end}}
```

Same for `mappings-table.html`:
```html
{{define "mappings-table"}}
<table id="mappings">
  ...existing table markup...
</table>
{{end}}
```

**2. Modify `loadPageTemplate` to accept extra files**

```go
func loadPageTemplate(pageFile string, extraFiles ...string) (*template.Template, error) {
    if cached, ok := pageTemplates.Load(pageFile); ok {
        return cached.(*template.Template), nil
    }
    files := []string{"templates/layout.html", "templates/" + pageFile}
    for _, f := range extraFiles {
        files = append(files, "templates/"+f)
    }
    tmpl, err := template.ParseFS(assets, files...)
    if err != nil {
        return nil, fmt.Errorf("parse %s: %w", pageFile, err)
    }
    actual, _ := pageTemplates.LoadOrStore(pageFile, tmpl)
    return actual.(*template.Template), nil
}
```

Existing callers (`renderPage`, test, logs HTMX) are unaffected — the variadic parameter defaults to empty.

**3. Update `renderPage` calls for providers/mappings**

```go
// In handleProviders (direct visit):
renderPage(w, "providers.html", data, logger)
// becomes:
renderPageWithFragments(w, "providers.html", []string{"providers-table.html"}, data, logger)

// In handleMappings (direct visit):
renderPage(w, "mappings.html", data, logger)
// becomes:
renderPageWithFragments(w, "mappings.html", []string{"mappings-table.html"}, data, logger)
```

Or simpler: add a wrapper `renderPageExtra` that passes the extra files through.

**4. Update `renderProvidersTable` / `renderMappingsTable`**

Change from `loadPageTemplate("providers-table.html")` to `loadFragmentTemplate("providers-table.html")` — the fragment is now a `{{define}}` block, so it should use the fragment loader which creates a template set with just that block.

Actually, `loadFragmentTemplate` uses `template.New(name).ParseFS(...)` which creates a root template named after the file. Executing `tmpl.ExecuteTemplate(w, "providers-table.html", data)` would work since the `{{define "providers-table"}}` block registers a named template in that set.

Wait — there's a subtlety. `loadFragmentTemplate` creates `template.New("providers-table.html")` as the root. The file defines `{{define "providers-table"}}` as a named template. `ExecuteTemplate(w, "providers-table.html", data)` would try to execute the root template (which is empty), not the defined block. We need `ExecuteTemplate(w, "providers-table", data)`.

**5. Update page templates to call the shared table**

In `providers.html`, replace the inline table with:
```html
{{define "content"}}
  <h1>Providers</h1>
  <button ...>+ Add Provider</button>
  <div id="provider-form-error" class="form-error"></div>
  {{template "providers-table" .}}
  <dialog ...>...</dialog>
  <script>...</script>
{{end}}
```

Same for `mappings.html`:
```html
{{define "content"}}
  <h1>Mappings</h1>
  <button ...>+ Add Mapping</button>
  <div id="mapping-form-error" class="form-error"></div>
  {{template "mappings-table" .}}
  <dialog ...>...</dialog>
  <script>...</script>
{{end}}
```

### Why This Works

Go's `html/template` allows multiple `{{define}}` blocks in a single template set. When `providers.html` and `providers-table.html` are parsed together:

- `layout.html` defines `{{define "layout"}}` with `{{block "content" .}}{{end}}`
- `providers.html` defines `{{define "content"}}` which overrides the layout's block
- `providers-table.html` defines `{{define "providers-table"}}` as a callable sub-template

When `ExecuteTemplate(w, "layout", data)` runs:
1. Layout renders, hits `{{block "content" .}}`
2. Finds `{{define "content"}}` from `providers.html` (overrides the default)
3. Content block executes `{{template "providers-table" .}}`
4. Finds `{{define "providers-table"}}` from `providers-table.html`
5. Table renders with the same `data` context

For HTMX fragments, `loadFragmentTemplate("providers-table.html")` creates a template set with just `{{define "providers-table"}}`. `ExecuteTemplate(w, "providers-table", data)` renders just the table.

### Edge Cases

- **Naming collision**: Page defines `"providers"`, fragment defines `"providers-table"`. No conflict — different names in the same set.
- **Cache key**: `pageTemplates` is keyed by `pageFile` ("providers.html"). Adding extra files doesn't change the key — same page always gets the same cached template set.
- **Fragment standalone**: `providers-table.html` works both as a standalone fragment (via `loadFragmentTemplate`) and as part of a page set (via `loadPageTemplate` with extras). The `{{define}}` block is the same either way.
- **Test unaffected**: `embed_test.go` tests `loadPageTemplate("index.html")` which doesn't use fragments.

### Impact Assessment

| Metric | Before | After |
|--------|--------|-------|
| Duplicated table markup | ~42 lines × 2 pairs = ~84 lines | 0 (shared via `{{define}}`) |
| Files changed | — | 4 (embed.go, providers.html, mappings.html, providers-table.html, mappings-table.html) |
| New files | — | 0 |
| Breaking changes | — | None |
| Risk | — | Low — Go template `{{define}}` blocks are well-understood; the two loading paths (page vs fragment) remain independent |

### Alternative Considered

**Template inheritance via `{{block}}`**: Go's `{{block "name" .}}...{{end}}` creates a default that can be overridden by a `{{define "name"}}` in the same set. This is what `layout.html` already uses for `"content"`. However, it doesn't help here because the fragment templates aren't parsed in the same set as the page templates — that's the whole problem being solved.

**Shared partial files without `{{define}}`**: Parsing a raw HTML file (no `{{define}}` wrapper) into a page set doesn't work — `template.ParseFS` requires valid template syntax. Every shared file needs a `{{define}}` block.

**Code-generation**: Generate the duplicate templates from a single source. Overkill for 2 pairs of templates.

## Code References

- `proxy/web/embed.go:51-61` — `loadPageTemplate` function (the target for the variadic change)
- `proxy/web/embed.go:34-43` — `loadFragmentTemplate` function (fragment loading path)
- `proxy/web/embed.go:79-89` — `renderPage` function (calls `loadPageTemplate`)
- `proxy/web/handlers.go:257-291` — `renderProvidersTable` (loads fragment as page template)
- `proxy/web/handlers.go:293-339` — `renderMappingsTable` (loads fragment as page template)
- `proxy/web/handlers.go:160-175` — `handleProviders` HTMX branch (calls `renderProvidersTable`)
- `proxy/web/handlers.go:204-219` — `handleMappings` HTMX branch (calls `renderMappingsTable`)
- `proxy/web/templates/providers.html:9-50` — duplicated table markup
- `proxy/web/templates/providers-table.html:1-42` — duplicated table markup
- `proxy/web/templates/mappings.html:9-44` — duplicated table markup
- `proxy/web/templates/mappings-table.html:1-36` — duplicated table markup
- `proxy/web/templates/layout.html:17` — `{{block "content" .}}` (the override mechanism)
- `proxy/web/embed_test.go:22-43` — test (unaffected)

## Architecture Insights

The template system follows a clean two-tier pattern:
- **Page tier**: layout + page, parsed together, cached per page. Used for full HTML responses.
- **Fragment tier**: standalone files, parsed individually, cached per file. Used for HTMX partial updates.

The duplication arose because these tiers evolved independently. The proposed fix keeps the tier separation intact — page templates still get layout wrapping, fragments still work standalone — while sharing the table definition via Go's native `{{define}}/{{template}}` mechanism.

This pattern scales to future pages: any new CRUD page with an HTMX-updated table would follow the same structure (page template calls shared table `{{define}}`, fragment template wraps the same `{{define}}` for standalone use).

## Historical Context

- The fragment templates (`providers-table.html`, `mappings-table.html`) were created during the provider-model-discovery change (commit `a9bc9e6`) to fix a pre-existing gap — `renderProvidersTable` and `renderMappingsTable` referenced these files but they didn't exist.
- The duplication was flagged as F3 in the implementation review (2026-07-05) and initially accepted as a trade-off. This research explores whether the trade-off is necessary.

## Open Questions

1. **Should `renderPage` gain the variadic parameter, or should a new `renderPageWithFragments` function be created?** The variadic approach is cleaner (one function, backward-compatible) but changes the signature of a widely-used function. A separate function keeps `renderPage` untouched but adds a new exported name.

2. **Should the render functions (`renderProvidersTable`, `renderMappingsTable`) switch from `loadPageTemplate` to `loadFragmentTemplate`?** Currently they use `loadPageTemplate` which parses `layout.html` + the fragment file. Since the fragment has no `{{define "content"}}` or `{{define "layout"}}` blocks, the layout parsing is wasted work. Switching to `loadFragmentTemplate` would be more efficient and semantically correct.
