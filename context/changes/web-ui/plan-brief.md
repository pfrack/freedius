# Replace TUI with Web UI — Plan Brief

> Full plan: `context/changes/web-ui/plan.md`
> Research: `context/changes/web-ui/research.md`

## What & Why

Replace the in-process Bubble Tea dashboard with an embedded Go
stdlib web server (`html/template` + vendored htmx) bound to a
separate port. freedius then runs cleanly in Docker / headless
environments while still showing live request and log data
through a browser. The driving motivation is Docker support: the
TUI is TTY-coupled and cannot run inside a container.

## Starting Point

The hardest plumbing already exists and is well-tested:
`proxy.LogSink` and `proxy.EventBus` (channel + 10k ring),
SSE handlers in `cmd/freedius/ipc_unix.go:106-223` (already
follow `lessons.md §1` JSON discipline), graceful shutdown
wiring, the TUI's `submitForm` rollback pattern. The team's
prior change `2026-06-21-daemon-mode` documented `--fg` as
"for Docker/scripts" but never shipped a non-TUI UI surface —
this plan closes that gap. The web UI is mostly exposing what
exists over HTTP.

## Desired End State

`freedius` (no flags) produces: plain text logs on stderr
(visible in `docker logs`), the HTTP proxy on `:8082` for
Claude Code traffic, and the web dashboard at `:8083` showing
live SSE event/log streams plus full provider/mapping CRUD
via htmx forms. Single static binary; vendored htmx via
`embed.FS`; no new Go modules. `docker build -t freedius .`
followed by `docker run -p 8082:8082 -p 8083:8083 -e
OPENCODE_API_KEY freedius` works end-to-end.

## Key Decisions Made

| Decision                       | Choice                                            | Why (1 sentence)                                  | Source    |
| ------------------------------ | ------------------------------------------------- | ------------------------------------------------- | --------- |
| Templating                     | stdlib `html/template` + vendored htmx            | Zero new Go modules; matches project's static-binary stance | Research  |
| Port layout                    | Separate UI port `:8083`                         | Clean proxy namespace; flexible Docker exposure   | Research  |
| Terminal mode                  | Plain text logs to stderr                         | Docker-friendly; no TTY dependency                | Research  |
| TUI fate                       | Drop entirely in this change                      | Smaller binary; cleaner deps; matches Docker motivation | Plan (Round 1 Q1) |
| Writeback timing               | Read-only Phase 2, writeback Phase 3              | Ships Docker observability faster; auth-first     | Plan (Round 1 Q2) |
| Auth model                     | Optional shared-secret token via `FREEDIUS_UI_TOKEN` | Opt-in security for LAN/beyond; zero friction localhost | Plan (Round 1 Q3) |
| URL structure                  | Per-tab URLs (`/logs`, `/providers`, `/mappings`) with shared layout | Bookmarkable, deep-linkable, htmx-friendly | Plan (Round 2 Q4) |
| IPC Unix socket survival       | Drop in Phase 4 (no consumers after TUI removal)  | Cleaner code; HTTP `/v1/*` replaces it             | Plan (Round 2 Q5) |
| Theme system on web            | Skip — CSS `@media (prefers-color-scheme)`        | Zero JS; browser handles dark/light automatically  | Plan (Round 2 Q6) |
| SSE handler refactor           | Extract into `internal/eventstream/handlers.go`   | Transport-agnostic; same handlers mount on web mux | Plan     |
| Dockerfile base                | distroless `static-debian12:nonroot`              | Minimal attack surface; no glibc; 12 MB image      | Plan     |

## Scope

**In scope:**
- New `proxy/web/` package (server, handlers, templates,
  static assets, tests)
- New `internal/eventstream/` package (SSE/JSON handlers
  shared with the web mux)
- Refactor `cmd/freedius/ipc_unix.go` to delegate to
  `eventstream.Handlers`
- `--ui-port`, `--ui-host`, `FREEDIUS_UI_PORT`, `FREEDIUS_UI_HOST`,
  `FREEDIUS_UI_TOKEN` env/flags
- Read-only `/logs`, `/providers`, `/mappings` pages with SSE
- Full writeback (POST/PUT/DELETE) for providers and mappings
- `Dockerfile` (multi-stage, distroless)
- `docker-compose.yml` example
- `mage dockerBuild` / `dockerRun` / `dockerPush` targets
- README + config.example.yaml updates
- Removal of `proxy/tui/`, `cmd/freedius/ipc_*.go`,
  `cmd/freedius/daemon_*.go`, `cmd/freedius/attach.go`,
  `cmd/freedius/signal_*.go`, `cmd/freedius/paths_*.go`
- Removal of `--fg`, `--daemon`, `--tui`, `attach`/`stop`/`status`

**Out of scope:**
- Per-user authentication (single-user local tool)
- Theme picker (CSS handles dark/light)
- Shell-RC install button (irrelevant in Docker)
- Per-provider deep health monitoring
- HTTP/2 push, gzip, transport-level tuning
- Persistent log storage / archive to disk
- WebSocket-based live updates (SSE is sufficient)

## Architecture / Approach

Two HTTP servers bound on different ports share a single
`*config.Config` and a single `*eventstream.Handlers`:

```
Browser ─► :8083 ───► WebServer (proxy/web/)
                          ├── GET /              index
                          ├── GET /logs          SSE tail + snapshot
                          ├── GET /providers     table + writeback forms
                          ├── GET /mappings      table + writeback forms
                          ├── GET /static/*      embed.FS
                          └── eventstream.Handlers.Register:
                                ├── GET /v1/events   SSE
                                ├── GET /v1/logs     SSE
                                ├── GET /v1/stats    JSON
                                └── GET /v1/config   JSON
                          [optional: requireAuth middleware if
                           FREEDIUS_UI_TOKEN is set]

Claude Code ─► :8082 ─► proxy.Server (existing)
                          └── existing dispatch + adapters
```

Logs flow: proxy → `ringHandler` → `os.Stderr` (terminal /
docker logs) AND `LogSink.ch` → SSE clients.

Config writeback: `htmx form` → handler → `decodeForm` →
`validateFields` → `cfg.Lock` → mutate → `cfg.Marshal` →
`cfg.SaveData` → `cfg.Unlock`. On any failure, rollback the
in-memory mutation before unlocking (mirrors TUI `submitForm`
discipline).

## Phases at a Glance

| Phase                                  | What it delivers                                  | Key risk                                     |
| -------------------------------------- | ------------------------------------------------- | -------------------------------------------- |
| 1. Refactor + scaffold                 | SSE handlers extracted; web package skeleton; htmx vendored; flags wired; no behavior change | embed.FS path surprises; htmx bundle drift |
| 2. Read-only web UI + auth wiring      | Web at `:8083` is the default; live SSE tails; auth opt-in | SSE under load; auth token leakage in referer logs |
| 3. Writeback                           | Full CRUD for providers and mappings through UI  | Save-failure rollback correctness            |
| 4. TUI + IPC removal + Docker          | TUI gone; binary smaller; Docker works end-to-end | Forgotten references; broken build           |

**Prerequisites:** Phase 0 research complete (`research.md`); no
other changes pending.
**Estimated effort:** ~4 focused sessions across 4 phases; each
phase is independently shippable.

## Open Risks & Assumptions

- The vendored `htmx.min.js` bundle (core + sse extension) is
  ~25 KB; if a future htmx API change occurs, the vendored file
  must be re-pinned and re-verified. Mitigated by the
  `htmx.min.js.sha256` sidecar.
- The auth middleware uses constant-time compare on the bearer
  token, but does not rate-limit; a brute-force attacker on the
  LAN could probe the token. Acceptable for a local proxy;
  document in README.
- Removing `--tui`, `--fg`, `--daemon`, `attach`, `stop`,
  `status` is a breaking change. The commit message + README
  must call this out so users on the old flags know what to do.
- The Docker image uses distroless `:nonroot`; some operators
  may need to override `USER` to mount root-owned volumes
  (e.g. for config persistence). Document the env vars
  `FREEDIUS_HOST` / `FREEDIUS_UI_HOST` for binding to
  `0.0.0.0` inside the container.

## Success Criteria (Summary)

- `mage ci` green at every phase boundary.
- `mage dockerBuild && mage dockerRun` (or equivalent) starts a
  container whose `docker logs` show live request lines and
  whose `:8083` dashboard streams the same data via SSE.
- A user can add, edit, and delete a provider through the
  browser; the YAML on disk reflects the changes; rollback
  works on simulated save failure.
- Binary size drops measurably (5-10 MB) after Phase 4 from
  `charm.land/*` removal.
