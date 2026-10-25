# Fix Relative Dashboard Links — Implementation Plan

## Overview

When the freedius web dashboard is reverse-proxied behind nginx (or any reverse
proxy) at a sub-path like `http://example.com/freedius/`, all navigation links,
static asset references, HTMX endpoints, and SSE subscriptions break because they
are hardcoded as root-relative absolute paths (`/`, `/mappings`, `/static/app.js`,
`/v1/events`, etc.). The browser resolves these against the actual server root
(`http://example.com/`) instead of the nginx mount point
(`http://example.com/freedius/`).

This plan introduces a configurable **base path prefix** so the dashboard is
fully usable behind a reverse proxy at any sub-path.

## Current State Analysis

All links in the dashboard are root-relative absolute paths hardcoded across:

- **Templates** (`proxy/web/templates/`): `layout.html`, `index.html`,
  `logs.html`, `mappings.html`, `providers.html`, `mappings-routing-table.html`,
  `providers-table.html`, `mapping-drawer.html`, `provider-drawer.html`
- **Go handlers** (`proxy/web/handlers.go:322` for LogsLink, `handlers.go:1041`
  for provider EditLink)
- **Inline JavaScript** in `mappings.html` (lines 64, 121, 162, 230) and
  `providers.html` (lines 77, 99)

The web server runs on a separate port (default 8083), configured via
`FREEDIUS_UI_HOST` / `FREEDIUS_UI_PORT` env vars and `--ui-host` / `--ui-port`
flags (see `cmd/freedius/main.go:96-97, 181-182, 455-477`). No base-path
prefix mechanism exists. `SetupMux` builds the mux using Go 1.22 pattern
matching; `NewServer` wraps it with optional auth and CSRF middleware
(`proxy/web/server.go:27-63`).

Key constraints discovered:
- HTMX attributes (`hx-get`, `hx-post`, `sse-connect`) need prefixed URLs in the
  rendered HTML — they fire from the browser to the prefixed address.
- SSE endpoints (`/v1/events`, `/v1/logs`) are used by `sse-connect` in templates
  and must be prefixed.
- Inline JavaScript constructs URLs via string concatenation (e.g.
  `'/v1/providers/'+name+'/models/refresh'`) — these also need the prefix.
- `go:embed templates static` bundles all assets — no external file changes needed
  for the binary, only template source.

## Desired End State

When `FREEDIUS_UI_PATH=/freedius` (or `--ui-path /freedius`) is set:

1. Every link, script, style, image, and HTMX target in the dashboard HTML is
   automatically prefixed with `/freedius`.
2. A `GET /freedius/` request serves the dashboard; `GET /freedius/mappings`
   serves the mappings page; `GET /freedius/static/app.css` serves the asset.
3. Inline JavaScript URL construction uses a `window.FREEDIUS_BASE_PATH` global
   so HTMX AJAX calls target `/freedius/v1/...`.
4. SSE subscriptions connect to `/freedius/v1/events` and `/freedius/v1/logs`.
5. When the env var/flag is unset (empty string), behavior is identical to today
   — fully backward compatible.

## What We're NOT Doing

- **Per-tenant or per-route base paths** — single global prefix only.
- **Runtime reconfiguration** — path is fixed at process start (matches
  `FREEDIUS_UI_HOST`/`UI_PORT` behavior).
- **X-Forwarded-Path auto-detection** — operators set the env var explicitly;
  no header inspection.
- **Config file field** in `config.yaml` — UI settings live in env vars, not
  `config.yaml`; consistency with `--ui-host`/`--ui-port`.
- **Path rewriting via nginx alone** — the fix is in freedius so the dashboard
  works correctly without fragile nginx `sub_filter` directives.

## Implementation Approach

1. **Config wiring**: Add `--ui-path` flag + `FREEDIUS_UI_PATH` env var (mirroring
   `--ui-port`/`FREEDIUS_UI_PORT`), thread it through `cmd/freedius/main.go` →
   `eventstream.Handlers.BasePath` → `web.SetupMux` → `web.NewServer`.
2. **Middleware**: `http.StripPrefix(basePath, mux)` when basePath is non-empty,
   wrapping the mux inside `SetupMux` so Go 1.22 route patterns are unchanged.
3. **Template function**: A package-level `basePath` variable + `baseURL`
   template function that prepends the prefix. Used in every template link/attribute.
4. **JavaScript**: A `<meta name="base-path">` tag in `layout.html` exposes the
   prefix to inline JS and `app.js` via `window.FREEDIUS_BASE_PATH`.
5. **Go-side link generation**: `Handlers.BasePath` used in `handlers.go` for
   `LogsLink` and `EditLink` construction.

## Critical Implementation Details

- **Template function + meta tag**: The `baseURL` template function reads a
  package-level `basePath` variable (set once at startup via `SetBasePath`).
  A `<meta name="base-path" content="{{baseURL ""}}">` tag in `layout.html`
  lets JavaScript read `document.querySelector('meta[name="base-path"]').content`
  to get the prefix. This avoids needing `BasePath` on every data struct and
  works for fragment templates too.
- **StripPrefix ordering**: The `http.StripPrefix` wrapper must sit INSIDE the
  CSRF guard but OUTSIDE the auth wrapper, so that auth applies to the
  prefixed path (matching `server.go:38-44` middleware layering).
- **Empty basePath is a no-op**: When `basePath == ""`, `http.StripPrefix`
  must not be applied (it would strip nothing and add overhead). The
  `baseURL` template function returns the path unchanged.
- **No double-slash**: `baseURL("/mappings")` with `basePath="/freedius"`
  must yield `/freedius/mappings`, not `/freedius//mappings`.

## Phase 1: Configuration & Middleware Wiring

### Overview

Thread the base path from CLI/env into the web server, and add the
`http.StripPrefix` middleware so the existing mux routes work at the prefixed
path.

### Changes Required:

#### 1. `eventstream.Handlers` struct + `cmd/freedius/main.go`

**File**: `internal/eventstream/handlers.go`

**Intent**: Add `BasePath string` to the `Handlers` struct so it's available to
the mux setup and Go-side link generators, following the pattern of `Host`,
`Port`, etc.

**Contract**: New field `BasePath string` added to the `Handlers` struct
(`handlers.go:23-36`). No methods change. `NewServer` receives the `Handlers`
struct and can read `h.BasePath`.

#### 2. `cmd/freedius/main.go` — flag + env resolution

**File**: `cmd/freedius/main.go`

**Intent**: Add `--ui-path` flag and `FREEDIUS_UI_PATH` env var, resolve it, and
set `h.BasePath` before constructing the web server.

**Contract**:
- New flag: `flagUIPort := fs.Int("ui-port", 0, "web UI port ...")` (line 96)
  → add `flagUIPath := fs.String("ui-path", "", "web UI path prefix ...")`
- New resolve function mirroring `resolveUIPort`:
  ```go
  func resolveUIPath(flagVal string, flagSet bool) string {
      if flagSet { return flagVal }
      if v := os.Getenv("FREEDIUS_UI_PATH"); v != "" { return v }
      return ""
  }
  ```
- Set `h.BasePath = resolveUIPath(...)` in the `run` function before
  `web.NewServer`.

#### 3. `web/server.go` + `web/handlers.go` — StripPrefix middleware

**File**: `proxy/web/server.go:33` (calls `SetupMux`)

**Intent**: Wrap the mux with `http.StripPrefix(basePath, mux)` when basePath
is non-empty, inside `SetupMux` so middleware layering (CSRF/auth) is
preserved.

**Contract**: `SetupMux` gains a `basePath string` parameter. When non-empty,
returns `http.StripPrefix(basePath, mux)` as the inner handler (before CSRF
guard). When empty, behavior is unchanged. `NewServer` passes `h.BasePath`
to `SetupMux`.

### Success Criteria:

#### Automated Verification:
- `go build ./...` compiles.
- `mage test` passes (existing tests unchanged, new tests added in Phase 3).
- Manual test: start with `FREEDIUS_UI_PATH=/test`, `GET /test/health` returns
  200, `GET /health` (no prefix) returns 404.

#### Manual Verification:
- `mage run` with `FREEDIUS_UI_PATH=/freedius` — browser at
  `http://127.0.0.1:8083/freedius/` shows the dashboard.
- `http://127.0.0.1:8083/health` (no prefix) returns 404.

## Phase 2: Template Function + Link Prefixing

### Overview

Add a `baseURL` template function and prefix all template links, static
assets, HTMX targets, and SSE endpoints across every template.

### Changes Required:

#### 4. `proxy/web/embed.go` — package-level basePath + template function

**File**: `proxy/web/embed.go`

**Intent**: Add a package-level `basePath` variable with a `SetBasePath` setter,
and a `baseURL` template function registered in `templateFuncs`.

**Contract**:
```go
var basePath string

func SetBasePath(p string) { basePath = p }

// Add to templateFuncs:
"baseURL": func(path string) string {
    if basePath == "" { return path }
    if path == "" { return basePath }
    return strings.TrimRight(basePath, "/") + path
},
```
- `SetupMux` calls `SetBasePath(basePath)` at the top (after receiving the
  parameter from Phase 1).

#### 5. `proxy/web/templates/layout.html` — prefix assets + nav + meta tag

**File**: `proxy/web/templates/layout.html`

**Intent**: Prefix all static asset links, navigation links, and add a meta
tag for JavaScript access.

**Contract**: Change these lines in `layout.html`:
- Line 17: `<link rel="stylesheet" href="/static/app.css">` → `href="{{baseURL "/static/app.css"}}"`
- Lines 32-47: nav `<a href="/">` → `{{baseURL "/"}}`, `/mappings` → `{{baseURL "/mappings"}}`, etc.
- Lines 65-67: script srcs → `{{baseURL "/static/htmx.min.js"}}` etc.
- After the script tags, add:
  `<meta name="base-path" content="{{baseURL ""}}">`

#### 6. `proxy/web/templates/index.html` — prefix links + HTMX + SSE

**File**: `proxy/web/templates/index.html`

**Intent**: Prefix the `/mappings` link, HTMX drawer endpoints, provider
detail endpoints, and the SSE `sse-connect` URL.

**Contract**: Change:
- Line 67: `<a href="/mappings">` → `<a href="{{baseURL "/mappings"}}">`
- Line 86: `hx-get="/v1/mappings/..."` → `hx-get="{{baseURL "/v1/mappings/..."}}"`
- Line 123: `hx-get="/v1/providers/..."` → `hx-get="{{baseURL "/v1/providers/..."}}"`
- Line 140: `sse-connect="/v1/events"` → `sse-connect="{{baseURL "/v1/events"}}"`

#### 7. `proxy/web/templates/logs.html` — prefix HTMX + SSE

**File**: `proxy/web/templates/logs.html`

**Intent**: Prefix all `hx-get="/logs"` and `sse-connect="/v1/logs"`.

**Contract**: Lines 12, 33, 48, 61, 78 (hx-get), line 95 (sse-connect).

#### 8. `proxy/web/templates/mappings.html` — prefix form actions + inline JS

**File**: `proxy/web/templates/mappings.html`

**Intent**: Prefix `hx-post`, `hx-delete`, `hx-put`, `hx-target`, and rewrite
inline JS URL construction to use `window.FREEDIUS_BASE_PATH`.

**Contract**:
- Line 19: `hx-delete="/v1/mappings/__pending__"` → `{{baseURL "/v1/mappings/__pending__"}}"`
- Line 40: `hx-post="/v1/mappings"` → `{{baseURL "/v1/mappings"}}`
- Line 64: JS `htmx.ajax('POST', '/v1/providers/'+name+...)` → use
  `window.FREEDIUS_BASE_PATH + '/v1/providers/' + name + ...`
- Line 97: `f.setAttribute('hx-post', '/v1/mappings')` → prefix with `window.FREEDIUS_BASE_PATH`
- Line 121: `f.setAttribute('hx-put', '/v1/mappings/' + ...)` → prefix
- Line 162: `form.setAttribute('hx-delete', '/v1/mappings/' + ...)` → prefix
- Line 230: `htmx.ajax('POST', '/v1/providers/' + ...)` → prefix

#### 9. `proxy/web/templates/providers.html` — prefix form actions + inline JS

**File**: `proxy/web/templates/providers.html`

**Intent**: Prefix `hx-post`, `hx-put`, `hx-delete`, `hx-target`, and rewrite
inline JS URL construction.

**Contract**:
- Line 15: `hx-post="/v1/providers"` → `{{baseURL "/v1/providers"}}`
- Line 77: `f.setAttribute('hx-post', '/v1/providers')` → prefix with `window.FREEDIUS_BASE_PATH`
- Line 99: `f.setAttribute('hx-put', '/v1/providers/' + name)` → prefix

#### 10. `proxy/web/templates/mappings-routing-table.html` — prefix filters + actions

**File**: `proxy/web/templates/mappings-routing-table.html`

**Intent**: Prefix `hx-get="/mappings"` on the filter bar.

**Contract**: Line 3-5 (the `hx-get` / `hx-target` attributes).

#### 11. `proxy/web/templates/providers-table.html` — prefix actions + links

**File**: `proxy/web/templates/providers-table.html`

**Intent**: Prefix `hx-post`, `hx-delete`, `hx-target`, and the `/mappings?provider=`
link.

**Contract**: Line 57 (`href="/mappings?provider=..."`), line 72 (`hx-post`),
line 87 (`hx-delete`).

#### 12. `proxy/web/templates/mapping-drawer.html` — prefix edit link

**File**: `proxy/web/templates/mapping-drawer.html`

**Intent**: Prefix the "Edit on Mappings page" link.

**Contract**: Line 54: `'/mappings?search=...'` → `{{baseURL "/mappings?search=..."}}`

#### 13. `proxy/web/templates/provider-drawer.html` — prefix edit link

**File**: `proxy/web/templates/provider-drawer.html`

**Intent**: The `EditLink` is computed in Go (`handlers.go:1041`); prefix it
there.

**Contract**: No template change needed if Go-side fix covers it (Phase 3).

#### 14. `proxy/web/static/app.js` — use window.FREEDIUS_BASE_PATH

**File**: `proxy/web/static/app.js`

**Intent**: Any URL construction in app.js must use the global base path.
Current app.js does no URL construction (pure DOM/event handling), so this
may only need a comment.

**Contract**: If no URLs are constructed in app.js, no change needed. Verify
by grep — no root-relative paths exist in app.js currently.

## Phase 3: Go-side Link Generation + Tests

### Overview

Fix the two places in `handlers.go` where URLs are constructed in Go code, and
add tests for the prefix behavior.

### Changes Required:

#### 15. `proxy/web/handlers.go` — prefix LogsLink and EditLink

**File**: `proxy/web/handlers.go`

**Intent**: Prefix the two Go-side URL constructions with `h.BasePath`.

**Contract**:
- Line 322: `LogsLink: fmt.Sprintf("/logs?mapping=%s&provider=%s", ...)` →
  `h.BasePath + fmt.Sprintf("/logs?mapping=%s&provider=%s", ...)`
- Line 1041: `EditLink: "/providers?provider=" + url.QueryEscape(name)` →
  `h.BasePath + "/providers?provider=" + url.QueryEscape(name)`

#### 16. `proxy/web/handlers.go` — SetupMux signature change

**File**: `proxy/web/handlers.go:147`

**Intent**: `SetupMux` now takes `basePath string` as second parameter, calls
`SetBasePath(basePath)`, and wraps the mux with StripPrefix.

**Contract**:
```go
func SetupMux(h *eventstream.Handlers, basePath string, logger *slog.Logger) *http.ServeMux {
    SetBasePath(basePath)
    mux := http.NewServeMux()
    // ... all existing route registrations unchanged ...
    if basePath != "" {
        return http.StripPrefix(basePath, mux)
    }
    return mux
}
```
Wait — `http.StripPrefix` returns an `http.Handler`, not `*http.ServeMux`.
`NewServer` stores the handler in `http.Server.Handler` (which accepts any
`http.Handler`). The CSRF/auth wrappers already accept `http.Handler`. So
returning `*http.ServeMux` is not strictly required, but `SetupMux` currently
returns `*http.ServeMux`. To minimize changes, we can keep the return type
as `*http.ServeMux` and do the StripPrefix wrapping inside `NewServer`
instead of `SetupMux`.

**Revised contract**: `SetupMux` keeps its current signature (takes `h`
already has `BasePath`). `NewServer` reads `h.BasePath`, calls
`SetBasePath`, and wraps the mux:
```go
mux := SetupMux(h, logger)
if h.BasePath != "" {
    handler = http.StripPrefix(h.BasePath, mux)
}
```
This keeps `SetupMux` simple and the StripPrefix wrapping in `server.go:38`.

#### 17. Tests — Go unit tests

**Files**: `proxy/web/handlers_test.go`, `proxy/web/server_test.go`

**Intent**: Verify prefix behavior with `httptest`.

**Contract**:
- `TestBasePathPrefix`: Start a test server with `FREEDIUS_UI_PATH=/test`.
  `GET /test/` returns 200 with dashboard HTML containing prefixed links.
  `GET /test/static/app.css` returns 200. `GET /health` (no prefix) returns 404.
- `TestNoBasePathBackwardCompat`: With empty `FREEDIUS_UI_PATH`, `GET /`
  works as before (existing `TestPageHandlers` covers this).

#### 18. Tests — update existing test helpers

**File**: `proxy/web/handlers_test.go:15`

**Intent**: `newTestMux()` calls `SetupMux(h, logger)` — signature unchanged,
so existing tests pass. Add a new `newTestMuxWithBase(basePath string)` helper.

**Contract**: Add:
```go
func newTestMuxBase(basePath string) http.Handler {
    SetBasePath(basePath)
    h := newTestHandlers()
    mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
    if basePath != "" {
        return http.StripPrefix(basePath, mux)
    }
    return mux
}
```

## Phase 4: E2E Tests

### Overview

Add Playwright E2E tests that run the server behind a base path prefix.

### Changes Required:

#### 19. E2E — base path config fixture

**File**: `e2e/fixtures/` (new file or modify test-config.yaml)

**Intent**: Create a config or test variant that starts the server with
`FREEDIUS_UI_PATH=/freedius`.

**Contract**: Add `--ui-path /freedius` to the webServer command in
`playwright.config.ts`, or create a separate test config.

#### 20. E2E — base path spec

**File**: `e2e/tests/base-path.spec.ts`

**Intent**: Verify all links are prefixed when `FREEDIUS_UI_PATH` is set.

**Contract**:
- `page.goto('/freedius/')` loads dashboard
- Check that nav links contain `/freedius/mappings`, `/freedius/providers`
- Check that `<link>` and `<script>` tags have `/freedius/static/`
- Check `sse-connect` attribute is `/freedius/v1/events`
- Click "Mappings" nav link → URL becomes `/freedius/mappings`

## Testing Strategy

### Unit Tests:
- `TestBasePathPrefixedRouting` — StripPrefix middleware serves prefixed routes
- `TestBaseURLTemplateFunc` — the `baseURL` function with empty, `/freedius`, `/freedius/`
- `TestGoSideLinkGeneration` — `LogsLink` and `EditLink` include `h.BasePath`
- `TestNoBasePathBackwardCompat` — empty path = no prefix (all existing tests)

### Integration Tests (E2E):
- Dashboard loads at `/freedius/`
- All static assets load at `/freedius/static/`
- HTMX endpoints work at `/freedius/v1/...`
- SSE connects to `/freedius/v1/events`
- Navigation links all contain the prefix

### Manual Testing Steps:
1. `FREEDIUS_UI_PATH=/freedius mage run`
2. Open `http://127.0.0.1:8083/freedius/` — dashboard renders
3. Click "Mappings" — URL is `/freedius/mappings`, page loads
4. Click a mapping row — drawer loads via `POST /freedius/v1/mappings/...`
5. Open browser devtools Network tab — SSE connects to `ws:/freedius/v1/events`
6. Check "Edit on Mappings page" link points to `/freedius/mappings?search=...`
7. Repeat without `FREEDIUS_UI_PATH` — everything works at root as before

## Performance Considerations

- No performance impact when `basePath` is empty (no-op path: StripPrefix not
  applied, template function returns path unchanged).
- `http.StripPrefix` adds one string operation per request when prefix is set —
  negligible.
- Template function is a simple string concatenation — no measurable overhead.

## Migration Notes

- **Backward compatible**: Default is empty string (no prefix). Existing
  deployments without `FREEDIUS_UI_PATH` work identically to today.
- **No config file migration**: The setting is env/flag-based, not in
  `config.yaml`.
- **No database changes**: Purely a routing/link-generation change.

## References

- Existing env var pattern: `cmd/freedius/main.go:96-97, 181-182, 455-477`
  (`resolveUIPort`, `resolveUIHost`)
- Web server setup: `proxy/web/server.go:27-63` (`NewServer`, middleware layering)
- Template functions: `proxy/web/embed.go:22-40` (`templateFuncs`)
- Go-side link generation: `proxy/web/handlers.go:322` (LogsLink),
  `handlers.go:1041` (EditLink)
- Template patterns: `proxy/web/templates/layout.html` (asset/nav links),
  `index.html` (HTMX + SSE), `logs.html` (hx-get + sse-connect)
- Inline JS URL construction: `mappings.html:64,121,162,230`,
  `providers.html:77,99`

## Progress

### Phase 1: Configuration & Middleware Wiring

#### Automated
- [x] 1.1 Add `BasePath` to `Handlers` struct — 06074f5
- [x] 1.2 Add `--ui-path` flag + `FREEDIUS_UI_PATH` env var in `main.go` — 06074f5
- [x] 1.3 `NewServer` wraps mux with StripPrefix when BasePath is set — 06074f5
- [x] 1.4 `SetupMux` calls `SetBasePath` for template function — 06074f5

#### Manual
- [ ] 1.5 `GET /test/health` returns 200, `GET /health` returns 404 with prefix

### Phase 2: Template Function + Link Prefixing

#### Automated
- [x] 2.1 Add `basePath` var + `SetBasePath` + `baseURL` template func in `embed.go` — bdc9915
- [x] 2.2 Prefix `layout.html` (assets, nav, meta tag) — bdc9915
- [x] 2.3 Prefix `index.html` (links, HTMX targets, SSE) — bdc9915
- [x] 2.4 Prefix `logs.html` (HTMX, SSE) — bdc9915
- [x] 2.5 Prefix `mappings.html` (form actions, inline JS) — bdc9915
- [x] 2.6 Prefix `providers.html` (form actions, inline JS) — bdc9915
- [x] 2.7 Prefix `mappings-routing-table.html` (filter hx-get) — bdc9915
- [x] 2.8 Prefix `providers-table.html` (actions, links) — bdc9915
- [x] 2.9 Prefix `mapping-drawer.html` (edit link) — bdc9915
- [x] 2.10 Add `window.FREEDIUS_BASE_PATH` inline script in layout — bdc9915

#### Manual
- [ ] 2.11 Verify all links are prefixed in browser with dev tools open

### Phase 3: Go-side Link Generation + Tests

#### Automated
- [x] 3.1 Prefix `LogsLink` in `handlers.go:322` — 06074f5
- [x] 3.2 Prefix `EditLink` in `handlers.go:1041` — 06074f5
- [x] 3.3 Add `newTestMuxBase` helper — ff2d2bd
- [x] 3.4 Add `TestBasePathPrefixedRouting` unit test — ff2d2bd
- [x] 3.5 Add `TestBaseURLTemplateFunc` unit test — ff2d2bd
- [x] 3.6 Add `TestGoSideLinkGeneration` unit test — ff2d2bd

#### Manual
- [ ] 3.7 `mage test` passes with `-race`

### Phase 4: E2E Tests

#### Automated
- [x] 4.1 Add E2E config or webServer command with `--ui-path /freedius` — 661795b
- [x] 4.2 Add `base-path.spec.ts` E2E test — 661795b

#### Manual
- [ ] 4.3 `mage e2e` passes with base path tests
