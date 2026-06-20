# Unified Server-Logs Tab + Single Entry Point — Implementation Plan

## Overview

Two architectural changes that collapse the multi-subcommand CLI into a single `freedius` binary that always starts the TUI+proxy, and replace Tab 1's columnar requests table with human-readable access-log lines matching the `AccessLogMiddleware` output format.

## Current State Analysis

The binary dispatches via `main.go:61-89` five subcommands: `serve` (default), `tui`, `init`, `version`, `help`. The `serve` and `tui` modes differ only in middleware chain (EventBus presence) and a few flag/behavior differences (`verboseErrors` hardcoded false in TUI, env-export hint only in serve). The `init` subcommand generates files and exits — orthogonal to proxy runtime.

Tab 1 (`renderRequestsTab` at `proxy/tui/views.go:33-88`) renders a columnar table (`HH:MM:SS  STATUS  model  provider  latency  errmsg`). The `RequestEvent` struct (`proxy/eventbus.go:13-24`) captures 9 fields but is missing `Method` and `Path` — the only gap between what AccessLogMiddleware produces and what the event bus can emit.

Current config flow: `runServe` and `runTUI` both auto-generate a config file on disk when none exists (`main.go:178-191`). The user wants embedded-config startup with lazy write — only persist when the user explicitly saves via the Config tab.

### Key Discoveries

- `EventBusMiddleware` (`proxy/proxy.go:472-504`) has access to `r.Method` and `r.URL.Path` but never captures them
- `Dispatcher.VerboseErrors` is a public field (`proxy/proxy.go:42`) — directly mutable at runtime
- `config.Load()` returns error on missing file; no existing "load from bytes" path
- Tab constants are `tabRequests=0`, `tabProviders=1`, `tabConfig=2` in `proxy/tui/styles.go:55-59`
- The `ringBuffer` in `proxy/tui/model.go:28-60` already handles overflow; log lines are denser than table rows
- 11 `TestRunInit_*` tests, 4 `TestDispatch_*` tests, 2 `TestRunServe_*` tests will need removal or adaptation
- `//go:embed templates/starter.yaml` is in `init.go:17-18` — must be relocated to `main.go` when `init.go` is removed

## Desired End State

A single `freedius` binary that starts the TUI+proxy with no subcommands. Tab 1 shows human-readable access-log lines (key=value format, color-coded status). The binary starts with an embedded default config in memory; a config file is only written to disk when the user explicitly saves from the Config tab. `--version`, `--help`, `--verbose-errors`, and `--no-export-hint` are CLI flags. Keyboard shortcuts in the TUI: `Ctrl+E` toggles verbose errors, `Ctrl+S` in Config tab writes the env-block to shell RC.

### Verification

- `freedius` (no args) starts TUI with proxy listening, Tab 1 shows log lines
- `freedius --version` prints version and exits
- `freedius --help` prints usage and exits
- `freedius --no-export-hint` suppresses the pre-TUI eval snippet
- `freedius --verbose-errors` enables error details; `Ctrl+E` in TUI toggles
- On first run with no config file, TUI shows default providers/mappings; no file is created
- After editing config and saving via Config tab, `freedius.yaml` is written at the resolved path
- `Ctrl+S` in Config tab writes env vars to shell RC
- `make run` starts the TUI (no more `make run-tui`)

## What We're NOT Doing

- No headless mode — TUI-only. `freedius` always requires a terminal
- No JSON log format in TUI — always human-readable key=value
- No init subcommand or `--init` flag — config generation is automatic/on-save
- No adapter-level verbose-errors toggle at runtime — `Dispatcher.VerboseErrors` toggle covers dispatch/validation errors only
- No `~/.claude/settings.json` auto-write — only shell RC via `Ctrl+S`
- No migration of existing config files — config format is unchanged

## Implementation Approach

Extend the event data model first (Phase 1), then rewrite the rendering that consumes it (Phase 2). Collapse the CLI dispatch next (Phase 3), followed by the lazy-config-startup flow (Phase 4). Add TUI keyboard shortcuts last (Phase 5), then clean up tests and docs (Phase 6).

Each phase produces a compilable binary with passing tests. Phase ordering respects dependencies: data model before rendering, entry point before config lifecycle, base TUI before shortcuts.

## Critical Implementation Details

- **Middleware ordering must stay: RequestID → AccessLog → EventBus → Recover** — EventBus must be after AccessLog so AccessLog can still log normally, and after RequestID so the ID is available. Recover is innermost (wraps the dispatcher). This is the current TUI chain from `tui.go:125-129`.
- **`checkRequiredEnvVars` error discarded** — like current TUI mode (`tui.go:118`), the unified mode logs a warning but doesn't block startup. The Config tab makes missing env vars visible to the user.
- **Tab constant rename**: `tabRequests` → `tabLog` in `proxy/tui/styles.go`. This is referenced in ~20 locations across `model.go`, `views.go`, and `model_test.go`. Rename globally.
- **`NewDashboard` signature change**: adds `host`, `port`, `verboseErrors` parameters. All call sites (production in main, tests in model_test.go) need updating.

## Phase 1: Add Method and Path fields to RequestEvent

### Overview

Extend the event data model so the TUI can render log lines with the same fields as `AccessLogMiddleware`. Populate the new fields in `EventBusMiddleware` where `r.Method` and `r.URL.Path` are already accessible.

### Changes Required

#### 1. RequestEvent struct — add fields

**File**: `proxy/eventbus.go`

**Intent**: Add `Method` and `Path` string fields to `RequestEvent` so event consumers can render access-log-style lines.

**Contract**: Two new exported fields: `Method string` and `Path string`, added after existing fields (no other reordering).

#### 2. EventBusMiddleware — populate new fields

**File**: `proxy/proxy.go`

**Intent**: Populate the new `Method` and `Path` fields in the `RequestEvent` built inside `EventBusMiddleware` so TUI events carry the full access-log context.

**Contract**: In `EventBusMiddleware` (lines ~489-497), add `Method: r.Method` and `Path: r.URL.Path` to the `RequestEvent` literal before `bus.Emit(event)`.

#### 3. EventBus tests — assert new fields

**File**: `proxy/eventbus_test.go`

**Intent**: Update existing tests to cover the new fields.

**Contract**: In `TestEventBus_EmitAndSubscribe`, populate `Method` and `Path` in test event structs, then assert `got.Method` and `got.Path` match after subscription.

#### 4. TUI model tests — include new fields in test events

**File**: `proxy/tui/model_test.go`

**Intent**: Update event-related tests to populate Method and Path so they reflect realistic events.

**Contract**: In `TestDashboard_Update_EventMsg` and `TestDashboard_Update_EventMsgErrorCount`, set `Method` and `Path` on the emitted `RequestEvent`.

### Success Criteria

#### Automated Verification

- Build passes: `go build ./...`
- All tests pass: `go test ./...`
- `go vet ./...` clean

#### Manual Verification

- N/A — data model change only; user-visible behavior unchanged until Phase 2

---

## Phase 2: Rewrite Tab 1 as access-log view

### Overview

Replace the columnar table in `renderRequestsTab` with human-readable log lines matching `AccessLogMiddleware` output format. Rename the tab from "Requests" to "Log". Keep status color-coding (green <400, yellow 400-499, red 500+). Keep the same scrolling behavior (last `height-4` events visible).

### Changes Required

#### 1. Tab constant and label rename

**File**: `proxy/tui/styles.go`

**Intent**: Rename `tabRequests` constant to `tabLog` to reflect the changed tab purpose.

**Contract**: `tabLog = 0` replaces `tabRequests = 0`.

**File**: `proxy/tui/views.go`

**Intent**: Update tab label from `[1] Requests` to `[1] Log`.

**Contract**: In `renderTabs`, string literal `"[1] Log"` replaces `"[1] Requests"` at line ~17.

#### 2. Rename constant references globally

**File**: `proxy/tui/model.go`, `proxy/tui/model_test.go`, `proxy/tui/views.go`

**Intent**: Replace all references to `tabRequests` with `tabLog`.

**Contract**: Mechanical rename — grep for `tabRequests` and replace with `tabLog` in all `.go` files under `proxy/tui/`.

#### 3. Rewrite renderRequestsTab → renderLogTab

**File**: `proxy/tui/views.go`

**Intent**: Replace the columnar table rendering with log-line output in the format `time=HH:MM:SS request_id=<hex> method=METHOD path=PATH status=STATUS duration_ms=DURATION matched_provider=PROVIDER matched_model=MODEL`.

**Contract**: Function signature stays `renderLogTab(events []proxy.RequestEvent, _ int, height int) string`. Empty state returns `windowStyle.Render("No requests yet...")`. Visible window: `len(events) - (height - 4)` clamped to ≥0. Each event line format:

```
time=15:04:05 request_id=abc123... method=POST path=/v1/messages status=200 duration_ms=42 matched_provider=nim matched_model=stepfun-ai/step-3.5-flash
```

- `status` field value is colorized with existing color styles (`statusOKStyle`, `statusClientErrStyle`, `statusErrorStyle`)
- `duration_ms` uses `e.Latency.Milliseconds()`
- `matched_provider` and `matched_model` come from `e.MatchedProvider` and `e.MatchedModel`
- For errors (status ≥ 400), append ` error="<ErrorMessage>"` inline if `e.ErrorMessage` is non-empty
- Lines join vertically with newlines, wrapped in `windowStyle`

**Contract — rename in Dispatch**: In `Dashboard.View()` (`model.go` line ~354), rename the case from `tabRequests`/`renderRequestsTab` to `tabLog`/`renderLogTab`.

#### 4. TUI tests — log rendering coverage

**File**: `proxy/tui/model_test.go`

**Intent**: Add tests that verify log-line rendering output.

**Contract**: New test `TestDashboard_RenderLogTab_Format` — creates a Dashboard, populates `d.eventLog` via `push()` with a `RequestEvent` containing known fields, calls `d.View()`, asserts the output string contains `request_id=`, `method=POST`, `path=/v1/messages`, `status=200`, `duration_ms=`. Another test `TestDashboard_RenderLogTab_Empty` asserts the empty-state message.

### Success Criteria

#### Automated Verification

- `go build ./...` succeeds
- `go test ./proxy/tui/... -v -run "Log"` passes
- All existing tests pass: `go test ./...`
- `go vet ./...` clean

#### Manual Verification

- Run `freedius`, send a request, verify Tab 1 shows log lines with `method=`, `path=`, `request_id=`, `status=`, `duration_ms=`
- Verify status colors: 200 green, 404 yellow, 500 red
- Verify tab label shows `[1] Log`
- Verify scrolling works with many requests

---

## Phase 3: Collapse subcommand dispatch into unified entry point

### Overview

Remove the `dispatch()` router and merge `runTUI` logic into `main()`. The binary no longer accepts positional subcommands. Remove `runServe` entirely. Add `--version`, `--help`, `--verbose-errors`, and `--no-export-hint` as flags on the unified binary. All 9 flags (`--config`, `--port`, `--host`, `--log-format`, `--stream-timeout`, `--verbose-errors`, `--no-export-hint`) are available.

### Changes Required

#### 1. main.go — merge entry points

**File**: `main.go`

**Intent**: Replace `main()` → `dispatch()` → `runServe`/`runTUI` with a single `run()` function that always starts TUI+proxy.

**Contract**:
- Remove: `dispatch()`, `runServe()`, `printTopLevelHelp()`
- `main()` calls `os.Exit(run())`
- `run()` parses all 9 flags with standard `flag` package
- `--version` prints version and exits 0
- `--help` prints usage and exits 0
- Logger, port, host, config resolution identical to current `runTUI()` flow
- `verboseErrors` resolved from flag (`--verbose-errors`) + env (`FREEDIUS_VERBOSE_ERRORS==1`)
- Middleware chain: `RequestID → AccessLog → EventBus → Recover` (current TUI chain, `tui.go:125-129`)
- `checkRequiredEnvVars(cfg)` called, error logged as warning (discarded, like `tui.go:118`)
- Env-export hint printed to stderr unless `--no-export-hint` (like `main.go:208-210`)
- Server starts in goroutine (like `tui.go:139-145`)
- TUI model created with `tui.NewDashboard(bus.Subscribe(), cfg, registry, cfgPath, host, port, verboseErrors)`
- `tea.NewProgram(model).Run()` blocks until TUI exits
- Graceful shutdown on TUI exit (like `tui.go:154-158`)
- Relocate `//go:embed templates/starter.yaml` from `init.go:17` to `main.go`
- Add `-c` shorthand for `--config` flag

#### 2. tui.go — remove file

**File**: `tui.go`

**Intent**: All logic now resides in `main.go`.

**Contract**: Delete the file.

#### 3. Remove runServe-specific code

**File**: `main.go`

**Intent**: Remove serve-only flags and behavior that no longer apply.

**Contract**:
- `--verbose-errors` becomes a unified-mode flag (was serve-only; TUI had it hardcoded)
- `--no-export-hint` becomes a unified-mode flag (was serve-only)
- `checkRequiredEnvVars` no longer blocks startup (keep the function for validation messages but discard the return)
- `allowedHosts` validation stays (TUI already uses it)

#### 4. NewDashboard signature update

**File**: `proxy/tui/model.go`

**Intent**: Add `host`, `port`, `verboseErrors` to Dashboard for use by shell-install and verbose-errors toggle (Phases 5).

**Contract**: `NewDashboard(ch <-chan proxy.RequestEvent, cfg *config.Config, registry *proxy.Registry, cfgPath string, host string, port int, verboseErrors bool) *Dashboard`. Store host/port/verboseErrors in new Dashboard fields. All tests in `model_test.go` that call `NewDashboard` must be updated with the new parameters.

#### 5. init.go — remove file (keep template embed)

**File**: `init.go`

**Intent**: `runInit` is no longer called; the starter template embed moves to `main.go`.

**Contract**: Delete the file. The `//go:embed templates/starter.yaml` plus `var starterTemplate string` are already in `main.go` from step 1 above.

### Success Criteria

#### Automated Verification

- `go build -o freedius .` produces a working binary
- `go test ./...` passes (dispatch tests removed, init tests removed, remaining tests adapted)
- `./freedius --version` exits 0, prints version
- `./freedius --help` exits 0, prints usage
- `go vet ./...` clean

#### Manual Verification

- `freedius` (no args) starts TUI with proxy on `127.0.0.1:8082`
- `freedius --port 9999` starts on port 9999
- `freedius --no-export-hint` starts without the eval snippet
- Ctrl+C exits the TUI cleanly, proxy shuts down
- `freedius --config /path/to/config.yaml` loads custom config
- Sending a request to the proxy works, events appear in Tab 1

---

## Phase 4: Embedded config startup with lazy write

### Overview

When no config file exists at the resolved path, start with the embedded starter template parsed into a `*config.Config` in memory — without writing to disk. The config file is only created when the user explicitly saves from the Config tab. Existing config files load normally as before.

### Changes Required

#### 1. Config package — add LoadFromBytes

**File**: `config/config.go`

**Intent**: Provide a way to construct a validated `*Config` from YAML bytes without reading a file.

**Contract**: New function `LoadFromBytes(data []byte) (*Config, error)` that does: `yamlUnmarshalStrict` → `applyDefaults` → `validate`. Mirrors the transform step of `Load()` but accepts bytes instead of a path. No file I/O.

#### 2. main.go — lazy config initialization

**File**: `main.go`

**Intent**: On startup, attempt `config.Load(configPath)`; if the file doesn't exist, call `config.LoadFromBytes([]byte(starterTemplate))` instead. Do not write to disk.

**Contract**: In `run()`, after resolving `configPath`:
```
cfg, err := config.Load(configPath)
if err != nil && os.IsNotExist(err) {
    cfg, err = config.LoadFromBytes([]byte(starterTemplate))
}
if err != nil {
    // handle non-ENOENT load errors (malformed file, validation failure)
    return failf(...)
}
// cfgPath is passed to TUI model for future saves — config.Save(cfgPath) handles first write
```

Remove the auto-generate-and-write block from the current `runTUI` flow (equivalent to `tui.go:100-112` where it generates default config and writes it).

### Success Criteria

#### Automated Verification

- `go test ./...` passes
- `go build ./...` succeeds

#### Manual Verification

- Remove or rename all config files (`freedius.yaml`, `freedius.yml`, `~/.config/freedius/config.yaml`)
- Run `freedius` — TUI starts, Config tab shows default providers (nim, zen, go) and mappings (opus, sonnet, haiku, auto, default)
- Verify NO config file was created on disk
- Navigate to Config tab, edit a mapping, save (Enter)
- Verify `freedius.yaml` was created at the resolved path with user's changes
- Stop freedius, run again — config loads from disk with user's edits

---

## Phase 5: TUI keyboard shortcuts — shell-install and verbose-errors toggle

### Overview

Add two keyboard shortcuts: `Ctrl+S` in Config tab writes the env-block to shell RC, and `Ctrl+E` anywhere in the TUI toggles verbose error details. Both show feedback in the status bar.

### Changes Required

#### 1. Dashboard — add state fields

**File**: `proxy/tui/model.go`

**Intent**: Add fields for host, port, verboseErrors, and a transient status message.

**Contract**: Dashboard struct gains:
- `host string` and `port int` — for shell-install (set in `NewDashboard`)
- `verboseErrors bool` — current error verbosity (set in `NewDashboard`, toggled at runtime)
- `statusMessage string` — transient feedback shown in stats bar

#### 2. Verbose errors toggle (Ctrl+E)

**File**: `proxy/tui/model.go`

**Intent**: `Ctrl+E` toggles `d.verboseErrors` and updates the dispatcher's `VerboseErrors` field. Display feedback in status bar.

**Contract**: In `handleTabModeKeyPress`, add case: `ctrl+e` → toggle `d.verboseErrors` and set `d.statusMessage = "Verbose errors: ON"` or `"Verbose errors: OFF"`. The dispatcher reference is not currently in Dashboard — either store it (add `dispatcher *proxy.Dispatcher` to Dashboard) or skip dispatcher update (toggle only affects future requests if we update the dispatcher field). Include the dispatcher in Dashboard and set `d.dispatcher.VerboseErrors = d.verboseErrors` on toggle.

`NewDashboard` signature gains `dispatcher *proxy.Dispatcher` parameter (or the existing `registry` can provide access).

#### 3. Shell-install shortcut (Ctrl+S)

**File**: `proxy/tui/model.go`

**Intent**: `Ctrl+S` in Config tab writes the env-block to the user's shell RC file using `envinject.WriteShellRC`.

**Contract**: In `handleTabModeKeyPress`, when `activeTab == tabConfig` and key is `ctrl+s`:
- Get home dir via `os.UserHomeDir()`
- Get shell via `os.Getenv("SHELL")`
- Call `envinject.WriteShellRC(home, shell, d.host, d.port, false, false)` (non-force, non-dryRun)
- On success: set `d.statusMessage = "Shell RC updated ✓"`
- On error: set `d.statusMessage = fmt.Sprintf("Shell install failed: %v", err)`
- The next event or tick clears `statusMessage`

#### 4. Status bar — show transient messages

**File**: `proxy/tui/views.go`

**Intent**: When `statusMessage` is non-empty, render it in the stats bar area.

**Contract**: In `renderStatsBar`, or a new footer render function called from `Dashboard.View()`, prepend or overlay the `statusMessage` text. Clear on next `requestEventMsg` or `tea.KeyPressMsg` (in Update loop).

#### 5. Stats struct — add message field

**File**: `proxy/tui/model.go`

**Intent**: Thread the status message through to rendering.

**Contract**: `statsData` gains `message string` field. On clearing, set to `""`.

### Success Criteria

#### Automated Verification

- `go build ./...` succeeds
- `go test ./...` passes
- `go vet ./...` clean

#### Manual Verification

- Run `freedius`, press `Ctrl+E` — status bar shows "Verbose errors: ON" or "Verbose errors: OFF"
- Toggle multiple times — each press toggles state
- Navigate to Config tab, press `Ctrl+S` — shell RC is updated (verify by checking `~/.zshrc` or `~/.bashrc` for the freedius block)
- Status bar shows confirmation/dismisses on next event
- `Ctrl+S` outside Config tab — no effect

---

## Phase 6: Cleanup, tests, documentation

### Overview

Remove leftover code, tests, and targets from the removed subcommand architecture. Adapt remaining tests to the new entry point. Update README and Makefile. Remove references to `freedius serve`, `freedius init`, `freedius tui` throughout codebase comments.

### Changes Required

#### 1. main_test.go — remove obsolete tests, add new ones

**File**: `main_test.go`

**Intent**: Remove tests for removed functionality. Add tests for new flag-based entry point. Preserve useful tests for `checkRequiredEnvVars`, `newLogger`, `resolveConfigPath`.

**Contract — Remove**:
- `TestDispatch_HelpSubcommand`, `TestDispatch_VersionSubcommand`, `TestDispatch_UnknownSubcommand`, `TestDispatch_NoSubcommandRoutesToServe`
- All 11 `TestRunInit_*` tests
- `TestRunServe_EvalSnippetAppears`, `TestRunServe_EvalSnippetSuppressed`

**Contract — Add/Adapt**:
- `TestRun_StartupBanner` — adapt to run `go run . --help` instead of `go run . tui` (verify banner appears from stderr)
- `TestRun_VersionFlag` — runs binary with `--version`, asserts exit 0, stdout contains version
- `TestRun_HelpFlag` — runs binary with `--help`, asserts exit 0, stdout contains usage text
- `TestRun_EvalSnippetAppears` — runs binary, asserts stderr contains eval snippet
- `TestRun_EvalSnippetSuppressed` — runs with `--no-export-hint`, asserts stderr does NOT contain eval snippet
- Keep `TestStarterTemplate_ValidYAML` as a standalone test verifying the embedded template is valid YAML/config (independent of init.go removal)

#### 2. Makefile — update targets

**File**: `Makefile`

**Intent**: Remove `run-tui` target; `run` target starts the unified binary. Update any other targets referencing subcommands.

**Contract**:
- Remove `run-tui:` target (lines 25-26)
- `run:` target unchanged (already defaults to no subcommand)
- Verify `ci:` target (line 42: `vet generate-check test build`) still works

#### 3. README.md — update CLI documentation

**File**: `README.md`

**Intent**: Replace subcommand-based CLI reference with unified flag-based reference.

**Contract**:
- Remove sections referencing `freedius serve`, `freedius tui`, `freedius init`
- Document all flags: `--config`, `--port`, `--host`, `--log-format`, `--stream-timeout`, `--verbose-errors`, `--no-export-hint`, `--version`, `--help`
- Update usage examples

#### 4. Code comments — remove stale references

**Files**: `config/config.go`, `internal/envinject/shellrc.go`

**Intent**: Remove comments referencing `freedius init` subcommand.

**Contract**:
- `config/config.go:222` — comment referencing `freedius init` → remove
- `internal/envinject/shellrc.go:31,41` — comments `"freedius init"` → update to `"freedius"`
- `test-manual.sh` — any `$BIN init` calls → rewrite to use unified flow

#### 5. Imports — check unused

**Files**: `main.go`

**Intent**: Remove any imports that were used only by removed functions (`runInit`, `runServe`).

**Contract**: After removal, run `goimports` to clean up unused imports.

### Success Criteria

#### Automated Verification

- `go build ./...` succeeds with no unused code
- `go test ./...` all pass with updated tests
- `go vet ./...` clean
- `make ci` passes (vet, generate-check, test, build)
- `grep -r "freedius serve\|freedius tui\|freedius init\|runServe\|runInit\|dispatch(" --include="*.go" --include="*.md" --include="Makefile" --include="*.sh" .` returns zero matches (not counting plan.md/research.md)

#### Manual Verification

- Run `make` or `make run` — TUI starts correctly
- Run `./freedius --help` — usage text is accurate
- Run `./freedius --version` — version printed
- README is accurate and up to date

---

## Testing Strategy

### Unit Tests

- **RequestEvent fields**: `proxy/eventbus_test.go` — assert Method/Path populate correctly
- **Log rendering**: `proxy/tui/model_test.go` — assert output format contains expected fields, status colorization, empty state
- **Tab constant rename**: all `model_test.go` tests referencing `tabRequests` updated to `tabLog`
- **Verbose errors toggle**: test that `Ctrl+E` toggles `d.verboseErrors` boolean
- **Shell-install guard**: test that `Ctrl+S` outside Config tab has no effect; inside Config tab triggers install attempt
- **Flag parsing**: `main_test.go` — assert `--version`, `--help`, `--no-export-hint` behavior
- **Embedded config**: test `config.LoadFromBytes` with valid and invalid YAML

### Integration Tests

- **End-to-end TUI startup**: binary starts, proxy responds, events appear in log tab
- **Config lazy write**: start without config, verify no file created, edit via TUI, verify file appears
- **Shell-install end-to-end**: run with mocked `$HOME`/`$SHELL`, trigger `Ctrl+S`, verify RC file written

### Manual Testing Steps

1. Start `freedius` with no config file — verify startup works, defaults shown
2. Send a curl request, verify log line in Tab 1 with `method=`, `path=`, `status=`, `duration_ms=`
3. Press `Ctrl+E` — verify status bar shows toggle feedback
4. Navigate to Config tab, press `Ctrl+S` — verify shell RC updated
5. Edit a provider in Config tab, save — verify `freedius.yaml` created on disk
6. Restart `freedius` — verify config loads from disk with edits
7. Run `freedius --no-export-hint` — verify no eval snippet before TUI
8. Run `freedius --version` — verify version printed
9. Run `freedius --help` — verify usage text

## Performance Considerations

- Log lines in Tab 1 are denser than the old table format — the `ringBuffer` remains at 1000 capacity, which is more than adequate (1000 lines is ~80KB)
- The EventBus middleware now captures 2 additional string fields per request — negligible overhead
- Config load on startup is unchanged (same YAML parsing)
- No additional goroutines — EventBus channel capacity unchanged at 1000

## Migration Notes

- Existing config files are fully compatible — no format changes
- Users who relied on `freedius serve` for headless/Docker deployments lose that capability — this is an intentional scope decision
- Users who used `freedius init --shell-install` must now use `Ctrl+S` inside the TUI
- `freedius version` becomes `freedius --version` — muscle memory break, but standard Go convention
- The `make run-tui` Makefile target is removed; `make run` is the replacement

## References

- Research: `context/changes/unified-server-logs-tab/research.md`
- Related plan (TUI dashboard): `context/changes/tui-dashboard/plan.md` — reverses the "freedius serve unchanged" guarantee at line 55
- Related plan (TUI config setup): `context/changes/tui-config-setup/plan.md` — reverses init-as-subcommand at line 69
- Access log format source: `proxy/proxy.go:443-466` (AccessLogMiddleware)
- Event bus source: `proxy/eventbus.go:13-24` (RequestEvent), `proxy/eventbus.go:48-67` (Emit)
- TUI model: `proxy/tui/model.go:68-90` (Dashboard), `proxy/tui/model.go:183-248` (handleTabModeKeyPress)
- Subcommand dispatch: `main.go:61-89` (dispatch), `tui.go:23-161` (runTUI), `init.go:20-122` (runInit)
- Configuration: `config/config.go:48-73` (Load), `config/config.go:206-229` (Save)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Add Method and Path to RequestEvent

#### Automated

- [x] 1.1 Build passes: `go build ./...` — 88127d4
- [x] 1.2 All tests pass: `go test ./...` — 88127d4
- [x] 1.3 `go vet ./...` clean — 88127d4

#### Manual

- [x] 1.4 N/A — data model change; user-visible behavior unchanged — 88127d4

### Phase 2: Rewrite Tab 1 as access-log view

#### Automated

- [x] 2.1 `go build ./...` succeeds — 9711e02
- [x] 2.2 `go test ./proxy/tui/... -v -run "Log"` passes — 9711e02
- [x] 2.3 All existing tests pass: `go test ./...` — 9711e02
- [x] 2.4 `go vet ./...` clean — 9711e02

#### Manual

- [ ] 2.5 Tab 1 shows log lines with method=, path=, request_id=, status=, duration_ms=
- [ ] 2.6 Status colors correct (green/yellow/red)
- [ ] 2.7 Tab label shows [1] Log
- [ ] 2.8 Scrolling works with many requests

### Phase 3: Collapse subcommand dispatch

#### Automated

- [x] 3.1 `go build -o freedius .` produces working binary — 10e690a
- [x] 3.2 `go test ./...` passes (dispatch/init/serve tests removed) — 10e690a
- [x] 3.3 `./freedius --version` exits 0, prints version — 10e690a
- [x] 3.4 `./freedius --help` exits 0, prints usage — 10e690a
- [x] 3.5 `go vet ./...` clean — 10e690a

#### Manual

- [ ] 3.6 `freedius` starts TUI with proxy on 127.0.0.1:8082
- [ ] 3.7 `freedius --port 9999` starts on custom port
- [ ] 3.8 `freedius --no-export-hint` suppresses eval snippet
- [ ] 3.9 Ctrl+C exits TUI cleanly
- [ ] 3.10 `freedius --config /path/to/config.yaml` loads custom config

### Phase 4: Embedded config startup with lazy write

#### Automated

- [x] 4.1 `go test ./...` passes
- [x] 4.2 `go build ./...` succeeds

#### Manual

- [ ] 4.3 No config file → TUI starts with default providers/mappings
- [ ] 4.4 No config file created on disk
- [ ] 4.5 After editing and saving in Config tab, freedius.yaml written
- [ ] 4.6 Second run loads config from disk with edits

### Phase 5: TUI keyboard shortcuts

#### Automated

- [ ] 5.1 `go build ./...` succeeds
- [ ] 5.2 `go test ./...` passes
- [ ] 5.3 `go vet ./...` clean

#### Manual

- [ ] 5.4 Ctrl+E toggles verbose errors with status bar feedback
- [ ] 5.5 Ctrl+S in Config tab writes shell RC
- [ ] 5.6 Status bar shows confirmation, dismisses on next event
- [ ] 5.7 Ctrl+S outside Config tab has no effect

### Phase 6: Cleanup, tests, docs

#### Automated

- [ ] 6.1 `go build ./...` succeeds
- [ ] 6.2 `go test ./...` passes (all adapted tests)
- [ ] 6.3 `go vet ./...` clean
- [ ] 6.4 `make ci` passes
- [ ] 6.5 Zero stale references to old subcommands in Go/MD/Makefile/sh files

#### Manual

- [ ] 6.6 `make run` starts TUI
- [ ] 6.7 `./freedius --help` shows accurate usage
- [ ] 6.8 `./freedius --version` shows version
- [ ] 6.9 README is accurate
