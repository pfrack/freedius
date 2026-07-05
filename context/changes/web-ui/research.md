---
date: 2026-07-02T00:00:00+00:00
researcher: freedius-research-agent
git_commit: 6d82093
branch: streaming-edge-cases
repository: freedius
topic: "Replace TUI with web UI for Docker-friendly deployments"
tags: [research, tui, web-ui, docker, htmx, sse, refactor]
status: complete
last_updated: 2026-07-02
last_updated_by: freedius-research-agent
---

# Research: Replace TUI with web UI (Docker-friendly)

**Date**: 2026-07-02
**Researcher**: freedius-research-agent
**Git Commit**: 6d82093
**Branch**: streaming-edge-cases
**Repository**: freedius

## Research Question

> "I want to move from TUI to web. I want to show logs on the terminal and
> in the same time spin webserver in sth fast in golang some templating
> like jinja whatever. That will enable me to spin server in docker."

The user wants the Bubble Tea dashboard replaced with a small embedded
web UI, while keeping plain-text logs streaming to stderr. The driving
motivation is Docker support: the TUI is TTY-coupled and cannot run
inside a container, so today the only "headless" mode is `--fg` with no
visualization at all. A web UI restores observability without needing
a TTY.

User-confirmed scope (via interview, 2026-07-02):

- Templating: **Go stdlib `html/template` + vendored htmx** (no new
  runtime dependencies; single static binary preserved).
- Port layout: **separate UI port** (default `FREEDIUS_UI_PORT=8083`)
  alongside the existing proxy port (default `8082`). See "Port-layout
  recommendation" below for the rationale the user asked for.
- Terminal mode: **plain text logs to stderr** (no Bubble Tea by
  default).

## Summary

**You are 90% of the way there.** The hardest plumbing already exists
in the repo, just behind a Unix-socket IPC server built for an
out-of-process TUI to consume. The migration is mostly about exposing
what already exists over HTTP and replacing the Bubble Tea renderer
with `html/template` + htmx. The non-TUI parts of `main.go` (config
loading, server wiring, middleware chain, dispatcher) are untouched.

Reusable today:

- `proxy.LogSink` — bounded channel + 10k-entry ring, replay via
  `SnapshotSince(seq)`.
- `proxy.EventBus` — same shape for request events.
- SSE handlers (`/v1/events`, `/v1/logs`, `/v1/stats`, `/v1/config`)
  defined inline in `cmd/freedius/ipc_unix.go` — already follow the
  `json.Marshal` discipline recorded in `lessons.md §1`.
- Graceful shutdown wiring via `signal.NotifyContext`.
- TUI config-edit/rollback semantics in `proxy/tui/model.go` — the
  validation and rollback logic is portable to HTTP handlers almost
  verbatim.

The TUI itself, by contrast, is hostile to Docker: it requires a TTY
(`tea.NewProgram` → AltScreen + mouse mode), reads `os.Stdin` for
`lipgloss.HasDarkBackground`, and assumes interactive keypresses.

## Detailed Findings

### Existing infrastructure to reuse

**`proxy/logtee.go`** — `LogSink` ([`proxy/logtee.go:1-260`](../logtee.go))
- Channel-buffer (default `1000`) plus a 10k-entry ring buffer for
  replay ([`proxy/logtee.go:43-50`](../logtee.go)).
- `SnapshotSince(seq int64) ([]LogEntry, int64, bool)` implements the
  exact "give me everything ≥ this seq, tell me if anything was
  evicted" semantic SSE needs
  ([`proxy/logtee.go:99-160`](../logtee.go)).
- `ringHandler.Handle` writes to **both** the inner handler (stderr
  when wired that way) and the sink
  ([`proxy/logtee.go:194-244`](../logtee.go)). To make logs
  visible on the terminal, the only change is making the inner
  handler write to `os.Stderr` instead of `io.Discard` in the new
  default mode.

**`proxy/eventbus.go`** — `EventBus` ([`proxy/eventbus.go:1-180`](../eventbus.go))
- Same channel + ring + `Since(seq)` pattern as LogSink.
- Already consumed by the TUI dashboard and by the IPC server.

**`cmd/freedius/ipc_unix.go`** — SSE handlers
- `handleEvents` ([`cmd/freedius/ipc_unix.go:106-148`](../cmd/freedius/ipc_unix.go))
  emits `event: event` and `event: replay` SSE frames with replay-
  then-live semantics.
- `handleLogs` ([`cmd/freedius/ipc_unix.go:150-193`](../cmd/freedius/ipc_unix.go))
  is structurally identical, swapping in `LogSink` + `SnapshotSince`.
- `handleStats` / `handleConfig` ([`cmd/freedius/ipc_unix.go:195-223`](../cmd/freedius/ipc_unix.go))
  return JSON snapshots.
- All four use `json.Marshal` (not `json.NewEncoder`) per
  `lessons.md §1`. This is load-bearing for SSE framing.

**`cmd/freedius/daemon_unix.go`** — fork-to-background pattern
- The `--daemon` flag re-execs the binary with `--fg`
  ([`cmd/freedius/daemon_unix.go:48-58`](../cmd/freedius/daemon_unix.go)).
- `--fg` itself is already documented as "no TUI, for Docker/scripts"
  ([`cmd/freedius/main.go:111`](../cmd/freedius/main.go)). It currently
  just exposes the proxy on a port with logs going to stderr (via
  `logWriter = os.Stderr` at `cmd/freedius/main.go:159-161`) — but
  with no UI surface.

**`cmd/freedius/main.go:startHeadlessWithIPC`**
([`cmd/freedius/main.go:266-291`](../cmd/freedius/main.go))
- Wires the IPC Unix-socket server as a sidecar to the proxy and
  blocks until shutdown. This is the exact pattern the web server
  needs to copy — same idea, different transport.

### TUI surface to replace

The TUI exposes three tabs; each maps cleanly onto web pages. All
file references are under `proxy/tui/`.

| TUI feature             | Source file                                       | Web equivalent                            |
| ----------------------- | ------------------------------------------------- | ----------------------------------------- |
| Live log view           | `views.go:30-67` (`renderLogTab`)                 | SSE `/v1/logs` + htmx `sse-swap`          |
| Live request events     | same view                                         | SSE `/v1/events`                          |
| Log level filter        | `loglevel.go:1-46`                                | Client-side `<select>` filter             |
| Stats (uptime, totals)  | `views.go:178-200` (`renderStatsBar`)             | JSON `/v1/stats`, polled or SSE-pushed    |
| Providers view          | `views.go:101-176` (`renderProvidersTab`)         | HTML table fed from `/v1/providers`       |
| Provider edit/add/del   | `model.go:544-680` (`openEditProviderForm`, `openAddProviderForm`, `handleDeleteConfirmKeyPress`) | HTML forms + POST/PUT/DEL handlers |
| Provider validation     | `model.go:691-737` (`validateForm`)               | Handler-side validation, 400 + JSON errors |
| Mappings view           | `views.go` (Mappings tab)                         | HTML table fed from `/v1/mappings`        |
| Mapping edit/add/del    | `model.go:558-590` + `handleDeleteConfirmKeyPress` | HTML forms + handlers                    |
| Form rollback on save failure | `model.go:617-768` (`submitForm`)             | Same rollback struct in handler           |
| Theme cycling (5 themes) | `styles.go:1-60`, `model.go:386-401`              | CSS variables, single neutral theme       |
| Verbose-errors toggle   | `model.go:369-383` (`toggleVerboseErrors`)        | Button → POST `/v1/runtime/verbose-errors`|
| Shell-RC install (Ctrl+S) | `model.go:228-247` (`installShellRC`)           | **Drop for Docker** — irrelevant in container |
| Detach/attach via socket | `cmd/freedius/ipc_unix.go` + `attach.go`         | **Vestigial** — web UI replaces this use case |
| Tab bar / mouse / modals | various                                           | htmx-driven, no JS framework needed       |

**What we lose on purpose:** install-shell-RC and the detach/attach
workflow are local-machine features; neither makes sense inside a
container. Theme cycling survives as a single CSS-variable-driven
theme (or is dropped entirely — see Open Question Q1).

### Recommended architecture

**Two servers, two ports.** The proxy keeps `8082` (Claude Code and
friends hit it). A new web UI binds `FREEDIUS_UI_PORT` (default
`8083`) and serves templated HTML + JSON + SSE. Default log
destination flips from `io.Discard` (current TUI mode) to
`os.Stderr` so logs appear on the terminal naturally without
needing a UI.

```
Browser  ──► http://localhost:8083/         (htmx-driven dashboard)
client   ──► http://localhost:8082/v1/...   (OpenAI / Anthropic API)
terminal ──► stderr                        (slog text lines, line-buffered)
```

Routing surface:

```
GET  /                  index.html (dashboard shell with tabs)
GET  /logs              logs.html (server-rendered recent + htmx SSE tail)
GET  /providers         providers.html (table + add form)
GET  /mappings          mappings.html (table + add form)
GET  /static/htmx.js    vendored htmx.min.js (via embed.FS)
GET  /static/app.css    dashboard styles (via embed.FS)

GET  /v1/events         SSE: request events (replay + live)
GET  /v1/logs           SSE: log entries (replay + live)
GET  /v1/stats          JSON snapshot (uptime, counts)
GET  /v1/config         JSON dump of providers + mappings
GET  /v1/providers      JSON list (for htmx re-render)
POST /v1/providers      create (validates, then mutate config)
PUT  /v1/providers/{n}  update (rollback pattern from TUI submitForm)
DEL  /v1/providers/{n}  delete (refuse if any mapping references it)
GET  /v1/mappings       JSON list
POST /v1/mappings       create
PUT  /v1/mappings/{n}   update
DEL  /v1/mappings/{n}   delete
POST /v1/runtime/verbose-errors   toggle (mirrors Ctrl+E)
```

### Templating choice

`html/template` + `embed.FS` (Go 1.16+; project is on 1.26.4).
Templates live next to the package, e.g.
`proxy/web/templates/{layout,log,providers,mappings}.html`.
htmx vendored at `proxy/web/static/htmx.min.js` and embedded with
`//go:embed`. **No new dependencies**; the static binary stays
single-file. This matches the project's "zero external runtime deps"
stance and keeps the existing `mage build` output unchanged.

Rationale for vendored htmx over CDN: the project explicitly aims
for air-gapped Docker deployments and a single static binary; an
HTMX CDN fetch would either fail in airgapped networks or force
operators to add `htmx.org` to an outbound-allowlist.

### Refactor: extract SSE handlers

`cmd/freedius/ipc_unix.go` currently defines `IPCServer` that
**owns** the handlers. Move the four handler funcs
(`handleEvents`, `handleLogs`, `handleStats`, `handleConfig`) into
a new `proxy/web/handlers.go` package taking `*proxy.EventBus`,
`*proxy.LogSink`, `*config.Config`, `*proxy.Registry`, `host`,
`port`. Both `IPCServer` and the new `WebServer` mount them via
`http.ServeMux`. Net: zero behavioral duplication; the Unix socket
keeps working as a backward-compat path for `freedius attach`.

### htmx wiring sketches

Log tail:
```html
<div hx-ext="sse" sse-connect="/v1/logs" sse-swap="log"
     hx-swap="beforeend scroll:#log:bottom">
  {{ range .RecentLogs }}
  <pre class="log-{{ .Level }}">{{ .Line }}</pre>
  {{ end }}
</div>
```

Provider add form:
```html
<form hx-post="/v1/providers" hx-target="#provider-list" hx-swap="outerHTML">
  <input name="name" required>
  <select name="behavior">
    <option>openai</option><option>anthropic</option><option>mix</option>
  </select>
  <input name="default_base_url" type="url">
  <input name="default_api_key_env">
  <input name="anthropic_version">
  <select name="protocol">
    <option value="">(none)</option>
    <option>openai</option><option>anthropic</option>
  </select>
  <button>Add</button>
</form>
```

Delete confirm:
```html
<button hx-delete="/v1/providers/{{ .Name }}"
        hx-confirm="Delete provider {{ .Name }}? Mappings referencing it will fail."
        hx-target="#provider-list" hx-swap="outerHTML">Delete</button>
```

### Port-layout recommendation: separate port

User asked "which is more user friendly?" Recommendation: **separate
port (8083)**.

For:
1. **Proxy namespace stays clean.** Tools/clients that hit
   `/v1/messages` never see `/ui/*` and can't accidentally 404 on the
   UI tree.
2. **Docker ergonomics.** `docker run -p 8082:8082` is enough if you
   only need the proxy. The UI can stay internal
   (`-p 8083:127.0.0.1:8083`) for the operator's browser via Docker
   host. With a single port, exposing only the proxy means carving
   out an exclusion path.
3. **Reverse-proxy friendly.** Operators commonly put the proxy
   behind nginx/Caddy for TLS termination; mounting UI on a separate
   port means no path-prefix gymnastics in the reverse-proxy config.
4. **Independent lifecycles.** Proxy and UI can be tested, restarted,
   and rate-limited independently.

Against (single port with `/ui/*` prefix): one URL, one `EXPOSE`.
That's the only real win, and it costs routing cleanness. For Docker
deployments specifically, separate port wins.

### Docker readiness gaps

1. **No `Dockerfile` in repo** — confirmed by `find` over the tree.
   Only `magefiles/mage.go:75-78` references `dockerBuild` /
   `dockerRun` / `dockerPush` mage targets that don't exist yet.
   `context/foundation/roadmap.md:50` also calls this out explicitly.
2. **TUI hard-couples to a TTY** — `tea.NewProgram`, AltScreen,
   `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)`. Cannot run in a
   container without `-it`. The web UI removes this constraint.
3. **No `EXPOSE` documentation in README** for which ports to
   forward.
4. **No example `docker-compose.yml`** showing how to wire env vars
   (`NVIDIA_NIM_API_KEY`, `OPENCODE_API_KEY`, `FREEDIUS_HOST=0.0.0.0`).

Items 1–3 are solved by adding the web UI + Dockerfile. Item 4 is a
one-time README/docs task.

## Code References

| File | Lines | Notes |
| --- | --- | --- |
| [`proxy/logtee.go`](../logtee.go) | 1–260 | `LogSink`, `NewRingHandler`, `SnapshotSince` — lift as-is |
| [`proxy/eventbus.go`](../eventbus.go) | 1–180 | `EventBus` — lift as-is |
| [`cmd/freedius/ipc_unix.go`](../cmd/freedius/ipc_unix.go) | 106–223 | SSE handlers to extract into `proxy/web/handlers.go` |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 111 | `flagFg` — becomes default behavior in Phase 2 |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 130–133 | `--daemon` vs `--fg` mutex — unchanged |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 159–167 | log writer selection (`io.Discard` → `os.Stderr` default) |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 224–227 | `startHeadlessWithIPC` becomes the new default startup path |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 266–291 | `startHeadlessWithIPC` body — copy pattern for `startHeadlessWithWeb` |
| [`cmd/freedius/main.go`](../cmd/freedius/main.go) | 415–419 | `/health` handler — already exists; reuse for Docker healthcheck |
| [`proxy/tui/model.go`](../proxy/tui/model.go) | 617–768 | `submitForm`, `validateForm` — port logic to HTTP handlers |
| [`proxy/tui/views.go`](../proxy/tui/views.go) | 30–67 | `renderLogTab` — informs Log template structure |
| [`proxy/tui/views.go`](../proxy/tui/views.go) | 101–176 | `renderProvidersTab` — informs Providers template |
| [`proxy/tui/views.go`](../proxy/tui/views.go) | 178–200 | `renderStatsBar` — informs stats JSON shape |
| [`proxy/tui/styles.go`](../proxy/tui/styles.go) | 1–60 | Color palette — informational; web will use CSS |
| [`go.mod`](../go.mod) | 1–18 | No new deps; vendored htmx via `embed.FS` |

## Architecture Insights

- **The IPC server is the prototype web server.** It already
  implements SSE replay-on-attach, JSON snapshots, and the
  `json.Marshal`-only discipline from `lessons.md §1`. Refactoring
  it into a transport-agnostic handler package is the single biggest
  leverage point — the web UI becomes a `net/http` server mounting
  the same handlers.

- **The `LogSink` `ringHandler` is already a perfect fan-out.** It
  writes to the inner handler (stderr in our new default) *and*
  pushes into the sink channel. Making the inner handler write to
  stderr by default is a one-line change in `main.go`.

- **TUI submit/validate/delete is HTTP-shaped already.** The state
  machine in `proxy/tui/model.go` (focused field, validation errors
  per field, rollback on save failure) maps 1:1 onto
  `application/x-www-form-urlencoded` POST + JSON error response +
  server-side rollback. We can lift the logic with minimal
  rewriting.

- **Two servers, one config.** Both the proxy and the web UI need
  the same `*config.Config` pointer. They both already hold it. No
  new shared state is required.

- **Theme system collapses for the web.** Five themes with adaptive
  light/dark is a TUI-necessary ergonomic — web CSS already does
  adaptive theming via `@media (prefers-color-scheme: dark)`. We can
  ship a single theme and skip the entire `themeRegistry` /
  `cycleTheme` machinery.

## Historical Context (from prior changes)

- **`2026-06-21-daemon-mode`** ([`context/archive/2026-06-21-daemon-mode/plan.md`](../archive/2026-06-21-daemon-mode/plan.md))
  added `--fg` and `--daemon` flags. Its plan-brief explicitly
  motivated `--fg` as "no TUI, for Docker/scripts" — but the team
  never shipped a non-TUI UI, so Docker deployments have been
  observability-blind since then. Our research closes that gap.
- **`2026-06-20-tui-themes`** ([`context/archive/2026-06-20-tui-themes/`](../archive/2026-06-20-tui-themes/))
  shows how the TUI's theme system is currently organized. The web
  UI will not need any of this — CSS variables are sufficient.
- **`2026-06-21-go-package-layout`** ([`context/archive/2026-06-21-go-package-layout/research.md`](../archive/2026-06-21-go-package-layout/research.md))
  records the team's commitment to `cmd/<binary>/` for entry points
  and `internal/<pkg>/` for non-exported packages. The proposed
  `proxy/web/` is consistent with that decision: web handlers are
  used by the binary, but if they grow further they should be
  considered for `internal/web/` if they pull in dependencies
  specific to the binary.

## Related Research

- [`context/archive/2026-06-21-daemon-mode/research.md`](../archive/2026-06-21-daemon-mode/research.md) — original
  rationale for headless `--fg` mode; this research assumes that
  flag survives.
- [`context/foundation/lessons.md`](../foundation/lessons.md) — `§1`
  SSE encoding rule is non-negotiable for the new web handlers.

## Open Questions

1. **Keep `--tui` as opt-in, or drop TUI outright?** Recommend
   keep-behind-flag for at least one release so devs keep their
   muscle memory; drop in a later major change.
2. **Write-back (POST/PUT/DEL) in v1, or read-only first?**
   Read-only is faster to ship and easier to secure; writeback is
   what makes the web UI actually *replace* the TUI for editing.
   Recommended split: Phase 1 = read-only (logs/events/stats/providers
   list/mappings list), Phase 2 = add writeback.
3. **Auth for the UI?** Localhost-only proxy implies trusted-user-
   on-localhost, but if you ever expose `:8083` to a LAN, anyone on
   the network can read logs (which may contain API keys in upstream
   error messages). Even a single shared-secret header via
   `FREEDIUS_UI_TOKEN` env is a ~30-line addition and should be
   considered for Phase 3.
4. **Should the IPC Unix socket + `attach` subcommand survive?**
   Backward compat for `freedius --daemon && freedius attach`
   workflows says yes. If the web UI is the new "attach" surface
   for headless use, no.
5. **Per-tab URLs or single-page tabs?** `/logs`, `/providers`,
   `/mappings` as separate paths (more bookmarkable, htmx-friendly,
   deep-linkable) or single-page tabs (simpler, less HTML). Recommend
   separate paths.
6. **Theme system on the web?** Skip entirely (CSS `@media`
   handles dark/light automatically) vs. ship a small theme picker.
   Recommend skip for v1; add only on request.

## Verification Strategy (for the implementation plan)

A future `/10x-plan` should propose chunks that verify against:

- `mage test` — handler-level table-driven tests for all
  `proxy/web/handlers.go` functions, using `httptest.NewServer` per
  the existing test conventions in `AGENTS.md`.
- `mage lint` — no new lint findings.
- Manual: `mage run -- --port 8082 --ui-port 8083`, open browser to
  `http://localhost:8083`, run a `curl` through the proxy, observe
  log + event lines streaming live.
- Manual: `docker build -t freedius . && docker run --rm -p 8082:8082
  -p 8083:8083 -e OPENCODE_API_KEY freedius` should print logs to
  docker logs and serve the dashboard.
- Backward compat: `freedius --tui` still works through Phase 2.

## Decision Log

| Decision | Choice | Rationale | Rejected |
| --- | --- | --- | --- |
| Templating | `html/template` + vendored htmx | Stdlib + zero-runtime-deps; matches project stance | External router (e.g. chi, gin); pure stdlib + write-everything-by-hand (too much JS); template engine like `templ` (compile-time but new dep) |
| Port layout | Separate UI port (8083) | Clean proxy namespace, flexible Docker exposure | Single port with `/ui/*` prefix |
| Terminal mode | Plain text stderr | Lowest friction; works in Docker; TUI optional behind `--tui` | Keep TUI default; ncurses-style logger |
| SSE handler location | `proxy/web/handlers.go` | Reusable for both Unix socket and web | Duplicate handlers in two packages |
| TUI fate | Keep behind `--tui` for now | Backward compat for muscle memory | Drop immediately (risky), keep as default (blocks Docker) |
| Where to bind UI | Same `FREEDIUS_HOST` by default; add `--ui-bind` later if needed | Simple default; most users run everything localhost | Force separate bind address now |
| Writeback ordering | Phase 1 read-only, Phase 2 writeback | Ship observable Docker faster; auth before writes | Writeback in v1 (auth gap), read-only forever (UI is read-mostly anyway) |
