---
date: 2026-06-20T08:45:52Z
researcher: opencode
git_commit: 1738265bc805fd061636939f24e9c52861fd9578
branch: tui-dashboard
repository: pfrack/freedius
topic: "Unified mode: server-log tab + single binary entry point"
tags: [research, codebase, tui, subcommand, access-log, event-bus, dispatch]
status: complete
last_updated: 2026-06-20
last_updated_by: opencode
---

# Research: Unified mode — server-log tab + single binary entry point

**Date**: 2026-06-20T08:45:52Z
**Researcher**: opencode
**Git Commit**: 1738265bc805fd061636939f24e9c52861fd9578
**Branch**: tui-dashboard
**Repository**: pfrack/freedius

## Research Question

The user wants two architectural changes:

1. **TUI Tab 1 as server logs**: The first tab should display raw server access log lines (like what `AccessLogMiddleware` outputs to stderr in serve mode) instead of the current columnar table format.
2. **Single mode**: Remove the multi-subcommand dispatch (`serve`, `tui`, `init`). `freedius` should always start the TUI+proxy with no subcommands.

## Summary

Both changes are architecturally feasible. Converting Tab 1 to show access-log-style lines requires adding `Method` and `Path` fields to `RequestEvent` (currently missing from the struct) and changing `renderRequestsTab` to produce log-line output instead of a columnar table. Collapsing subcommands requires migrating `runTUI`'s logic into the default entry path and handling the init/bootstrap flow differently — likely as an automatic first-run experience or a `--init` flag. The historical evolution shows the codebase started single-mode, then subcommands were added incrementally; consolidation reverses that trend but simplifies the user experience.

## Detailed Findings

### 1. Current Tab 1 vs Server Access Logs — Field Gap Analysis

**Current Requests Tab** (`proxy/tui/views.go:33-88`) displays a table with columns:

```
HH:MM:SS  STATUS  MODEL(max20)  PROVIDER(max14)  LATENCY  [ERROR_MESSAGE]
```

Example: `15:04:05  200  opus  nim  1.234s`

**Server Access Logs** (`proxy/proxy.go:443-466`, `AccessLogMiddleware`) produce structured log lines:

```
time=... level=INFO msg="request complete" request_id=<hex> method=POST path=/v1/messages status=200 duration_ms=42 matched_provider=nim matched_model=test-model
```

**Missing fields in `RequestEvent`** (`proxy/eventbus.go:13-24`):

| Field | In AccessLog? | In RequestEvent? | Action needed |
|-------|:---:|:---:|---|
| `Method` (POST) | Yes (`method`) | **No** | Add to struct |
| `Path` (/v1/messages) | Yes (`path`) | **No** | Add to struct |
| `RequestID` | Yes (`request_id`) | Yes (`RequestID`) | None |
| `Status` | Yes (`status`) | Yes (`Status`) | None |
| `Duration` | Yes (`duration_ms`, int64) | Yes (`Latency`, time.Duration) | None |
| `MatchedProvider` | Yes | Yes | None |
| `MatchedModel` | Yes | Yes | None |
| `Model` (from body) | **No** | Yes | Optional (could suppress in log format) |
| `Timestamp` | Implicit (slog `time`) | Yes | None |
| `ErrorMessage` | **No** | Yes (≥400 only) | Optional |
| `ErrorType` | **No** | Yes (≥400 only) | Optional |

**To make Tab 1 show access-log-style lines:**

1. Add `Method string` and `Path string` fields to `RequestEvent` (`proxy/eventbus.go:13-24`).
2. Populate them in `EventBusMiddleware` (`proxy/proxy.go:472-504`) from `r.Method` and `r.URL.Path` (both available since the middleware has access to `r`).
3. Change `renderRequestsTab` (`proxy/tui/views.go:33-88`) to render log-line format instead of a table. Options:
   - **Option A (text format)**: `time=15:04:05 level=INFO msg="request complete" request_id=abc method=POST path=/v1/messages status=200 duration_ms=42 matched_provider=nim matched_model=test-model`
   - **Option B (JSON format)**: `{"time":"15:04:05","level":"INFO","msg":"request complete","request_id":"abc","method":"POST","path":"/v1/messages","status":200,"duration_ms":42}`
   - The format could follow `--log-format` (text/json) configured at startup.
4. Consider a new field in `Dashboard` to store the log format preference.

**Format comparison:**

| Aspect | Current Requests tab | Proposed log tab |
|--------|---------------------|------------------|
| Layout | Columnar table with fixed-width truncation | Raw log lines (scroll naturally) |
| Scrolling | Shows last `height-4` events | Same scrolling behavior (fits more per line) |
| Status coloring | Color-coded (green/yellow/red) | Can embed ANSI in log line or keep plain text |
| Header | "Request Log" title | Could omit header or show minimal label |
| Error display | Separate error message column appended | Error could be a log field or embedded inline |
| Information density | Lower (formatted columns waste space) | Higher (single-line per request) |

### 2. Subcommand Dispatch Architecture — Collapse Plan

**Current dispatch** (`main.go:61-89`):

```
freedius           → sub="serve" (default) → runServe()
freedius serve     → sub="serve"           → runServe()
freedius tui       → sub="tui"             → runTUI()
freedius init      → sub="init"            → runInit()
freedius version   → sub="version"         → print version
freedius help      → sub="help"            → printTopLevelHelp()
freedius <unknown> → error exit 2
```

**`runServe` vs `runTUI` — key differences** (`main.go:91-253` vs `tui.go:23-161`):

| Aspect | `runServe` | `runTUI` |
|--------|-----------|----------|
| Flags | 7 (`--config`, `--port`, `--host`, `--verbose-errors`, `--log-format`, `--stream-timeout`, `--no-export-hint`) | 5 (same except no `--verbose-errors`, no `--no-export-hint`) |
| Middleware chain | `Recover` → `AccessLog` → `RequestID` | `Recover` → **`EventBus`** → `AccessLog` → `RequestID` |
| Server start | `server.ListenAndServe()` blocks main | `server.ListenAndServe()` in goroutine |
| Main loop | `signal.NotifyContext` → wait for SIGINT/SIGTERM | `tea.NewProgram(model).Run()` |
| Shutdown | Signal-triggered graceful shutdown | TUI exit → graceful shutdown |
| verboseErrors | Configurable (flag/env) | Always `false` |
| Env-export hint | Printed unless `--no-export-hint` | Not printed |
| Env var check | `checkRequiredEnvVars` blocks startup | Discarded (`_ = checkRequiredEnvVars(cfg)`) |

**`runInit` behavior** (`init.go:20-122`): Generates config file (`templates/starter.yaml`) + optionally `~/.claude/settings.json` + optionally shell rc env block. Uses 7 flags (`--output`, `--force`, `--dry-run`, `--no-env`, `--shell-install`, `--host`, `--port`).

**Collapse strategy:**

The simplest approach: merge `runTUI` into the default entry point and inline the init functionality as flags or automatic behavior:

1. **`dispatch()` → remove**, `main()` calls `run()` directly.
2. **`run()` merges `runTUI` + `runServe` flags**: Accept all flags from both. When `--tui` is explicitly requested or when no `--headless` flag is set, start the TUI. Otherwise run headless (equivalent to old `serve`).
3. **Init functionality**: Either:
   - Add `--init` flag that runs the init logic before starting the proxy, or
   - Auto-detect first run (no config file) and offer to generate one, or
   - Keep `--init` as a flag that generates config and exits (like `--dry-run` exit semantics)
4. **`version`**: Print via `--version` flag (Go convention).
5. **`help`**: Print via `--help` flag (Go convention).

**Alternative (simpler, per user's request):** Just one mode — `freedius` always starts TUI+proxy. If no config exists, auto-generate one. Remove all subcommand code. No headless mode at all. This is the most radical simplification.

### 3. Historical Context — Evolution Toward Subcommands

The codebase started single-mode and subcommands were added incrementally:

| Phase | Document | Architecture |
|-------|----------|-------------|
| F-01 (proxy-skeleton) | `context/archive/proxy-skeleton/plan.md:58` | Single-mode binary: just a server, no subcommands |
| S-04 (error-hardening) | `context/archive/error-hardening/plan.md:17,61` | Subcommand dispatch born: `serve`, `init`, `version`, `help` |
| V-01 (tui-dashboard) | `context/changes/tui-dashboard/plan.md:24,36` | `tui` added as 4th subcommand |
| Current state | `main.go:61-89` | 5 subcommands: `serve`(default), `init`, `tui`, `version`, `help` |

The roadmap (`context/foundation/roadmap.md:185`) explicitly considered the subcommand vs flag question for the TUI and chose subcommand because "separate command is the Bubble Tea convention."

Key design decisions in the plan that would be reversed:
- `tui-dashboard/plan.md:55`: "`freedius serve` (headless mode) is unchanged" — consolidation removes this guarantee.
- `tui-dashboard/plan.md:36`: "The TUI subcommand runs its own proxy instance" — consolidation makes TUI the default/only mode.
- `tui-config-setup/plan.md:69`: "No config template generation — `freedius init` is the CLI for that" — init functionality must be rehomed.

### 4. Files Affected by Consolidation

| File | Current role | What changes |
|------|-------------|-------------|
| `main.go` | `dispatch()`, `runServe()`, `printTopLevelHelp()` | Remove dispatch; merge `runTUI()` logic into default path; remove `runServe` or keep as headless flag |
| `tui.go` | `runTUI()` | Logic moved to `main.go` or kept as `run()` entry point |
| `init.go` | `runInit()` | Refactor into `runInit()` called from flag or auto-detection |
| `proxy/eventbus.go` | `RequestEvent` struct | Add `Method`, `Path` fields |
| `proxy/proxy.go` | `EventBusMiddleware` (472-504) | Populate new `Method`/`Path` fields |
| `proxy/tui/views.go` | `renderRequestsTab()` (33-88) | Rewrite for log-line format |
| `proxy/tui/model.go` | Dashboard struct, tab constants | May need `logFormat` field; `tabRequests` constant stays |
| `Makefile` | `run-tui:` target (line 25-26) | Remove or repurpose |
| `test-manual.sh` | `$BIN init ...` calls | Rewrite to `$BIN --init ...` |
| `README.md` | CLI section listing subcommands | Rewrite for unified mode |
| `main_test.go` | `TestDispatch_*` and `TestRunInit_*` tests | Rewrite or remove dispatch tests; adapt init tests |
| `.github/workflows/ci.yml` | No subcommand invocations | No changes needed |
| `templates/starter.yaml` | Config template | Unchanged (still needed) |
| `internal/envinject/shellrc.go:31,41` | Comment "freedius init" | Update comment |
| `config/config.go:222` | Comment referencing `freedius init` | Update comment |
| Context docs | Multiple references to subcommands | Updated in plan.md (not research.md) |

### 5. Test Impact Assessment

**`main_test.go`** — tests that need changes:

| Test | Lines | Impact |
|------|-------|--------|
| `TestDispatch_HelpSubcommand` | 240-245 | Remove (no dispatch) |
| `TestDispatch_VersionSubcommand` | 247-252 | Remove |
| `TestDispatch_UnknownSubcommand` | 254-271 | Remove |
| `TestDispatch_NoSubcommandRoutesToServe` | 273-284 | Remove |
| `TestRunInit_*` (11 tests) | 286-566 | Adapt if init becomes flag |
| `TestRun_StartupBanner` | 213-236 | Adapt (no `go run . tui`) |
| `TestRunServe_EvalSnippetAppears` | 527-547 | Adapt |
| `TestRunServe_EvalSnippetSuppressed` | 549-566 | Adapt |

**`proxy/tui/model_test.go`** — needs new tests for:
- Log-format rendering path
- New `Method`/`Path` fields in event messages

**`proxy/eventbus_test.go`** — needs new assertions for:
- `Method` and `Path` field population

## Code References

### Current access log production
- `proxy/proxy.go:443-466` — `AccessLogMiddleware` — produces structured log lines with `request_id`, `method`, `path`, `status`, `duration_ms`, `matched_provider`, `matched_model`
- `proxy/proxy.go:455-464` — The exact `logger.Info` call site with all key-value pairs

### Current event bus and request data
- `proxy/eventbus.go:13-24` — `RequestEvent` struct — **missing `Method` and `Path` fields**
- `proxy/eventbus.go:48-67` — `EventBus.Emit()` — non-blocking channel send with overflow handling
- `proxy/proxy.go:472-504` — `EventBusMiddleware` — populates RequestEvent from response headers; has access to `r.Method` and `r.URL.Path` but doesn't capture them
- `proxy/proxy.go:509-527` — `extractModelFromBody()` — reads body to get `"model"` field, then rewinds body for downstream handlers

### Current TUI tab architecture
- `proxy/tui/styles.go:55-59` — Tab constants: `tabRequests=0`, `tabProviders=1`, `tabConfig=2`
- `proxy/tui/views.go:15-31` — `renderTabs()` — tab bar with labels `[1] Requests`, `[2] Providers`, `[3] Config`
- `proxy/tui/views.go:33-88` — `renderRequestsTab()` — current columnar table format with timestamp, status, model, provider, latency, error
- `proxy/tui/model.go:68-90` — `Dashboard` struct — fields including `eventLog *ringBuffer`, `stats statsData`
- `proxy/tui/model.go:183-248` — `handleTabModeKeyPress()` — tab switching logic (keys `1`/`2`/`3`, `tab`/`shift+tab`)
- `proxy/tui/model.go:338-379` — `Dashboard.View()` — tab dispatch logic
- `proxy/tui/model.go:381-392` — `waitForEvent()` — channel-based event consumption

### Subcommand dispatch
- `main.go:57-59` — `main()` → `os.Exit(dispatch(os.Args))`
- `main.go:61-89` — `dispatch()` — full subcommand routing logic with switch/case
- `main.go:255-268` — `printTopLevelHelp()` — lists all subcommands
- `tui.go:23-161` — `runTUI()` — TUI subcommand entry point (to be merged)
- `init.go:20-122` — `runInit()` — init subcommand (to be refactored)
- `main.go:91-253` — `runServe()` — headless serve subcommand

### Key differences between serve and TUI modes
- `main.go:216-218` — serve middleware chain: `Recover` → `AccessLog` → `RequestID` (no EventBus)
- `tui.go:124-128` — TUI middleware chain: `Recover` → **`EventBus`** → `AccessLog` → `RequestID`
- `tui.go:118` — TUI discards `checkRequiredEnvVars` errors (permissive at startup)
- `main.go:195-197` — serve mode blocks on `checkRequiredEnvVars` errors

### Middleware architecture
- `proxy/proxy.go:349-356` — `RequestIDMiddleware` — generates 32-hex ID, sets context+header (outermost in both chains)
- `proxy/proxy.go:395-423` — `RecoverMiddleware` — panic recovery (innermost in serve, 2nd-innermost in TUI)
- `proxy/proxy.go:329-335` — `generateRequestID()` — crypto/rand 16-byte → hex

### Configuration system
- `config/config.go:13-27` — `Config` struct with `Providers` and `Mappings`
- `config/config.go:199-229` — `Config.Marshal()` and `Config.Save()` — YAML serialization with backup
- `config/config.go:206-229` — `Save()` creates `.bak` backup, validates before write
- `templates/starter.yaml` — embedded config template (used by init)

### Historical decisions
- `context/foundation/roadmap.md:185` — Open question: "Subcommand vs flag" for TUI — settled on subcommand
- `context/changes/tui-dashboard/plan.md:55` — "`freedius serve` (headless mode) is unchanged" — explicit design guarantee
- `context/changes/tui-dashboard/plan.md:61` — "No config editing in the TUI — Config tab read-only" (since reversed in tui-config-setup)
- `context/changes/tui-config-setup/plan.md:69` — "No config template generation — `freedius init` is the CLI for that"
- `context/archive/proxy-skeleton/plan.md:58` — Original architecture: single-mode binary, no subcommands
- `context/archive/error-hardening/plan.md:17,61` — Birth of subcommand dispatch: `serve`/`init`/`version`/`help`

## Architecture Insights

### Pattern: Decoupled event bus enables log-format flexibility

The event bus (`proxy/eventbus.go`) decouples request metadata capture from rendering. `EventBusMiddleware` already wraps the response writer and has access to `r.Method` and `r.URL.Path`. Adding these fields to `RequestEvent` is a backward-compatible extension — existing consumers (the current TUI table renderer) simply ignore new fields with zero values. This means the log-format rendering in `renderRequestsTab` can be tested and developed independently of the field-population changes.

### Pattern: Middleware chain differences drive mode behavior

The only architectural difference between serve and TUI modes is the presence of `EventBusMiddleware` in the chain. Serve mode has no `EventBus` at all (nil bus → middleware is a no-op). Consolidation means the EventBus always exists, but this has negligible overhead (a 1000-slot buffered channel and non-blocking sends). The `verboseErrors` flag (currently hardcoded `false` in TUI mode) would need to become user-configurable.

### Pattern: Init functionality is orthogonal to proxy runtime

`runInit` generates files and exits — it doesn't start a server. In a unified mode, init would either:
1. Run as a separate flag (`--init`) that generates config and exits (preserving `runInit`'s independence), or
2. Run automatically on first start (no config found), then proceed to TUI
3. Be triggered by `--init` flag and then optionally continue to start the proxy

Option 2 is most aligned with "only one mode" — `freedius` detects no config, generates one, shows the TUI.

### Pattern: Access log format has two representations

`AccessLogMiddleware` writes structured key-value pairs via `slog.Info()`. The log format (text vs. JSON) is determined at logger creation time via `--log-format`/`FREEDIUS_LOG`. For the TUI log tab, there are two approaches:
1. **Reuse slog**: Write log lines to a `bytes.Buffer` with a matching slog handler, then read and display the buffer in the TUI. This guarantees format parity.
2. **Manual formatting**: Build log lines manually in `renderRequestsTab` from RequestEvent fields. Simpler but risks format drift.

## Historical Context (from prior changes)

- `context/archive/proxy-skeleton/plan.md:58` — Initial architecture was single-mode (just a server). The consolidation direction returns to this root but with the TUI added.
- `context/archive/error-hardening/plan.md:17` — Subcommand dispatch was added here (S-04). Before this, `main.go` had "zero subcommand dispatch" — the binary was a single `run()` function.
- `context/changes/tui-dashboard/plan.md:55` — Explicitly stated "`freedius serve` (headless mode) is unchanged" as a design guarantee. Consolidation reverses this commitment.
- `context/changes/tui-dashboard/plan.md:63` — "No separate `freedius dashboard` command — only `freedius tui`." Consolidation takes this a step further: no separate `freedius serve` command either.
- `context/foundation/roadmap.md:185` — The canonical record of the subcommand-vs-flag decision for TUI. Shows it was an intentional design choice, not accidental.
- `context/changes/tui-config-setup/plan.md:69` — Init delegated to CLI subcommand. Consolidation would move init into the unified flow.

## Related Research

- `context/changes/tui-dashboard/research.md` — Original TUI UI approach decision (TUI vs Web vs GUI)
- `context/changes/tui-config-setup/research.md` — Extending TUI with config editing and error display
- `context/changes/tui-dashboard/plan.md` — Phase-by-phase implementation for the TUI, including middleware chain decisions

## Open Questions

1. **Headless mode**: Should `freedius` support a `--headless` flag to run without TUI (old serve behavior)? The user said "only one mode" — does that mean NO headless option at all, or just no separate subcommand?
2. **Log format in TUI**: Should the log tab follow `--log-format` (text/json), or always use a fixed human-readable format? JSON logs in a terminal are less readable but truer to the server mode output.
3. **Init auto-detection**: Should `freedius` auto-generate a config file on first run when none exists (current behavior already does this), and if so, should it also offer `--shell-install` / `--no-env` functionality?
4. **Verbose errors**: Currently `runTUI` hardcodes `verboseErrors=false`. Should this remain hardcoded or become a flag in the unified mode?
5. **Env-export hint**: `runServe` prints the eval snippet by default; `runTUI` never prints it. Which behavior should the unified mode adopt?
6. **Version**: `freedius version` becomes `freedius --version`. Standard Go convention but breaks existing muscle memory.
7. **Init --shell-install**: This writes to the shell rc file and requires `--force` for re-install — should this be a flag in the unified mode or removed entirely in favor of manual setup?
8. **Log format toggle**: Should there be a keyboard shortcut in the TUI to toggle between text/json log formats on the log tab?
