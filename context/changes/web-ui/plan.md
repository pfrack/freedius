---
date: 2026-07-02
planner: freedius-plan-agent
git_commit: 6d82093
branch: streaming-edge-cases
repository: freedius
change_id: web-ui
title: Replace Bubble Tea TUI with embedded web UI
status: planned
last_updated: 2026-07-02
last_updated_by: freedius-plan-agent
---

# Replace TUI with Web UI — Implementation Plan

## Overview

Replace the in-process Bubble Tea dashboard (`proxy/tui/`) with an
embedded Go stdlib web server (`html/template` + vendored htmx) bound
to `FREEDIUS_UI_PORT` (default `8083`) alongside the existing proxy
port (`8082`). The proxy stays unchanged; logs go to stderr by default;
the web UI replaces the TUI as the live observability surface so
freedius runs cleanly in Docker / headless environments.

## Current State Analysis

**What exists and is reusable:**

- `proxy.LogSink` ([`proxy/logtee.go:1-260`](../logtee.go)) — bounded
  channel + 10k ring, replay via `SnapshotSince(seq)`. Perfect SSE feed.
- `proxy.EventBus` ([`proxy/eventbus.go:1-180`](../eventbus.go)) — same
  shape for request events.
- SSE handlers in [`cmd/freedius/ipc_unix.go:106-223`](../cmd/freedius/ipc_unix.go)
  — already use `json.Marshal` only (per `lessons.md §1`). Lift verbatim.
- `config.Config` API ([`config/config.go:117-160`](../config/config.go))
  — `Lock/Unlock/RLock/RUnlock`, `ProvidersSnapshot/MappingsSnapshot`,
  `Marshal`, `SaveData`. Mirrors TUI's `submitForm` discipline.
- Graceful shutdown via `signal.NotifyContext`.
- Tests use `httptest.NewRecorder` + table-driven + slog with
  `io.Discard` ([`proxy/proxy_test.go:1-50`](../proxy/proxy_test.go),
  [`proxy/middleware_test.go:1-30`](../proxy/middleware_test.go)).
- Error JSON format: `{"error": "code", "message": "..."}` with
  `Content-Type: application/json`.

**What's missing:**

- No web server in the binary; no templates; no embed.FS for assets.
- No Dockerfile (`context/foundation/roadmap.md:50` calls this out).
- `magefiles/mage.go:75-78` lists `dockerBuild` / `dockerRun` /
  `dockerPush` targets that are **not implemented**.
- TUI is TTY-coupled: `tea.NewProgram`, AltScreen, mouse mode,
  `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` — cannot run in
  Docker.

**Constraints discovered:**

- Project must stay single static binary with zero runtime deps
  ([`AGENTS.md`](../../AGENTS.md)). htmx vendored via `embed.FS`, not
  a Go module.
- `lessons.md §1` is load-bearing: SSE handlers MUST use
  `json.Marshal`, never `json.NewEncoder`.
- Go 1.26.4 → `embed.FS`, `slog`, `http.ServeMux` pattern matching,
  `signal.NotifyContext` all available.

## Desired End State

Running `freedius` (no flags) in any environment — local terminal,
Docker container, headless server — produces:

1. Plain text logs streamed to stderr (visible via `docker logs`,
   visible in the terminal).
2. HTTP proxy listening on `:8082` for Claude Code traffic.
3. Web dashboard at `http://localhost:8083/` showing live request
   events and logs via SSE, with read-write provider/mapping
   management via htmx forms.
4. Single static binary (`mage build` → `freedius`, no runtime deps).
5. `docker build -t freedius . && docker run -p 8082:8082 -p 8083:8083
   -e OPENCODE_API_KEY freedius` works end-to-end.

### Key Discoveries

- The IPC server is the prototype web server. Refactoring its four
  handlers into a transport-agnostic package is the single biggest
  leverage point — web UI mounts the same handlers, zero
  behavioral duplication.
- TUI's `submitForm` rollback pattern
  ([`proxy/tui/model.go:617-768`](../proxy/tui/model.go)) ports 1:1 to
  HTTP handlers: decode → validate → Lock → mutate → Marshal →
  SaveData → Unlock; rollback (delete the just-added entry) on any
  failure.
- `FREEDIUS_UI_TOKEN` opt-in middleware (~30 lines) is enough for
  Docker / LAN exposure — no per-user auth needed for a local proxy.
- Auth must gate **all** routes when set, not just writeback —
  logs can leak upstream API keys via error messages.

## What We're NOT Doing

- Per-user authentication (single-user local tool; opt-in shared
  secret is the documented security model).
- Theme picker (CSS `@media (prefers-color-scheme)` handles dark/light
  automatically; 5-theme port is unnecessary for the web).
- Shell-RC install button (irrelevant in Docker / non-interactive use).
- Detailed provider health monitoring (the proxy already has metrics;
  the dashboard surfaces the basics — total requests, error count,
  uptime — but no deep per-provider health).
- HTTP/2 push, gzip, or other transport-level tuning (premature for
  a local dashboard; can layer on later).
- Persistent log storage (logs are ring-buffered in memory; archive
  to disk is a separate change).
- WebSocket-based live updates (SSE is simpler and unidirectional,
  which matches our use case).

## Implementation Approach

Four phases, each independently shippable:

1. **Refactor + scaffold** — no user-visible change yet; extract SSE
   handlers, add web package skeleton + htmx + layout, add flags.
   TUI still default.
2. **Read-only web UI + auth wiring** — flip default to web+stderr,
   ship the three read-only pages with SSE tails, enforce
   `FREEDIUS_UI_TOKEN` when set. TUI kept behind `--tui` flag for one
   release.
3. **Writeback** — POST/PUT/DELETE for providers and mappings with
   validation + rollback; htmx forms surface field-level errors.
4. **TUI + IPC removal + Docker** — drop `proxy/tui/`, drop the Unix
   socket IPC + daemon + attach, drop `charm.land/*` from go.mod,
   add `Dockerfile` (multi-stage, distroless), implement the missing
   `mage dockerBuild` / `dockerRun` / `dockerPush` targets, add
   `docker-compose.yml` example.

## Critical Implementation Details

These are non-obvious constraints the implementer must respect;
they're not derivable from the file paths alone.

- **SSE encoding (`lessons.md §1`)** — every SSE write MUST use
  `json.Marshal(buf)` then `fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ...)`.
  Never `json.NewEncoder(w).Encode(v)` (it appends `\n`, breaking
  framing). The refactored handlers in `internal/eventstream/`
  inherit this; do not "modernize" them.

- **Auth constant-time compare** — the `FREEDIUS_UI_TOKEN` middleware
  MUST use `crypto/subtle.ConstantTimeCompare` (or equivalent), not
  `==`. Token mismatch is a known-side-channel risk even on localhost.

- **Static-binary build flags for Docker** — the Dockerfile MUST build
  with `CGO_ENABLED=0 -tags netgo,osusergo -ldflags="-s -w"`. Without
  `netgo,osusergo`, the resulting binary needs glibc and breaks in
  distroless / scratch base images. Without `-s -w`, the binary is
  ~30% larger.

- **Config rollback on save failure** — the writeback handlers MUST
  follow the exact pattern from TUI `submitForm`: capture `old` state
  before mutation, mutate, Marshal, SaveData; on any error, restore
  `old` before unlocking. Do not just unlock and return error — the
  in-memory config must stay consistent with what would have been on
  disk if the save had succeeded.

- **htmx SSE bundle** — Phase 1 vendored file
  `proxy/web/static/htmx.min.js` is `htmx.org@2.0.4` core concatenated
  with `htmx.org-ext-sse@2.2.2` sse extension. Pinned versions; do
  not "upgrade" without re-verifying the SHA256 in `htmx.min.js.sha256`.

- **`embed.FS` path conventions** — embedded paths are relative to
  the file containing the `//go:embed` directive. Templates live at
  `templates/<name>.html` (no `web/` prefix in the embed). The
  template loader strips no prefix.

- **Drop `--fg` and `--daemon` only in Phase 4** — keeping them in
  Phases 1-3 gives users a transition period; removing them is part
  of the breaking-change phase so it's all in one release.

## Phase 1: Refactor + scaffold

### Overview

Extract SSE handlers into a transport-agnostic package, scaffold the
new `proxy/web/` package with templates + vendored htmx + layout,
wire `--ui-port` and `FREEDIUS_UI_TOKEN` env (auth not enforced yet).
No user-visible behavior change: TUI still default; web server starts
inert on `:8083` alongside TUI for early dogfooding.

### Changes Required:

#### 1.1 New: `internal/eventstream/handlers.go`

**File**: `internal/eventstream/handlers.go` (new)

**Intent**: Transport-agnostic SSE/JSON handlers, mountable on any
`http.Handler`. Lifts the four handlers from
[`cmd/freedius/ipc_unix.go:106-223`](../cmd/freedius/ipc_unix.go)
so they can serve both the Unix-socket IPC (until Phase 4 deletes it)
and the new web server. Preserves `lessons.md §1` discipline.

**Contract**: Package `eventstream` exports:
- `type Handlers struct { Bus *proxy.EventBus; LogSink *proxy.LogSink; Cfg *config.Config; Registry *proxy.Registry; Host string; Port int; StartTime time.Time; AuthToken string }`
- `(h *Handlers) Register(mux *http.ServeMux)` — mounts
  `GET /v1/events`, `GET /v1/logs`, `GET /v1/stats`, `GET /v1/config`.
  When `AuthToken != ""`, all routes are wrapped by `requireAuth`
  middleware (constant-time compare).
- Route semantics: same as current `IPCServer` — SSE replay via
  `?since=N`, `event: replay` frame on eviction, `event: event` /
  `event: log` for live items. `json.Marshal` only (never
  `json.NewEncoder`).

#### 1.2 Modify: `cmd/freedius/ipc_unix.go`

**File**: `cmd/freedius/ipc_unix.go`

**Intent**: Replace inline handler bodies with a call to
`eventstream.Handlers{...}.Register(mux)`. Reduce duplication; keep
the Unix-socket IPC server working through Phase 4.

**Contract**: `IPCServer` keeps `socketPath`, `listener`, `server`,
`ListenAndServe`, `Shutdown`, but the mux is now
`http.NewServeMux(); h.Register(mux)` where `h` is built once at
construction time. Remove `handleEvents`, `handleLogs`, `handleStats`,
`handleConfig`, `writeSSE` methods.

#### 1.3 New: `cmd/freedius/ipc_windows.go`

**File**: `cmd/freedius/ipc_windows.go` (Windows build stub)

**Intent**: Keep the Windows IPC stub consistent with the refactored
shape. May be a no-op or delegate to `eventstream.Handlers`.

**Contract**: Build-tagged `//go:build windows`; maintains existing
external behavior.

#### 1.4 New: `proxy/web/server.go`

**File**: `proxy/web/server.go` (new package)

**Intent**: `WebServer` struct + lifecycle (`ListenAndServe` /
`Shutdown`) mirroring `IPCServer`'s shape, bound on `--ui-port`.

**Contract**:
- `type WebServer struct { port int; host string; listener net.Listener; server *http.Server; logger *slog.Logger }`
- `NewWebServer(host string, port int, handlers *eventstream.Handlers, cfg *config.Config, registry *proxy.Registry, logger *slog.Logger) *WebServer`
- `(ws *WebServer) ListenAndServe() error` — binds, builds the mux,
  registers page handlers + `handlers.Register`, calls
  `ws.server.Serve(ws.listener)`.
- `(ws *WebServer) Shutdown(ctx context.Context) error` — graceful
  shutdown via `http.Server.Shutdown`.
- Mux mounts: `GET /` → `handleIndex`; `GET /logs` → `handleLogs`;
  `GET /providers` → `handleProviders`; `GET /mappings` →
  `handleMappings`; `GET /static/*` → `serveStatic` (from
  `embed.FS`); `GET /health` → reuse the existing `{"status":"ok"}`
  handler from `main.go:415-419`; the four eventstream routes via
  `handlers.Register`.

#### 1.5 New: `proxy/web/embed.go`

**File**: `proxy/web/embed.go`

**Intent**: Single `embed.FS` for templates + static assets.

**Contract**:
```
//go:embed templates static
var assets embed.FS

func loadTemplates() (*template.Template, error)
```
`loadTemplates` parses `templates/layout.html` first, then all other
`templates/*.html`, with `template.ParseFS`. Returns a single
`*template.Template` for `ExecuteTemplate(w, "layout.html", ...)`.

#### 1.6 New: `proxy/web/handlers.go`

**File**: `proxy/web/handlers.go`

**Intent**: HTML page handlers that render the layout with
page-specific snapshot data. Phase 1 ships inert placeholders
(headers + nav only); Phase 2 fills in real content.

**Contract**:
- `func handleIndex(w http.ResponseWriter, r *http.Request)`
- `func handleLogs(w http.ResponseWriter, r *http.Request)`
- `func handleProviders(w http.ResponseWriter, r *http.Request)`
- `func handleMappings(w http.ResponseWriter, r *http.Request)`
- `func serveStatic(w http.ResponseWriter, r *http.Request)` — serves
  from `assets` subdirectory; sets `Cache-Control: public, max-age=300`.
- All page handlers call `t.ExecuteTemplate(w, "layout.html", pageData)`
  where `pageData` includes `Active string` for nav highlighting.
- Phase 1 `pageData` is minimal: just `Active` and a placeholder
  message; real snapshots come in Phase 2.

#### 1.7 New: `proxy/web/templates/layout.html`

**File**: `proxy/web/templates/layout.html`

**Intent**: Base layout: shared `<head>`, nav, footer.

**Contract**: tmpl name `layout.html`. Defines
`{{define "title"}}{{end}}`, `{{define "content"}}{{end}}`,
`{{define "scripts"}}{{end}}`. Nav links to `/logs`, `/providers`,
`/mappings`. Includes `/static/htmx.min.js` and `/static/app.css`.

#### 1.8 New: `proxy/web/templates/logs.html`, `providers.html`, `mappings.html`

**File**: `proxy/web/templates/{logs,providers,mappings}.html`

**Intent**: Phase-1 placeholder pages that define only the
`content` block with a "Coming in Phase 2" message.

**Contract**: Each defines `{{define "content"}}...{{end}}` rendering
a single `<h1>` with the page name + a placeholder paragraph.

#### 1.9 New: `proxy/web/static/htmx.min.js`

**File**: `proxy/web/static/htmx.min.js`

**Intent**: Vendored htmx core + sse extension, single file served
via embed.FS.

**Contract**: Concatenation of
`htmx.org@2.0.4/dist/htmx.min.js` and
`htmx.org-ext-sse@2.2.2/sse.js`. Pinned versions. Sidecar file
`htmx.min.js.sha256` records `sha256sum` output for drift detection.

#### 1.10 New: `proxy/web/static/app.css`

**File**: `proxy/web/static/app.css`

**Intent**: Minimal Phase-1 stylesheet — just enough to look like a
dashboard (font, padding, nav layout, dark/light variables).

**Contract**: Defines CSS variables for color palette;
`@media (prefers-color-scheme: dark)` overrides. Phase 2 adds table
+ log entry styles; Phase 3 adds form/modal styles.

#### 1.11 Modify: `cmd/freedius/main.go`

**File**: `cmd/freedius/main.go`

**Intent**: Add `--ui-port` flag + `FREEDIUS_UI_PORT` env, read
`FREEDIUS_UI_TOKEN`, start `WebServer` alongside the proxy, build
`eventstream.Handlers` once and share.

**Contract**: New declarations in `run()` after `flagNoExportHint`:
- `flagUIPort := fs.Int("ui-port", 0, "web UI port (overrides FREEDIUS_UI_PORT; default 8083)")`
- `flagUIHost := fs.String("ui-host", "", "web UI bind address (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")`
- After proxy `serverErr` channel setup, build
  `handlers := &eventstream.Handlers{Bus: bus, LogSink: logSink, Cfg: cfg, Registry: registry, Host: host, Port: port, StartTime: time.Now(), AuthToken: os.Getenv("FREEDIUS_UI_TOKEN")}`.
- New `webServer := NewWebServer(uiHost, uiPort, handlers, cfg, registry, logger)`;
  start it in a goroutine; add its error to a shared `serverErr`-like
  channel.
- Add `resolveUIPort(flagVal int, flagSet bool) int` mirroring
  `resolveInt`.
- Add `defaultUIHost = "127.0.0.1"` constant.
- TUI default path unchanged in Phase 1.

#### 1.12 Modify: `cmd/freedius/printUsage`

**File**: `cmd/freedius/main.go`

**Intent**: Surface `--ui-port` and `--ui-host` in `--help`.

**Contract**: Add `fs.Int("ui-port", 0, ...)` and
`fs.String("ui-host", "", ...)` declarations to the rebuild-FlagSet
in `printUsage`.

#### 1.13 Modify: `cmd/freedius/main_test.go`

**File**: `cmd/freedius/main_test.go`

**Intent**: Cover `resolveUIPort` + `FREEDIUS_UI_TOKEN` env read.

**Contract**: Add `TestResolveUIPort` table-driven (flag set, env
set, both, neither, invalid range). Add `TestAuthTokenEnv` single
test asserting env round-trip.

#### 1.14 New: `proxy/web/handlers_test.go`

**File**: `proxy/web/handlers_test.go`

**Intent**: Smoke tests for the four page handlers + `serveStatic`.

**Contract**: 5 table-driven tests using `httptest.NewRecorder`;
assert 200, `Content-Type: text/html; charset=utf-8`, presence of
expected `<nav>` element + active-page class in body.

#### 1.15 New: `proxy/web/embed_test.go`

**File**: `proxy/web/embed_test.go`

**Intent**: Catch missing assets at test time.

**Contract**: 1 test asserting
`assets.Open("static/htmx.min.js")` and
`assets.Open("templates/layout.html")` both succeed; loadTemplates
returns no error; rendered `layout.html` contains `/static/htmx.min.js`.

#### 1.16 New: `proxy/web/server_test.go`

**File**: `proxy/web/server_test.go`

**Intent**: End-to-end lifecycle smoke test.

**Contract**: 1 test that uses `net.Listen("127.0.0.1:0")` for a
random port, calls `ListenAndServe`, hits `GET /health`, asserts
200, calls `Shutdown(ctx)`, asserts no error.

#### 1.17 New: `internal/eventstream/handlers_test.go`

**File**: `internal/eventstream/handlers_test.go`

**Intent**: Lift-and-shift coverage of the four SSE/JSON handlers
with replay + live + eviction cases.

**Contract**: Table-driven tests per handler:
- Replay: emit N events/logs, GET `/v1/events?since=0`, assert
  SSE framing `event: event\ndata: {…}\n\n` and `event: replay\n…`
- Live: open request, emit one more event, assert it streams through
  before context cancellation.
- Eviction: GET `?since=<very-old-seq>`, assert first frame is
  `event: replay\ndata: {"complete":false,"current_seq":N}`.
- `json.Marshal`-only verification: regex check that no frame has
  triple-newline (the bug `lessons.md §1` warns against).

### Success Criteria:

#### Automated Verification:

- `mage vet` passes
- `mage lint` passes
- `mage test` passes (new tests cover handlers, embed, server
  lifecycle, env reads, SSE framing, eviction)
- `mage build` produces the same `freedius` binary shape
- `mage modVerify` passes
- `mage tidyCheck` passes
- `mage govulncheck` passes

#### Manual Verification:

- `mage run` starts the TUI (default) AND logs `web dashboard on
  http://127.0.0.1:8083` to stderr.
- Browser at `http://localhost:8083/` shows the layout with the
  three-tab nav and placeholder content.
- `mage run -- --ui-port 9090` binds to 9090; `curl
  http://localhost:9090/health` returns `{"status":"ok"}`.
- TUI still functions identically (no regression).
- `curl http://localhost:8083/static/htmx.min.js` returns the
  vendored file (not 404).

---

## Phase 2: Read-only web UI + auth wiring

### Overview

Flip default: no TUI; logs go to stderr; web server always up. Ship
the three read-only pages (`/logs`, `/providers`, `/mappings`) with
SSE tails and snapshot rendering. Wire the optional
`FREEDIUS_UI_TOKEN` middleware to all routes. TUI survives behind
`--tui` flag for one release.

### Changes Required:

#### 2.1 Modify: `cmd/freedius/main.go`

**File**: `cmd/freedius/main.go`

**Intent**: Flip the default log destination to stderr; remove TUI
as default startup; keep TUI behind `--tui`.

**Contract**:
- Replace `logWriter := io.Discard` (line ~159) with
  `logWriter := os.Stderr` unconditionally.
- Add `flagTUI := fs.Bool("tui", false, "enable terminal dashboard (default: web UI)")`.
- In `run()`, after server starts: if `*flagTUI`, run existing TUI
  startup block; otherwise skip directly to graceful shutdown of
  proxy + web servers only.
- Update `--help` and the `printUsage` FlagSet.

#### 2.2 Modify: `proxy/web/templates/logs.html`

**File**: `proxy/web/templates/logs.html`

**Intent**: Real log page — server-renders recent entries from the
ring buffer, then htmx SSE appends new ones.

**Contract**: Layout's `content` block renders:
- A level filter dropdown (`<select name="min" hx-get="/logs"
  hx-target="#log" hx-trigger="change">` with options `all`,
  `debug`, `info`, `warn`, `error`).
- A `<div id="log" hx-ext="sse" sse-connect="/v1/logs"
  sse-swap="log" hx-swap="beforeend scroll:#log:bottom">` with
  server-rendered initial block of the last 200 log entries
  (`LogSink.SnapshotSince(0)`, capped to 200 most recent).
- Each entry as `<pre class="log-{level}">{line}</pre>`.
- Layout's `scripts` block defines the htmx extension registration.

#### 2.3 Modify: `proxy/web/templates/providers.html`

**File**: `proxy/web/templates/providers.html`

**Intent**: Read-only table of all configured providers.

**Contract**: Layout's `content` block renders:
- A `<table>` with header columns: name, behavior, base_url,
  default_api_key_env, protocol, mapping_count.
- One row per `Config.ProvidersSnapshot()` entry.
- mapping_count computed by ranging over `Config.MappingsSnapshot()`
  and counting those where `Mapping.ProviderName == name`.

#### 2.4 Modify: `proxy/web/templates/mappings.html`

**File**: `proxy/web/templates/mappings.html`

**Intent**: Read-only table of all configured mappings.

**Contract**: Layout's `content` block renders:
- A `<table>` with header columns: name, provider, model.
- One row per `Config.MappingsSnapshot()` entry.

#### 2.5 Modify: `proxy/web/templates/layout.html`

**File**: `proxy/web/templates/layout.html`

**Intent**: Active-page highlighting; final touches.

**Contract**: Each nav anchor gets
`class="{{ if eq .Active "logs" }}active{{end}}"` etc. Add
`<meta name="viewport" content="width=device-width, initial-scale=1">`
for mobile.

#### 2.6 Modify: `proxy/web/handlers.go`

**File**: `proxy/web/handlers.go`

**Intent**: Real handlers — render templates with snapshot data
from LogSink, EventBus, Config.

**Contract**:
- `handleLogs(w, r)` — parse `?min=`; filter
  `LogSink.SnapshotSince(0)` by `slog.Level`; render with
  `Active: "logs"`, `Entries: filtered`, `Level: filter.Label`.
- `handleProviders(w, r)` — render with `Active: "providers"`,
  `Providers: collectedProviders(cfg)`. Lift
  `collectProvidersFromConfig` from
  [`proxy/tui/views.go:208-233`](../proxy/tui/views.go).
- `handleMappings(w, r)` — render with `Active: "mappings"`,
  `Mappings: cfg.MappingsSnapshot()`.
- New helper `collectedProviders(cfg *config.Config) []providerInfo`
  in `proxy/web/types.go` (lift from tui with same struct shape).

#### 2.7 Modify: `proxy/web/static/app.css`

**File**: `proxy/web/static/app.css`

**Intent**: Table styles, log entry level colors, dark-mode polish.

**Contract**: CSS for `.log-info`, `.log-warn`, `.log-error`,
`.log-debug` (background tint per level); table borders + hover
row; `.active` nav highlight; monospace font for log entries.

#### 2.8 Modify: `internal/eventstream/handlers.go`

**File**: `internal/eventstream/handlers.go`

**Intent**: Implement `requireAuth` middleware that enforces
`AuthToken` when set.

**Contract**: Middleware function:
- When `h.AuthToken == ""`, returns the next handler unchanged.
- When set, reads `r.Header.Get("Authorization")`, strips
  `Bearer ` prefix, uses
  `subtle.ConstantTimeCompare([]byte(provided), []byte(h.AuthToken))`.
- On mismatch: writes
  `{"error":"unauthorized","message":"invalid or missing token"}`
  with `Content-Type: application/json` and `http.StatusUnauthorized`.
- Wrapped via
  `mux.Handle("GET /v1/events", requireAuth(http.HandlerFunc(s.handleEvents)))`
  etc.

#### 2.9 New: `proxy/web/log_filter_test.go`

**File**: `proxy/web/log_filter_test.go`

**Intent**: Cover server-side log level filtering.

**Contract**: Table-driven tests:
- No filter (`?min` empty) returns all entries.
- `?min=info` returns only info+.
- `?min=invalid` returns 400 with JSON error.

#### 2.10 New: `internal/eventstream/auth_test.go`

**File**: `internal/eventstream/auth_test.go`

**Intent**: Cover auth middleware.

**Contract**: 4 tests:
- No token configured → all requests pass (regardless of headers).
- Correct token → 200 + expected body.
- Wrong token → 401 + JSON `{"error":"unauthorized",…}`.
- Missing Authorization header → 401.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (new auth + filter tests + page rendering tests
  with populated config fixtures)
- `mage vet`, `mage lint`, `mage govulncheck` pass

#### Manual Verification:

- `mage run` (no flags) — logs on stderr, web UI at :8083.
- Browser: `/logs` shows recent entries from the ring buffer; new
  log lines stream in live via SSE.
- Browser: `/providers` lists all configured providers; `/mappings`
  lists all.
- Trigger a request through the proxy (`curl -X POST
  http://localhost:8082/v1/messages -d '{"model":"opus"}'`) and
  verify the SSE event stream in the browser shows it within ~1s.
- Set `FREEDIUS_UI_TOKEN=secret` and restart; browser requests
  without token get 401 JSON; with `Authorization: Bearer secret`
  work.
- `mage run -- --tui` (explicit) — TUI works as before.
- Run in Docker:
  `docker run --rm -p 8082:8082 -p 8083:8083 -e OPENCODE_API_KEY
  $OPENCODE_API_KEY <built-image>` — logs on `docker logs`,
  dashboard at `http://localhost:8083/`.

---

## Phase 3: Writeback (POST/PUT/DELETE)

### Overview

Add full CRUD for providers and mappings through the web UI. Forms
validate server-side; htmx surfaces field-level errors; mutations
follow the TUI `submitForm` rollback discipline. Auth middleware
(from Phase 2) is already in place.

### Changes Required:

#### 3.1 New: `proxy/web/forms.go`

**File**: `proxy/web/forms.go`

**Intent**: Reusable form-decoding + validation logic for both
provider and mapping forms.

**Contract**:
- `type ValidationError struct { Fields map[string]string }` —
  implements `error`; JSON-marshals as
  `{"error":"validation_failed","message":"...","fields":{"name":"required",…}}`.
- `func decodeProviderForm(r *http.Request) (name string, p config.Provider, err error)`.
- `func validateProviderName(name string) error` — rejects empty,
  CR/LF, colon.
- `func validateProviderBehavior(behavior string) error` — must be
  one of `openai`, `anthropic`, `mix`.
- `func validateProviderBaseURL(s string) error` — empty allowed;
  if non-empty, must parse as http(s) URL.
- `func validateProviderAPIKeyEnv(s string) error` — rejects empty
  OK; rejects CR/LF/`=`.
- `func validateProviderProtocol(s string) error` — empty allowed;
  if non-empty, must be `openai` or `anthropic`.
- `func validateProviderFields(name string, p config.Provider) *ValidationError` —
  aggregates all field errors.
- Same for mapping: `decodeMappingForm`,
  `validateMappingProviderExists`, `validateMappingFields`.
- Mirror the rules in
  [`proxy/tui/model.go:691-737`](../proxy/tui/model.go).

#### 3.2 Modify: `proxy/web/handlers.go`

**File**: `proxy/web/handlers.go`

**Intent**: Wire POST/PUT/DELETE for providers and mappings.

**Contract**:
- `POST /v1/providers`:
  1. `decodeProviderForm(r)` → name, p
  2. `validateProviderFields(name, p)` → 400 + JSON if any errors
  3. `cfg.Lock()`
  4. `old, hadOld := cfg.Providers[name]`
  5. `cfg.Providers[name] = p`
  6. `data, mErr := cfg.Marshal()`
  7. If `cfgPath != ""`: `saveErr := cfg.SaveData(cfgPath, data)`
  8. If any error: `delete(cfg.Providers, name)` (rollback);
     `cfg.Unlock()`; 400/500 JSON
  9. `cfg.Unlock()`; return 201 + `{"status":"created","name":name}`
- `PUT /v1/providers/{name}`:
  1. Parse `{name}` from path
  2. `cfg.RLock()`; `_, existed := cfg.Providers[name]`;
     `cfg.RUnlock()`; if not existed, 404
  3. Same flow as POST with: capture `old`, delete old, add new;
     rollback restores old on failure.
- `DELETE /v1/providers/{name}`:
  1. Parse `{name}`
  2. `cfg.RLock()`; check `cfg.UsesProvider(name)`;
     `cfg.RUnlock()`
  3. If used by any mapping, return 409 + JSON
     `{"error":"provider_in_use","message":"N mappings reference this provider"}`
  4. Same flow: lock, delete, marshal, save; rollback restores on
     failure.
- Same pattern for `/v1/mappings`, `/v1/mappings/{name}`,
  `DELETE /v1/mappings/{name}`. Mapping delete has no in-use check
  (mappings are leaves).
- Helper: `pathName(r *http.Request, prefix string) (string, error)`
  using `strings.TrimPrefix(r.URL.Path, prefix)`.

#### 3.3 Modify: `proxy/web/templates/providers.html`

**File**: `proxy/web/templates/providers.html`

**Intent**: Add/edit/delete forms via htmx.

**Contract**:
- "+ Add provider" button toggles a modal form (Phase 3 of CSS)
  with all six fields.
- Each row has "Edit" (opens modal pre-filled) and "Delete" (with
  `hx-confirm`).
- Forms POST/PUT to `/v1/providers` or `/v1/providers/{name}` and
  PUT/DELETE use the row's name in the URL.
- On success, htmx `hx-target="#providers"` `hx-swap="outerHTML"`
  re-renders the table from `/providers`.
- On error, htmx `hx-target="#provider-form-error" hx-swap="innerHTML"`
  shows the JSON errors.

#### 3.4 Modify: `proxy/web/templates/mappings.html`

**File**: `proxy/web/templates/mappings.html`

**Intent**: Same pattern for mappings.

**Contract**: 3-field form (name, provider dropdown sourced from
`Config.ProvidersSnapshot()`, model). Add/Edit/Delete same shape.

#### 3.5 Modify: `proxy/web/static/app.css`

**File**: `proxy/web/static/app.css`

**Intent**: Modal + form + error styles.

**Contract**: `.modal`, `.modal-backdrop`, `.modal-content`, `.form-error`,
`.btn-primary`, `.btn-danger`, `.btn-cancel`. Modal uses native
`<dialog>` element with `showModal()`/`close()` driven by ~10 lines
of vanilla JS.

#### 3.6 New: `proxy/web/forms_test.go`

**File**: `proxy/web/forms_test.go`

**Intent**: Cover form validation.

**Contract**: Table-driven:
- Happy path (all valid fields → no errors)
- Empty name → field error on `name`
- Name with CR/LF/colon → field error
- Behavior not in {openai, anthropic, mix} → field error
- base_url not http(s) → field error
- api_key_env with CR/LF/= → field error
- Protocol not in {openai, anthropic, ""} → field error
- Mapping with empty provider → field error
- Mapping with non-existent provider → field error
- Mapping model with CR/LF/colon → field error

#### 3.7 New: `proxy/web/handlers_write_test.go`

**File**: `proxy/web/handlers_write_test.go`

**Intent**: End-to-end writeback tests with rollback verification.

**Contract**: ~15 tests using `httptest.NewServer` with the full
mux; cases:
- POST creates a provider, config reflects it, SaveData called.
- POST with validation error → 400, no mutation, SaveData not called.
- POST with save failure (inject by passing a read-only `cfgPath`)
  → 500 + in-memory rollback (provider not present).
- PUT updates an existing provider; rollback on save failure.
- DELETE removes a provider; rollback on save failure.
- DELETE on a provider used by a mapping → 409 + not deleted.
- POST mapping with non-existent provider → 400.
- DELETE mapping not present → 404.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (~30 new test cases)
- `mage vet`, `mage lint`, `mage govulncheck` pass

#### Manual Verification:

- Browser: add a new provider through the UI; verify it appears in
  the list; verify `freedius.yaml` updated on disk.
- Edit a provider; verify changes persist across reload.
- Try to delete a provider that has an active mapping → 409 error
  shown in UI; provider still present.
- Add/edit/delete a mapping; verify persistence.
- With `FREEDIUS_UI_TOKEN` set, writeback without token → 401;
  with token → 200/201.
- Trigger a reload of the page after each writeback and verify
  changes survive.

---

## Phase 4: TUI + IPC removal + Docker

### Overview

Drop the Bubble Tea TUI entirely, drop the Unix-socket IPC + daemon
+ attach machinery, drop the `charm.land/*` deps, ship the
Dockerfile that motivates this change. Update docs.

### Changes Required:

#### 4.1 Delete: `proxy/tui/` directory

**File**: `proxy/tui/` (entire package removed)

**Intent**: Drop the TUI per user decision (Round 1 question).

**Contract**: `rm -rf proxy/tui/` removes ~12 files
(model.go, views.go, picker.go, styles.go, loglevel.go, help.go,
and `*_test.go` siblings). No imports elsewhere in the codebase
after Phase 1-3 changes.

#### 4.2 Delete: `cmd/freedius/ipc_unix.go`, `cmd/freedius/ipc_windows.go`

**File**: `cmd/freedius/ipc_unix.go`, `cmd/freedius/ipc_windows.go`

**Intent**: Drop the Unix-socket IPC server. No consumers after the
TUI removal.

**Contract**: Files deleted; no references in `main.go` after the
Phase 4 cleanup.

#### 4.3 Delete: `cmd/freedius/ipc_client.go`

**File**: `cmd/freedius/ipc_client.go`

**Intent**: Drop the IPC client (used only by `attach`).

**Contract**: File deleted.

#### 4.4 Delete: `cmd/freedius/daemon_unix.go`, `cmd/freedius/daemon_windows.go`

**File**: `cmd/freedius/daemon_unix.go`, `cmd/freedius/daemon_windows.go`

**Intent**: Drop the fork-to-background machinery. Docker and
process managers make this obsolete.

**Contract**: Files deleted.

#### 4.5 Delete: `cmd/freedius/attach.go`, `paths_unix.go`, `paths_windows.go`

**File**: `cmd/freedius/attach.go`, `cmd/freedius/paths_unix.go`, `cmd/freedius/paths_windows.go`

**Intent**: Drop the attach subcommand + runtime path helpers
(runtimeDir, socketPath, pidFilePath, lockFilePath all go away).

**Contract**: Files deleted.

#### 4.6 Delete: `cmd/freedius/signal_unix.go`, `cmd/freedius/signal_windows.go`

**File**: `cmd/freedius/signal_unix.go`, `cmd/freedius/signal_windows.go`

**Intent**: Drop TUI-specific signal handling. New code uses
stdlib `signal.NotifyContext`.

**Contract**: Files deleted.

#### 4.7 Modify: `cmd/freedius/main.go`

**File**: `cmd/freedius/main.go`

**Intent**: Final cleanup — remove all references to TUI / daemon /
attach / IPC.

**Contract**:
- Remove `flagTUI`, `flagFg`, `flagDaemon`, `flagDaemonShort`
  declarations + branches.
- Remove `handleStop`, `handleStatus`, `handleAttach`,
  `handleDaemonStart`, `startHeadlessWithIPC`, `runHeadless`
  functions.
- Simplify `run()` to: handleEarlyArgs → flag parse → log setup →
  config load → start proxy + web → waitForShutdown (which now
  closes both servers) → exit.
- `printUsage` is rewritten to show only `freedius [flags]` with
  the live flags (config, port, host, ui-port, ui-host,
  verbose-errors, log-format, stream-timeout, no-export-hint).
- Add a comment noting that `internal/eventstream/Handlers` is the
  shared handler for the web server (no longer the IPC server).

#### 4.8 Modify: `go.mod`, `go.sum`

**File**: `go.mod`, `go.sum`

**Intent**: Drop `charm.land/*` deps after TUI removal.

**Contract**: Run `mage tidy`; commit resulting diff. Verify
`go list -m all | grep charm.land` returns empty after build.

#### 4.9 New: `Dockerfile`

**File**: `Dockerfile`

**Intent**: Multi-stage static-binary build for Docker. Standard
two-stage layout with distroless base.

**Contract**:
```
# syntax=docker/dockerfile:1.7

FROM golang:1.26.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -tags netgo,osusergo \
    -ldflags="-s -w" \
    -trimpath \
    -o /out/freedius ./cmd/freedius

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/freedius /usr/local/bin/freedius
EXPOSE 8082 8083
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/freedius"]
```
Build tags and ldflags are load-bearing — see Critical
Implementation Details.

#### 4.10 New: `.dockerignore`

**File**: `.dockerignore`

**Intent**: Don't bloat Docker build context.

**Contract**: Excludes: `freedius`, `coverage.*`, `.git`, `.idea`,
`node_modules`, `context/`, `*.md`, `test-manual.sh`,
`.golangci.yaml`, `.work.keys`.

#### 4.11 New: `docker-compose.yml`

**File**: `docker-compose.yml`

**Intent**: One-command deployment example for new operators.

**Contract**: Single service `freedius`:
- `build: .`
- `ports: ["8082:8082", "8083:8083"]`
- `environment:` includes `OPENCODE_API_KEY`, `NVIDIA_NIM_API_KEY`
  (read from host env, no defaults), `FREEDIUS_HOST=0.0.0.0`,
  `FREEDIUS_UI_HOST=0.0.0.0` (so dashboard is reachable on the
  published port).
- `restart: unless-stopped`

#### 4.12 Modify: `magefiles/mage.go`

**File**: `magefiles/mage.go`

**Intent**: Implement the previously-stub `dockerBuild` /
`dockerRun` / `dockerPush` targets.

**Contract**:
- `DockerBuild() error` →
  `sh.RunV("docker", "build", "-t", "freedius:dev", ".")`
- `DockerRun() error` →
  `sh.RunV("docker", "run", "--rm", "-p", "8082:8082", "-p", "8083:8083", "--name", "freedius-dev", "freedius:dev")`
- `DockerPush() error` → reads `IMAGE_NAME` env (default
  `freedius:dev`), calls `sh.RunV("docker", "push", image)`
- Add to Help() output (already there, now wired)
- Add `DockerAll = mg.Deps{Build, DockerBuild}` style chain where
  appropriate.

#### 4.13 Modify: `cmd/freedius/main.go` `printUsage`

**File**: `cmd/freedius/main.go`

**Intent**: Show only the live flags + subcommands.

**Contract**: Rewrite to show: `freedius [flags]` + the full flag
list (no `attach`, `stop`, `status` subcommands, no `--fg`,
`--daemon`, `--tui` flags).

#### 4.14 Modify: `README.md`

**File**: `README.md`

**Intent**: Document web UI as primary surface + Docker as
deployment option. Remove references to `--fg`, `--daemon`,
`--tui`, `attach`, `stop`, `status`.

**Contract**: Rewrite of "Usage" section + new sections:
- `## Web Dashboard` (how to access, what it shows, screenshots
  deferred to a follow-up).
- `## Docker` (quickstart with the docker-compose.yml snippet).
- `## Configuration` (unchanged in shape, but mention that the
  web UI shows and edits the same YAML).

#### 4.15 Modify: `config.example.yaml`

**File**: `config.example.yaml`

**Intent**: Hint that the dashboard shows the config.

**Contract**: Add header comment line:
`# View and edit at http://localhost:8083/providers` (after the
existing `# freedius starter config` block).

### Success Criteria:

#### Automated Verification:

- `mage test` passes (no TUI/IPC test files remain; new tests cover
  TUI-free main.go flow)
- `mage vet`, `mage lint`, `mage govulncheck` pass
- `mage tidyCheck` passes (no go.mod churn after final removal)
- `mage build` produces a binary measurably smaller than pre-Phase-1
  (record delta in commit message — expect ~5-10 MB reduction from
  charm.land removal)
- `mage dockerBuild` succeeds and produces a working image
- `mage dockerRun` (or direct `docker run`) starts the container
  with logs visible via `docker logs`
- `mage tidy` shows `go.mod` no longer requires `charm.land/*`
  (verify with `go list -m all | grep -c charm.land` returns 0)

#### Manual Verification:

- `docker build -t freedius:dev .` succeeds
- `docker run --rm -p 8082:8082 -p 8083:8083 -e OPENCODE_API_KEY
  $OPENCODE_API_KEY freedius:dev` shows logs on `docker logs` and
  serves web UI at `http://localhost:8083`
- `docker compose up` works with the example compose file
- `freedius --tui` exits with an "unknown flag" error
- `freedius attach` exits with "unknown subcommand" error
- `freedius --fg` exits with "unknown flag" error
- Binary size reduced (capture in commit message; expect ~5-10 MB
  drop from `charm.land/*` removal)
- End-to-end smoke: open `http://localhost:8083`, add a provider
  through the UI, trigger a Claude Code request through `:8082`,
  see the request appear live on the dashboard within ~1s.

---

## Testing Strategy

### Unit Tests

- **Phase 1**: `internal/eventstream/handlers_test.go` (SSE framing,
  replay, eviction, JSON shape per `lessons.md §1`);
  `proxy/web/` page-handler smoke tests; `embed.FS` smoke test;
  server lifecycle test; env-read tests.
- **Phase 2**: auth middleware tests (correct token, wrong token,
  no token, missing header); log level filter tests.
- **Phase 3**: form validation tests (table-driven, ~10 cases per
  form type); writeback integration tests (~15 cases including
  rollback on save failure).
- **Phase 4**: final integration smoke covering the simplified
  `main.go` flow.

### Integration Tests

- Full `WebServer` + `proxy.Server` coexistence test (random ports).
- Round-trip test: add provider via UI → reload page → assert
  visible → trigger proxy request → assert event in SSE stream.
- Docker smoke (manual in Phase 4): build + run + curl `/health`
  + browser visit `/logs`.

### Manual Testing Steps

1. Run `mage run` (default web mode) — logs on stderr, dashboard
   at :8083.
2. Browser: switch between /logs, /providers, /mappings tabs.
3. Trigger a Claude Code request through :8082 — observe the
   event arrive on /logs within ~1s.
4. With `FREEDIUS_UI_TOKEN=secret` set — verify browser needs the
   token; curl with `-H 'Authorization: Bearer secret'` works.
5. Add a new provider through the UI; verify YAML file updated.
6. Delete a provider with active mapping — verify 409 error.
7. `docker run ...` and verify the same flow works inside a
   container.

## Performance Considerations

- **SSE replay**: ring buffer capped at 10k entries (existing
  constraint in `proxy/logtee.go:50`); no growth concern.
- **Template parsing**: at startup, once; cached in
  `*template.Template` (avoid per-request `ParseFiles`).
- **Hot path**: log sink → SSE channel is already non-blocking;
  the web layer adds zero overhead on the proxy hot path.
- **Static assets**: served from `embed.FS` (in-memory); no disk
  I/O on the hot path. `Cache-Control: public, max-age=300` set
  on `/static/*`.
- **Multiple SSE clients**: each connection consumes its own
  goroutine + channel slot; the bus/sink still broadcasts one copy
  of each event. Memory cost = N_clients × buffer_size × entry_size;
  bounded by the ring buffer policy.
- **Writeback rollback**: extra Marshal on the failure path only;
  happy path is one Lock + one Marshal + one SaveData.

## Migration Notes

- **Users on `--fg`**: drop the flag. New default is the same
  (headless foreground), just with a web server too.
- **Users on `--daemon`**: switch to `docker run -d` or a process
  manager. The fork-to-background pattern is obsolete.
- **Users on `freedius attach`**: use the web UI at `:8083` instead.
  There is no replacement for the Unix-socket IPC.
- **`FREEDIUS_UI_TOKEN`**: opt-in. Not required for localhost-only
  deployments. Set it when exposing the UI to a LAN or beyond.
- **Existing config files (YAML)**: work without modification.
  The web UI reads and writes the same shape.
- **IPC Unix socket**: no longer created. Any tooling that connected
  to it must switch to HTTP `/v1/events`, `/v1/logs`, `/v1/stats`,
  `/v1/config` endpoints (and pass `Authorization: Bearer <token>`
  if `FREEDIUS_UI_TOKEN` is set on the server).

## References

- Related research: `context/changes/web-ui/research.md`
- Lessons (load-bearing): `context/foundation/lessons.md` §1
  (SSE `json.Marshal` only)
- Predecessor change: `context/archive/2026-06-21-daemon-mode/`
  — added `--fg` and `--daemon` flags; this plan supersedes them
- Package layout: `context/archive/2026-06-21-go-package-layout/research.md`
  — confirms `cmd/<binary>/` convention; web UI lives in
  `proxy/web/` since it's used by the binary and exported via
  the proxy domain
- AGENTS.md: `mage` targets, lint/format conventions, Go version
  pinning (1.26.4)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>`
> when a step lands. Do not rename step titles.

### Phase 1: Refactor + scaffold

#### Automated

- [x] 1.1 `mage vet` passes — 1229f03
- [x] 1.2 `mage lint` passes — 1229f03
- [x] 1.3 `mage test` passes — 1229f03
- [x] 1.4 `mage build` produces binary (TUI default) — 1229f03
- [x] 1.5 `mage modVerify` passes — 1229f03
- [x] 1.6 `mage tidyCheck` passes — 1229f03
- [x] 1.7 `mage govulncheck` passes — 1229f03

#### Manual

- [ ] 1.8 TUI default; web server logs `:8083`; layout renders
- [ ] 1.9 `--ui-port 9090` binds to 9090; `/health` returns 200
- [ ] 1.10 TUI regression-free

### Phase 2: Read-only web UI + auth wiring

#### Automated

- [x] 2.1 `mage test` passes (auth + filter tests) — c62fb19
- [x] 2.2 `mage vet`, `mage lint`, `mage govulncheck` pass — c62fb19

#### Manual

- [ ] 2.3 Default mode: stderr logs + web at :8083
- [ ] 2.4 Live SSE event + log streams visible in browser
- [ ] 2.5 `FREEDIUS_UI_TOKEN` gates all routes when set
- [ ] 2.6 `--tui` flag still works
- [ ] 2.7 Docker smoke: container runs, dashboard reachable

### Phase 3: Writeback (POST/PUT/DELETE)

#### Automated

- [x] 3.1 `mage test` passes (~30 new cases) — 87fffd7
- [x] 3.2 `mage vet`, `mage lint`, `mage govulncheck` pass — 87fffd7

#### Manual

- [ ] 3.3 Add/edit/delete provider through UI persists to YAML
- [ ] 3.4 Delete provider with active mapping → 409
- [ ] 3.5 Add/edit/delete mapping through UI persists
- [ ] 3.6 `FREEDIUS_UI_TOKEN` gates writeback when set

### Phase 4: TUI + IPC removal + Docker

#### Automated

- [x] 4.1 `mage test` passes
- [x] 4.2 `mage vet`, `mage lint`, `mage govulncheck` pass
- [x] 4.3 `mage tidyCheck` passes; `go.mod` no `charm.land/*`
- [x] 4.4 `mage build` produces smaller binary (size delta recorded)
- [x] 4.5 `mage dockerBuild` produces working image
- [x] 4.6 `mage dockerRun` starts container; logs on `docker logs`

#### Manual

- [ ] 4.7 `docker run` end-to-end works
- [ ] 4.8 `docker compose up` works
- [ ] 4.9 `--tui`, `--fg`, `--daemon`, `attach` exit with error
- [ ] 4.10 End-to-end: add provider in UI → request via :8082 →
       see event live on dashboard
