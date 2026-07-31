# Custom 404 Page Implementation Plan

## Overview

Add a branded, helpful 404 experience to the freedius embedded web dashboard. Today the `GET /` route is a Go 1.22 `ServeMux` catch-all, so any unknown GET path (`/logz`, a typo'd link, `/v1/anything` via GET) silently renders the full dashboard with **200 OK** — a dead end that lies about the resource existing. This plan turns `GET /` into a real router boundary (exactly `/` serves the dashboard; everything else 404s) and extends the same branded 404 to missing `/static/` assets, which currently return `http.FileServerFS`'s plain-text `404 page not found`.

## Current State Analysis

- **`proxy/web/handlers.go:44`** — `mux.HandleFunc("GET /", func(w, _ *http.Request){…})` renders `index.html`. In Go 1.22+ `ServeMux`, the pattern `/` is the root subtree and matches **any** GET path not claimed by a more specific pattern. The handler discards the request (`_`), so it cannot branch on the path.
- **Route precedence is safe to rely on**: `/health`, `/logs`, `/providers`, `/mappings`, `/static/`, and the `/v1/...` patterns are registered explicitly and win over `/`. Only genuinely unregistered GET paths fall through to the catch-all. Confirmed against `SetupMux` (`proxy/web/handlers.go:27-128`).
- **`proxy/web/embed.go:97` `renderPage`** sets `Content-Type: text/html` then `ExecuteTemplate(w, "layout", data)`. It never calls `WriteHeader`, so the first body write implicitly emits **200**. A 404 must write the status *before* the body — the current helper can't express that.
- **`proxy/web/embed.go:91` `serveStatic`** sets `Cache-Control: public, max-age=300` then delegates to `http.StripPrefix("/static/", http.FileServerFS(StaticFS()))`. On a miss the FileServer calls `http.Error(...)` → `Content-Type: text/plain` + `WriteHeader(404)` + body `404 page not found`. `serveStatic` has no access to `logger`.
- **Layout contract** (`proxy/web/templates/layout.html`): the sidebar highlights the active item via `{{if eq .Active "index"}}…`. Every page struct embeds `pageData{Active string}` (`proxy/web/types.go:8`). A 404 rendered through the layout gets the sidebar (and thus a one-click "way back") for free; `Active: ""` highlights nothing, which is correct.
- **Per-page template isolation**: `loadPageTemplate` (`embed.go:65`) parses `layout.html` + exactly one page file per set and caches it. This is why each page can `{{define "content"}}` / `{{define "title"}}` without collision — a new `404.html` follows the same rule.
- **Test conventions** (`proxy/web/handlers_test.go`): `httptest` + `strings.Contains` assertions on rendered HTML and on headers (e.g. `Cache-Control` `max-age=300` at line 88). One case already fetches `/` expecting a 200 dashboard — the branch must preserve that.
- **Auth interplay** (`proxy/web/server.go:38-41`): when `AuthToken != ""`, `h.RequireAuth(mux)` wraps the whole mux, so unknown paths are auth-gated *before* reaching the catch-all. Unauthenticated → 401 (unchanged); authenticated → our 404. No change needed, but the plan must not assume the 404 is always reachable pre-auth.

## Desired End State

- `GET /` → dashboard, **200** (unchanged).
- `GET /<anything-else>` → branded 404 page (full sidebar layout, oversized `404` mark, headline, "Back to dashboard" primary action, quick links to Mappings/Providers/Logs), **status 404**, `Content-Type: text/html`.
- `GET /static/<missing>` → the same branded 404 HTML, **status 404**, without the `public, max-age=300` cache header.
- `GET /static/app.css` (and other real assets) → **200** with `Cache-Control: public, max-age=300` (unchanged).
- All existing `proxy/web` tests stay green; new tests cover both 404 paths and the `/`-still-200 regression.

Verify by: `go test ./proxy/web/` green; manual browser visit to `/nope` and `/static/nope.css`.

### Key Discoveries:

- Catch-all branch idiom: `if r.URL.Path != "/" { renderNotFound(...); return }` — the standard Go 1.22 way to reclaim a custom 404 from the root subtree (`proxy/web/handlers.go:44`).
- Status must precede body: need a `renderPageStatus(w, status, …)` that does `Header().Set("Content-Type") → WriteHeader(status) → ExecuteTemplate`; refactor `renderPage` to call it with `200` so existing callers are untouched (`proxy/web/embed.go:97-108`).
- Static 404 interception: a wrapper `http.ResponseWriter` that watches for `WriteHeader(404)` from the FileServer, diverts to the branded render, and swallows the FileServer's plain body — the established technique for rebranding `FileServer` errors (`proxy/web/embed.go:91-94`).

## What We're NOT Doing

- **Non-GET / method-mismatch pages.** `POST /unknown`, `DELETE /whatever`, etc. keep `ServeMux`'s default plain-text 404/405. (User scope: pages + static only.)
- **Rewriting routing** or introducing a third-party router — staying on stdlib `http.ServeMux` per `AGENTS.md`.
- **Echoing the attempted path** in the 404 body (user chose "quick links" over "include the attempted path"). Keeps us clear of any reflected-content escaping concerns.
- **Custom 401/403/500 pages** — out of scope; only 404.
- **Changing auth behavior** — `RequireAuth` remains the outer boundary.

## Implementation Approach

Two incremental phases. Phase 1 delivers the user-visible win (navigation 404) end-to-end: a status-aware render helper, the template, the CSS, and the `/` branch, with tests. Phase 2 extends the identical branded page to static-asset misses via a small response interceptor, with tests. Both live entirely in `proxy/web/`.

## Critical Implementation Details

- **Header ordering under interception.** `http.Error` sets `Content-Type: text/plain` and `X-Content-Type-Options: nosniff` *before* it calls `WriteHeader(404)`. Because the interceptor catches `WriteHeader(404)` and renders *before* delegating, `renderPageStatus`'s `Header().Set("Content-Type", "text/html; charset=utf-8")` still overrides the pending text/plain (headers aren't flushed yet). The interceptor must also `Header().Del("Cache-Control")` so the branded 404 isn't cached for 5 minutes.
- **Only divert 404.** The interceptor must pass `304`/`206`/`200` straight through — FileServer uses `304 Not Modified` for conditional requests and `206` for ranges. Diverting anything but `404` would corrupt caching/range behavior.

---

## Phase 1: Branded 404 for unknown page routes

### Overview

Introduce a status-aware page renderer and a `renderNotFound` helper, create the `404.html` template and `.not-found` styles, and branch the `GET /` handler so only the exact root serves the dashboard.

### Changes Required:

#### 1. Status-aware render helpers

**File**: `proxy/web/embed.go`

**Intent**: Allow rendering a full layout page with an explicit HTTP status (needed to emit 404 before the body), and provide a single entry point for the branded not-found page so both the page-route branch (Phase 1) and the static interceptor (Phase 2) render identically.

**Contract**:
- New `renderPageStatus(w http.ResponseWriter, status int, pageFile string, data any, logger *slog.Logger, extraFiles ...string)`: sets `Content-Type: text/html; charset=utf-8`, calls `w.WriteHeader(status)`, then executes the cached `"layout"` template. On template-load error, keep the existing 500 behavior.
- Refactor existing `renderPage(...)` to delegate to `renderPageStatus(w, http.StatusOK, …)` — signature and all current call sites unchanged.
- New `renderNotFound(w http.ResponseWriter, logger *slog.Logger)`: `w.Header().Del("Cache-Control")` then `renderPageStatus(w, http.StatusNotFound, "404.html", pageData{Active: ""}, logger)`.

#### 2. 404 template

**File**: `proxy/web/templates/404.html` (new)

**Intent**: A branded not-found page rendered through the shared layout so it inherits the sidebar (way back), fonts, and grain. Provides a primary "Back to dashboard" action plus quick links to the other sections — no dead end.

**Contract**: Defines `{{define "title"}}Page not found{{end}}` and `{{define "content"}}…{{end}}` (mirroring `index.html`'s structure, including the vestigial `{{define "404"}}{{template "layout" .}}{{end}}` wrapper for convention). Content root is a `.not-found` block containing: `.not-found__code` (`404`), an `<h1>` headline, a one-sentence `<p>`, a primary `<a href="/" class="btn btn--primary">`, and a small `<nav>` of links to `/mappings`, `/providers`, `/logs`. No template data beyond `pageData` is referenced.

#### 3. Not-found styles

**File**: `proxy/web/static/app.css`

**Intent**: A dedicated, on-brand error block distinct from the dashed `.empty-state`, giving the 404 typographic presence via an oversized tabular numeral using existing design tokens.

**Contract**: Add a `.not-found`, `.not-found__code`, `.not-found__actions`, `.not-found__links` rule group. Use existing custom properties only (`--space-*`, `--accent`, `--text-*`, `--radius-*`, `--font-*`); the numeral uses `font-variant-numeric: tabular-nums` and negative letter-spacing. No new colors or hard-coded z-index (respect the `--z-*` scale established in the prior redesign).

#### 4. Branch the root handler

**File**: `proxy/web/handlers.go`

**Intent**: Reclaim the catch-all: serve the dashboard only for the exact root path and route everything else to the branded 404.

**Contract**: In the `GET /` handler, capture the request (`r *http.Request` instead of `_`) and add a guard at the top: when `r.URL.Path != "/"`, call `renderNotFound(w, logger)` and return; otherwise run the existing dashboard render unchanged.

#### 5. Tests — page 404 + regression

**File**: `proxy/web/handlers_404_test.go` (new) — or extend `handlers_test.go`

**Intent**: Lock the new boundary behavior and guard the dashboard regression.

**Contract**: Table/unit tests using `httptest` against `SetupMux`:
- `GET /definitely-not-a-route` → `404`, `Content-Type` contains `text/html`, body contains the branded markers (`not-found__code`, `Page not found`, `href="/"`) and the sidebar (`<nav>`).
- `GET /` → `200` and body contains a dashboard marker (e.g. `class="stats-grid"`), asserting the branch didn't break the root.

### Success Criteria:

#### Automated Verification:

- [ ] Build passes: `go build ./...`
- [ ] Formatting/lint clean: `mage lint` (gofmt/goimports/gci/golines + vet + staticcheck)
- [ ] Web package tests pass: `go test ./proxy/web/`
- [ ] New test asserts unknown GET path → status 404 + `text/html` + branded body markers
- [ ] Regression test asserts `GET /` → status 200 + dashboard marker

#### Manual Verification:

- [ ] Visiting a bad path (e.g. `http://localhost:<port>/nope`) shows the styled 404 with the sidebar and a working "Back to dashboard" button
- [ ] Quick links (Mappings/Providers/Logs) navigate correctly
- [ ] 404 looks correct in both dark and light `prefers-color-scheme`, and respects reduced-motion (no animation jank)

**Implementation Note**: After completing this phase and all automated verification passes, pause for human confirmation that manual testing succeeded before starting Phase 2.

---

## Phase 2: Extend 404 to static-asset misses

### Overview

Rebrand `http.FileServerFS`'s plain-text 404 for missing `/static/` files by wrapping the response writer, so a direct hit on a missing asset returns the same branded HTML page with status 404 — while real assets and conditional/range responses pass through untouched.

### Changes Required:

#### 1. Static 404 interceptor

**File**: `proxy/web/embed.go`

**Intent**: Detect the FileServer's `WriteHeader(404)`, divert to the branded not-found render, and discard the FileServer's plain body — without disturbing 200/304/206 responses or the cache header on real assets.

**Contract**: Add an unexported `http.ResponseWriter` wrapper (e.g. `notFoundInterceptWriter`) embedding the real writer plus `logger` and an `intercepted bool`. Override `WriteHeader(code)`: on `http.StatusNotFound`, set `intercepted = true` and call `renderNotFound(underlying, logger)`, then return (do not forward the 404 to the underlying writer directly — `renderNotFound` writes it); otherwise forward. Override `Write(b)`: when `intercepted`, return `len(b), nil` to swallow the FileServer body; else forward. Update `serveStatic` to still `Header().Set("Cache-Control", …)` for the happy path, then serve through the wrapper. `serveStatic` must receive `logger` (see change #2).

```go
// Signature contract for the wrapper (bodies elided):
type notFoundInterceptWriter struct {
    http.ResponseWriter
    logger      *slog.Logger
    intercepted bool
}
func (w *notFoundInterceptWriter) WriteHeader(code int) // divert 404 → renderNotFound
func (w *notFoundInterceptWriter) Write(b []byte) (int, error) // swallow body when intercepted
```

#### 2. Wire logger into static handler

**File**: `proxy/web/handlers.go`

**Intent**: `serveStatic` needs `logger` to render the branded 404; the current registration passes only `(w, r)`.

**Contract**: Change the `GET /static/` registration to a closure that calls `serveStatic(w, r, logger)` (or make `serveStatic` a method/closure capturing `logger`). No route pattern change.

#### 3. Tests — static 404 + asset regression

**File**: `proxy/web/handlers_404_test.go`

**Intent**: Verify missing assets are branded and real assets are unaffected.

**Contract**:
- `GET /static/does-not-exist.css` → `404`, `Content-Type` contains `text/html`, body contains a branded marker (`not-found__code`), and body does **not** equal/contain the plain `404 page not found` sentinel; `Cache-Control` is not `public, max-age=300`.
- `GET /static/app.css` → `200`, `Cache-Control` contains `max-age=300` (mirrors existing `handlers_test.go` assertion, ensuring the wrapper didn't regress the happy path).

### Success Criteria:

#### Automated Verification:

- [ ] Build passes: `go build ./...`
- [ ] Formatting/lint clean: `mage lint`
- [ ] Web package tests pass: `go test ./proxy/web/`
- [ ] New test asserts `GET /static/<missing>` → 404 + `text/html` + branded marker + no `public, max-age=300`
- [ ] Regression test asserts `GET /static/app.css` → 200 + `Cache-Control: …max-age=300`
- [ ] Full suite with race passes: `mage test`

#### Manual Verification:

- [ ] Navigating directly to `http://localhost:<port>/static/nope.js` shows the branded 404 (not plain text)
- [ ] The dashboard still loads its real CSS/JS (no console 404s on normal pages)

**Implementation Note**: After automated verification passes, pause for human confirmation of manual testing.

---

## Testing Strategy

### Unit Tests:

- Unknown GET page path → 404 HTML with branded markers + sidebar.
- `GET /` → 200 dashboard (regression against the new branch).
- Missing `/static/*` → 404 branded HTML, no long-cache header, not the plain FileServer body.
- Existing `/static/app.css` → 200 + `max-age=300` (regression against the interceptor).

### Integration Tests:

- Covered by the `httptest`-against-`SetupMux` handler tests (the package's established integration style); no separate harness needed.

### Manual Testing Steps:

1. `mage run`, then in a browser visit `/` → dashboard loads (200).
2. Visit `/nope` → branded 404 with working "Back to dashboard" + section links.
3. Visit `/static/nope.css` → branded 404 (view-source shows HTML, not `404 page not found`).
4. Toggle OS light/dark and reduced-motion; confirm the 404 renders correctly.

## Performance Considerations

Negligible. The 404 template is parsed once and cached like every other page (`loadPageTemplate`/`sync.Map`). The static interceptor adds one struct allocation and two method dispatches per `/static/` request — immaterial next to filesystem serving, and it only changes behavior on the 404 branch.

## Migration Notes

None. No config, schema, or persisted-state changes. Behavior change is limited to previously-200 unknown routes now correctly returning 404.

## References

- Root catch-all handler: `proxy/web/handlers.go:44`
- Render helper to refactor: `proxy/web/embed.go:97`
- Static handler to wrap: `proxy/web/embed.go:91`
- Layout `.Active` contract: `proxy/web/templates/layout.html:27`, `proxy/web/types.go:8`
- Header/cache test precedent: `proxy/web/handlers_test.go:80-90`
- Prior UI work (design tokens, z-index scale): `context/changes/web-ui-design-upgrade/`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Branded 404 for unknown page routes

#### Automated

- [x] 1.1 Build passes: `go build ./...`
- [x] 1.2 Formatting/lint clean: `mage lint`
- [x] 1.3 Web package tests pass: `go test ./proxy/web/`
- [x] 1.4 New test asserts unknown GET path → 404 + `text/html` + branded body markers
- [x] 1.5 Regression test asserts `GET /` → 200 + dashboard marker

#### Manual

- [ ] 1.6 Bad path shows styled 404 with sidebar and working "Back to dashboard"
- [ ] 1.7 Quick links (Mappings/Providers/Logs) navigate correctly
- [ ] 1.8 404 correct in dark + light and respects reduced-motion

### Phase 2: Extend 404 to static-asset misses

#### Automated

- [ ] 2.1 Build passes: `go build ./...`
- [ ] 2.2 Formatting/lint clean: `mage lint`
- [ ] 2.3 Web package tests pass: `go test ./proxy/web/`
- [ ] 2.4 New test asserts `GET /static/<missing>` → 404 + `text/html` + branded marker + no `public, max-age=300`
- [ ] 2.5 Regression test asserts `GET /static/app.css` → 200 + `Cache-Control: …max-age=300`
- [ ] 2.6 Full suite with race passes: `mage test`

#### Manual

- [ ] 2.7 Direct visit to `/static/nope.js` shows the branded 404 (not plain text)
- [ ] 2.8 Dashboard still loads its real CSS/JS (no console 404s)
