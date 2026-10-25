# Fix Relative Dashboard Links — Plan Brief

> Full plan: `context/changes/fix-relative-dashboard-links/plan.md`

## What & Why

The freedius dashboard hardcodes all links, static asset paths, HTMX endpoints,
and SSE URLs as root-relative absolute paths (`/`, `/mappings`, `/static/app.js`,
`/v1/events`). When reverse-proxied behind nginx at a sub-path like `/freedius/`,
the browser resolves these against the actual server root, breaking the entire
dashboard. This plan adds a configurable `--ui-path` / `FREEDIUS_UI_PATH` setting
so the dashboard works correctly behind any reverse-proxy mount point.

## Starting Point

The web server runs on port 8083 (separate from the proxy on 8082), configured
via `FREEDIUS_UI_HOST` / `FREEDIUS_UI_PORT` env vars and `--ui-host` / `--ui-port`
flags. The mux in `proxy/web/handlers.go:147` uses Go 1.22 pattern matching with
no path-prefix mechanism. Templates are bundled with `go:embed` and reference
root-relative paths directly.

## Desired End State

Setting `FREEDIUS_UI_PATH=/freedius` (or `--ui-path /freedius`) makes
`GET /freedius/` serve the dashboard, `GET /freedius/static/app.css` serve assets,
all HTMX actions target `/freedius/v1/...`, and SSE subscriptions connect to
`/freedius/v1/events`. With the env var unset, behavior is identical to today.

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Config mechanism | Env var + CLI flag | Matches existing FREEDIUS_UI_HOST/UI_PORT pattern exactly | Plan |
| Default | Empty string (root-relative) | Zero config change for existing deployments; fully backward compatible | Plan |
| Routing | http.StripPrefix middleware | Existing Go 1.22 routes stay unchanged; minimal blast radius | Plan |
| Link generation | Template function + meta tag | Single `baseURL` func prefixes HTML attrs; `<meta>` tag exposes prefix to JS | Plan |
| Inline JS | `window.FREEDIUS_BASE_PATH` global | JS constructs URLs dynamically at runtime; needs runtime access to prefix | Plan |
| Static assets | Same prefix as everything else | Single coherent URL space; nginx proxies the whole subtree | Plan |
| Config file field | Not used | UI settings live in env vars, not config.yaml | Plan |

## Scope

**In scope:**
- `FREEDIUS_UI_PATH` env var + `--ui-path` flag
- `http.StripPrefix` middleware in `NewServer`
- `baseURL` template function in `embed.go`
- All 9 templates (`layout.html`, `index.html`, `logs.html`, `mappings.html`,
  `providers.html`, `mappings-routing-table.html`, `providers-table.html`,
  `mapping-drawer.html`, `provider-drawer.html`)
- `app.js` (if URL construction needed — currently doesn't construct URLs)
- Go-side link generation in `handlers.go` (LogsLink, EditLink)
- Unit tests + E2E tests

**Out of scope:**
- X-Forwarded-Path header auto-detection
- Config file field in `config.yaml`
- Runtime reconfiguration / hot-reload of base path
- Per-route or per-tenant base paths

## Architecture / Approach

```
Browser → /freedius/mappings → StripPrefix → mux → handler
Browser → /freedius/static/app.css → StripPrefix → mux → serveStatic
Browser → /freedius/v1/events → StripPrefix → mux → SSE handler

Template rendering:
  {{baseURL "/mappings"}} → "/freedius/mappings" or "/mappings"
  <meta name="base-path" content="{{baseURL ""}}"> → "/freedius" or ""
  JS: window.FREEDIUS_BASE_PATH + "/v1/providers/" + name
```

One package-level `basePath` variable is set once at startup via `SetBasePath`.
A `baseURL` template function prepends it to every path. A `<meta>` tag in
`layout.html` exposes it to inline JS. Go-side link generation uses
`h.BasePath` on the `Handlers` struct.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1 | Config wiring + StripPrefix middleware | Middleware layering (auth/CSRF wrapping StripPrefix) |
| 2 | Template function + all 9 templates prefixed | Missing a hardcoded URL in inline JS |
| 3 | Go-side link generation + unit tests | Test coverage gaps for the template function |
| 4 | E2E tests | Test config complexity for prefixed server |

**Prerequisites:** Existing `mage test` / `mage run` working baseline.
**Estimated effort:** ~2-3 focused sessions across 4 phases.

## Open Risks & Assumptions

- Inline JS in `mappings.html` and `providers.html` constructs URLs via string
  concatenation — all call sites must be found and updated to use
  `window.FREEDIUS_BASE_PATH`.
- `app.js` currently has no URL construction, but must be verified.
- Go 1.22 `ServeMux` pattern matching must still work after StripPrefix strips
  the prefix (it does — `http.StripPrefix` rewrites `r.URL.Path` and `r.URL.RawPath`).

## Success Criteria (Summary)

- `FREEDIUS_UI_PATH=/freedius mage run` — dashboard fully functional at
  `http://127.0.0.1:8083/freedius/`
- All links, assets, HTMX, and SSE use `/freedius/...` prefix
- Without the env var, behavior is identical to today (backward compatible)
- `mage test` and `mage e2e` pass
