# Unified Server-Logs Tab + Single Entry Point — Plan Brief

> Full plan: `context/changes/unified-server-logs-tab/plan.md`
> Research: `context/changes/unified-server-logs-tab/research.md`

## What & Why

Replace the TUI's first tab (columnar Requests table) with access-log-style log lines matching the `AccessLogMiddleware` output format. Collapse the five-subcommand CLI (`serve`, `tui`, `init`, `version`, `help`) into a single `freedius` binary that always starts the TUI+proxy. The motivation is simplification: one mode, one code path, no subcommand learning curve.

## Starting Point

The codebase currently dispatches 5 subcommands via `main.go:61-89`. `serve` and `tui` each spin up their own HTTP server with nearly identical config — the only architectural difference is `EventBusMiddleware` in the TUI chain (`tui.go:125-129`). Tab 1 renders a columnar table of `HH:MM:SS STATUS model provider latency` (`proxy/tui/views.go:33-88`). The `RequestEvent` struct (`proxy/eventbus.go:13-24`) has 9 fields but is missing `Method` and `Path`. Config auto-generates and writes to disk on first run when no file is found.

## Desired End State

`freedius` starts the TUI+proxy with no subcommands. Tab 1 shows human-readable access-log lines (`time=... request_id=... method=POST path=/v1/messages status=200 duration_ms=42 matched_provider=nim`). The binary starts with an embedded default config in memory — no file is written until the user explicitly saves from the Config tab. Flags: `--version`, `--help`, `--verbose-errors`, `--no-export-hint`, plus existing `--config`, `--port`, `--host`, `--log-format`, `--stream-timeout`. TUI shortcuts: `Ctrl+E` toggles verbose errors, `Ctrl+S` in Config tab writes env vars to shell RC.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Headless mode | Removed — TUI-only | User wants single mode with zero CLI complexity; Docker/systemd use cases accepted as out of scope. | Plan |
| Config first run | Embedded defaults, lazy write | Start from embedded starter template in memory; only persist when user saves — no side effects on first run. | Plan |
| Log format in TUI | Always human-readable key=value | Terminal-optimized, matches AccessLogMiddleware text output, easier to scan than JSON. | Plan |
| Init/bootstrap | Auto-detect, start with embedded config | Detect no config file, parse embedded template into memory, proceed to TUI without writing to disk. | Plan |
| Shell-install | TUI shortcut Ctrl+S in Config tab | Self-contained within TUI; discovered naturally while exploring config; no separate CLI flag. | Plan |
| Version/help | `--version` and `--help` flags | Standard Go CLI convention; simpler dispatch. | Plan |
| Verbose errors | `--verbose-errors` flag + Ctrl+E toggle | Flag sets startup state; runtime toggle for live debugging. | Plan |
| Env-export hint | Printed at startup, suppressible with `--no-export-hint` | Discoverability for first-time users; suppressible for experienced users. | Plan |
| Settings.json | Not written | `~/.claude/settings.json` auto-write removed; shell RC via Ctrl+S is the canonical env setup. | Plan |

## Scope

**In scope:**
- Add `Method` and `Path` to `RequestEvent`
- Rewrite `renderRequestsTab` → `renderLogTab` with log-line format
- Remove `dispatch()`, `runServe()`, `runInit()`, `printTopLevelHelp()`
- Merge `runTUI` logic into `main()`
- Embedded config startup (no auto-write on first run)
- `Ctrl+E` verbose-errors toggle in TUI
- `Ctrl+S` shell-install shortcut in Config tab
- Remove subcommand-related tests, add flag-based tests
- Update README, Makefile, code comments

**Out of scope:**
- Headless/Docker mode
- JSON log format in TUI
- `freedius version` or `freedius help` positional subcommands (only `--version`/`--help`)
- Adapter-level runtime verbose-errors toggle (only `Dispatcher.VerboseErrors` toggles)
- `~/.claude/settings.json` auto-write
- Config file format changes

## Architecture / Approach

The unified `run()` in `main.go` replaces all three entry points (`runServe`, `runTUI`, `runInit`). It parses flags, creates the middleware chain (RequestID → AccessLog → EventBus → Recover), starts the HTTP server in a goroutine, then runs the Bubble Tea TUI program. Server shutdown is TUI-driven (not signal-driven). Config loading tries the resolved path first; on ENOENT falls back to parsing the embedded starter template in memory. The config path is stored in the TUI model for future saves.

Data flow for log tab: `EventBusMiddleware` populates `RequestEvent` with all fields → `EventBus.Emit()` sends to channel → TUI's `waitForEvent()` reads → `eventLog.push()` stores in ring buffer → `renderLogTab()` formats as `time=... request_id=...` lines on render.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Add Method/Path fields | Extended `RequestEvent` with Method and Path | Low — backward-compatible struct extension |
| 2. Rewrite Tab 1 rendering | Log-line format matching AccessLogMiddleware output | Low — rendering change only |
| 3. Collapse subcommand dispatch | Single `freedius` binary, TUI-only | Medium — removes ~200 lines of entry-point code; must preserve all flag resolution logic |
| 4. Embedded config + lazy write | No auto-file-generation on first run; save only on user edit | Low — new code path in config loading |
| 5. TUI shortcuts | Ctrl+E (verbose toggle), Ctrl+S (shell-install) | Medium — requires threading dispatcher reference into TUI model |
| 6. Cleanup, tests, docs | Zero references to old subcommands; passing tests | Medium — 25+ test removals/adaptations |

**Prerequisites:** Go 1.22+, existing build toolchain, `make` available
**Estimated effort:** ~4-6 sessions across 6 phases

## Open Risks & Assumptions

- **Dispatcher.VerboseErrors toggle**: Only affects the dispatcher-level error response — adapters that have `ErrorHandler` set at construction time won't see the runtime change. This is acceptable since dispatch/validation errors are the primary debugging target.
- **Shell-install failure**: If `$SHELL` is set to an unsupported shell (tcsh, csh, etc.), `envinject.WriteShellRC` returns an error. The TUI shows this in the status bar — user must set up env vars manually.
- **Config path resolution**: When no config file exists and no `--config` flag is given, the resolved path defaults to `~/.config/freedius/config.yaml`. This directory may not be writable — `config.Save()` handles write errors with rollback, and the TUI form shows the error.
- **Tab constant rename**: `tabRequests` → `tabLog` is referenced in ~20 locations. Mechanical rename is low-risk but must be grep-complete.

## Success Criteria (Summary)

- `freedius` starts TUI+proxy, Tab 1 shows log lines with `method=`, `path=`, `request_id=`, `status=`, `duration_ms=`
- No config file created on first run until user saves from Config tab
- `Ctrl+E` toggles verbose errors, `Ctrl+S` in Config tab writes shell RC
- All tests pass, no references to removed subcommands remain in codebase
