# Safe Claude Code settings injection — Implementation Plan

## Overview

Add a `freedius configure` subcommand that gives the operator a **backup-first,
non-destructive** way to point Claude Code at the local freedius proxy. It backs
up `~/.claude/settings.json` to a timestamped `settings.json.bak.<ts>`, then
writes a clean `{"env": {...}}` block containing freedius's env vars (the same
Anthropic-shaped vars the startup snippet already prints). A `--restore` flag
undoes the last write from the newest backup. This keeps the config write inside
the operator's own terminal process — no web-server-into-$HOME boundary crossing
— consistent with freedius's terminal-first design.

## Current State Analysis

- `internal/envinject/WriteSettingsJSON` (`internal/envinject/settings.go:25`)
  exists but has **zero callers** (dead code). It **merges**: it preserves every
  existing top-level key and only sets/updates the `"env"` object (`:52`).
- Tests `TestWriteSettingsJSON_MergePreservesKeys` (`:52`) and
  `TestWriteSettingsJSON_MalformedEnvReplaced` (`:87`) in `internal/envinject/envinject_test.go`
  **enforce the merge behavior**. The "overwrite only freedius's needs" decision
  in this plan **reverses** that, so those tests become invalid and must be
  updated/removed.
- `envBlock` (`settings.go:11`) hardcodes Anthropic-shaped vars
  (`ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY="freedius-dummy"`, `ENABLE_TOOL_SEARCH`,
  `DISABLE_TELEMETRY`, `DISABLE_ERROR_REPORTING`). This is correct today — the
  only client is Claude Code — and is left Anthropic-only per the planning
  decision (structured so a future `--client` switch is easy, but not built now).
- `cmd/freedius/main.go` is a **flat-flag** CLI: `run()` parses flags directly
  with no subcommand dispatch (`handleEarlyArgs` at `:355` only handles
  `--version`/`--help`). `main.go:151` prints `envinject.Snippet` to stderr but
  never writes a file.
- `defaultHost="127.0.0.1"` / `defaultPort=8082` constants already exist at
  `main.go:31-32`; `configure` will reuse them as the injected address.

### Key Discoveries:

- The "overwrite" decision directly conflicts with the frame's own "What Changes"
  footer (which still says "merge"); the Reframed problem statement and the
  user's explicit choice win — **overwrite, not merge**.
- `WriteSettingsJSON`'s merge logic and the two tests that pin it are the only
  things standing between us and the overwrite behavior; both are in-scope to change.
- A `BackupSettingsJSON` / `RestoreSettingsJSON` pair belongs in `internal/envinject`
  (same package that already owns the `~/.claude` write), keeping it testable
  independent of the CLI.

## Desired End State

An operator runs `freedius configure` in the same shell they use for `claude`:
freedius prints a short summary of what it will write and the backup path, asks
`y/N`, backs up `settings.json`, and writes the freedius env block. Their original
settings are fully restorable via `freedius configure --restore`. `go test ./...`
passes, including the rewritten envinject tests.

## What We're NOT Doing

- **No UI button / web-into-$HOME write** — the dashboard action is explicitly a
  fallback, not built here (boundary-crossing smell, per the frame).
- **No `--client` switch** — env vars stay Anthropic-shaped; a future generic
  client flag is out of scope.
- **No `--host`/`--port` flags on `configure`** — the injected address is the fixed
  `127.0.0.1:8082` default (operator accepted the mismatch risk for custom binds).
- **No whole-directory backup** — only `settings.json` is backed up (the only file
  freedius touches).
- **No shell-rc writing from this command** — `WriteShellRC` (`shellrc.go`) is a
  separate concern (TUI Ctrl+S); not wired here.
- **No merge / key preservation** — pre-existing custom keys live only in the
  backup, by design.

## Implementation Approach

Three layered changes plus docs:
1. Add backup/restore primitives to `internal/envinject`.
2. Rewrite `WriteSettingsJSON` to overwrite-only (drop merge), fixing the tests.
3. Add the `configure` subcommand in `cmd/freedius` that orchestrates
   backup → summary → confirm → write (or restore), reusing phases 1–2.
4. Document the one-liner in README and record the change on the roadmap.

## Critical Implementation Details

- **Overwrite reverses merge — tests must change.** `WriteSettingsJSON` today
  merges and is pinned by `TestWriteSettingsJSON_MergePreservesKeys` and
  `TestWriteSettingsJSON_MalformedEnvReplaced`. Switching to overwrite-only makes
  both tests wrong; delete/replace them and add a test asserting other keys are
  discarded. This is the single load-bearing gotcha in the plan.
- **Subcommand dispatch order.** `freedius configure ...` must be intercepted
  *before* the flat `flag.NewFlagSet("freedius", ...)` parse in `run()`, otherwise
  the subcommand's flags get parsed as server flags and `configure` is treated as
  a positional arg. Add a guard at the top of `run()` (after `handleEarlyArgs`):
  if `len(args) > 0 && args[0] == "configure"`, call `runConfigure(args[1:])` and
  return its code.
- **Backup filename + restore ordering.** Use
  `settings.json.bak.<ts>` with `ts = time.Now().Format("20060102-150405")` so
  lexicographic `max` selects the newest backup deterministically. Restore errors
  clearly when no `settings.json.bak.*` exists.
- **`y/N` prompt reads stdin** (`bufio.Reader.ReadString('\n')`); accept `y`/`yes`
  (case-insensitive). `--yes`/`-y` skips the prompt for scripting. `--dry-run`
  prints the would-be JSON + backup path and writes nothing (no prompt needed).
- **Atomic write preserved.** Keep the existing tmp-file + `os.Rename` and
  `MkdirAll(0o700)` behavior from `WriteSettingsJSON`; just change the payload
  from merge to the freedius-only `{"env": envBlock(...)}`.

## Phase 1: Backup / Restore primitives

### Overview

Give the `envinject` package two small, independently-testable functions that
manage the timestamped backup lifecycle, so the CLI (Phase 3) and tests don't
touch filesystem ordering logic directly.

### Changes Required:

#### 1. `internal/envinject/settings.go`

**Intent**: Add `BackupSettingsJSON` (copy live `settings.json` →
`settings.json.bak.<ts>`; no-op returning `("", nil)` when the file is absent) and
`RestoreSettingsJSON` (find the newest `settings.json.bak.*`, copy it back to
`settings.json`; return an error when none exists). Both resolve `configDir` the
same way `WriteSettingsJSON` does (`$HOME/.claude` when empty).

**Contract**:
```go
// BackupSettingsJSON copies the existing settings.json (if any) to a
// timestamped settings.json.bak.<ts> in the same dir. Returns ("", nil) when
// there is no source file to back up.
func BackupSettingsJSON(configDir string) (string, error)

// RestoreSettingsJSON restores the newest settings.json.bak.* back to
// settings.json. Returns an error when no backup exists.
func RestoreSettingsJSON(configDir string) (string, error)
```
Backup timestamp format must sort lexicographically: `time.Now().Format("20060102-150405")`.
`RestoreSettingsJSON` selects the entry with the highest filename via `filepath.Base`
comparison (or `slices.Max` over matching names).

### Success Criteria:

#### Automated Verification:

- `BackupSettingsJSON` copies an existing file to `settings.json.bak.<ts>` and returns the new path.
- `BackupSettingsJSON` returns `("", nil)` (no write) when `settings.json` is absent.
- `RestoreSettingsJSON` restores the newest of multiple backups to `settings.json`.
- `RestoreSettingsJSON` returns a clear error when no `settings.json.bak.*` exists.
- `go test ./internal/envinject` passes.

#### Manual Verification:

- (none)

## Phase 2: Overwrite-only `WriteSettingsJSON`

### Overview

Change the writer from merge to overwrite-only (freedius's env block), and fix
the two tests that falsely pin merge behavior. This is the behavioral pivot the
user chose.

### Changes Required:

#### 1. `internal/envinject/settings.go`

**Intent**: Rewrite `WriteSettingsJSON(configDir, host, port, dryRun)` to write a
fresh `{"env": envBlock(host, port)}` document — discarding any prior keys — while
keeping the atomic tmp+rename + `MkdirAll` + `dryRun` behavior. Remove the
read-existing / merge / malformed-replace logic (`:37-52`).

**Contract**: After the call, `settings.json` contains exactly
`{"env": {"ANTHROPIC_BASE_URL": "...", "ANTHROPIC_API_KEY": "freedius-dummy", "ENABLE_TOOL_SEARCH": "true", "DISABLE_TELEMETRY": "1", "DISABLE_ERROR_REPORTING": "1"}}`
(no other top-level keys). Signature unchanged.

#### 2. `internal/envinject/envinject_test.go`

**Intent**: Remove `TestWriteSettingsJSON_MergePreservesKeys` and
`TestWriteSettingsJSON_MalformedEnvReplaced` (they assert the old merge contract);
keep `TestWriteSettingsJSON_CreatesNew` and `TestWriteSettingsJSON_DryRunNoWrite`;
add `TestWriteSettingsJSON_OverwriteDiscardsOtherKeys` asserting a pre-existing
`{"project":"x"}` file ends up containing only the `env` block after a write.

### Success Criteria:

#### Automated Verification:

- `WriteSettingsJSON` writes only the `env` block (no other keys survive).
- `dryRun` still prints the JSON and writes no file.
- `go test ./internal/envinject` passes with the replaced/added tests.

#### Manual Verification:

- (none)

## Phase 3: `freedius configure` subcommand

### Overview

Wire the subcommand into the CLI entry and implement the orchestration:
backup → summary → confirm → write, or restore. Reuses Phase 1–2 functions.

### Changes Required:

#### 1. `cmd/freedius/main.go` (dispatch)

**Intent**: Intercept `configure` as the first positional arg in `run()` before
the flat flag parse, delegating to `runConfigure(args[1:])`. Mention `configure`
in `printUsage`.

**Contract**: At the top of `run()`, after `handleEarlyArgs`, add:
```go
if len(args) > 0 && args[0] == "configure" {
    return runConfigure(args[1:])
}
```
`printUsage` gains one line: `freedius configure [--config-dir DIR] [--restore] [--dry-run] [--yes]`.

#### 2. `cmd/freedius/configure.go` (new)

**Intent**: Implement `runConfigure(args []string) int`. Parse a local
`flag.FlagSet` for `--config-dir` (default `$HOME/.claude`), `--restore`,
`--dry-run`, `--yes`/`-y`. Resolve the injected host/port from the existing
`defaultHost`/`defaultPort` constants. When `--restore`: call
`RestoreSettingsJSON`, print result, return. Otherwise: call `BackupSettingsJSON`,
print a summary (the env block + backup path), and unless `--yes` prompt `y/N` on
stdin; on confirm (or `--yes`) call `WriteSettingsJSON`. In `--dry-run`, print the
would-be JSON and backup path and write nothing. Use `failf` for errors and
non-zero exit on failure.

**Contract**:
```go
func runConfigure(args []string) int
```
Behavior table:
- `--restore` → `RestoreSettingsJSON(configDir)`; print "restored <path>".
- `--dry-run` (no restore) → `BackupSettingsJSON` (no write), print JSON + backup path, no file change.
- default → `BackupSettingsJSON`, summary, y/N (skip if `--yes`), then `WriteSettingsJSON`.
- On any error → `failf(...)` and return 1.

#### 3. `cmd/freedius/configure_test.go` (new)

**Intent**: Test the orchestration headlessly against a temp dir: `--dry-run
--config-dir TMP` writes nothing and exits 0; a full run creates `settings.json`
and a `.bak`; a second run creates another `.bak`; `--restore` restores the
newest backup. Since `runConfigure` prints to stdout/stderr, assert on filesystem
state (file existence, `.bak` count, `env` content) rather than stdout.

### Success Criteria:

#### Automated Verification:

- `freedius configure --dry-run --config-dir <tmp>` writes nothing, exits 0, and the would-be content is valid JSON.
- `freedius configure --config-dir <tmp>` (no existing file) writes `settings.json` and makes no backup.
- A second `freedius configure --config-dir <tmp>` creates a new timestamped `.bak` and overwrites `settings.json`.
- `freedius configure --restore --config-dir <tmp>` restores the newest backup to `settings.json`.
- `--yes` runs without a stdin prompt.
- `go build ./...`, `go vet ./...`, and `mage lint` pass.

#### Manual Verification:

- Real run with no `--config-dir` backs up `~/.claude/settings.json` to a timestamped `.bak`, writes freedius's env block, and `claude` afterward routes through freedius; `freedius configure --restore` returns the original settings.

## Phase 4: Docs + roadmap

### Overview

Surface the one-command path in the README (the canonical Claude-Code wiring was
previously only an undocumented startup snippet) and record the change on the
roadmap per the request.

### Changes Required:

#### 1. `README.md`

**Intent**: In the Claude-Code wiring / Quickstart section, add `freedius
configure` as the backup-safe one-liner, noting it backs up `settings.json` and
that `freedius configure --restore` undoes it. Reference the command, don't inline
the env vars (the startup snippet stays canonical).

**Contract**: New subsection or bullet under the existing Claude Code setup; keep
the `envinject.Snippet` reference as the manual alternative.

#### 2. `context/foundation/roadmap.md`

**Intent**: Add a row for `claude-settings-injection` under the S-04 (env
auto-injection) group as `S-04a`, and bump the frontmatter `updated` date to
`2026-08-09`.

**Contract**:
```
| S-04a | claude-settings-injection | Safe Claude Code settings injection (backup + freedius env) | — | 2026-08-09 |
```

### Success Criteria:

#### Automated Verification:

- README contains `freedius configure` and a mention of the backup / `--restore`.
- `roadmap.md` contains the `claude-settings-injection` / `S-04a` row.
- `mage lint` (markdown/link checks if any) passes.

#### Manual Verification:

- Docs read correctly and the one-liner is easy to find from the Quickstart.

## Testing Strategy

### Unit Tests:

- `internal/envinject`: backup no-op / creates-timestamped / restore-newest / restore-no-backup-error; write-overwrite-discards-keys / dry-run-no-write / creates-new.
- `cmd/freedius/configure_test.go`: dry-run no write; first run writes; second run adds `.bak`; restore returns newest.

### Integration Tests:

- End-to-end via the built binary in `cmd/freedius/configure_test.go` (executes `runConfigure` in-process against temp dirs).

### Manual Testing Steps:

1. Run `freedius configure` against a throwaway `$HOME` copy; confirm `.bak` + new `settings.json`.
2. Open Claude Code and confirm requests hit freedius (proxy logs show activity).
3. Run `freedius configure --restore`; confirm `settings.json` matches the original.

## Performance Considerations

Negligible — single small JSON file write/restore, no hot path.

## Migration Notes

None — `settings.json` is operator-owned and the backup guarantees reversibility.
Existing users with a custom `settings.json` are unaffected until they opt in by
running `configure`.

## References

- Frame brief: `context/changes/claude-settings-injection/frame.md`
- Client-setup snippet: `cmd/freedius/main.go:151`, `internal/envinject/snippet.go`
- Unused merge writer: `internal/envinject/settings.go:25`
- Shell-rc precedent (not wired here): `internal/envinject/shellrc.go`

## Progress

### Phase 1: Backup / Restore primitives

#### Automated

- [x] 1.1 `BackupSettingsJSON` copies existing file to `settings.json.bak.<ts>` and returns the path — 42e5893
- [x] 1.2 `BackupSettingsJSON` returns `("", nil)` when `settings.json` is absent — 42e5893
- [x] 1.3 `RestoreSettingsJSON` restores the newest of multiple backups — 42e5893
- [x] 1.4 `RestoreSettingsJSON` errors clearly when no backup exists — 42e5893
- [x] 1.5 `go test ./internal/envinject` passes — 42e5893

### Phase 2: Overwrite-only `WriteSettingsJSON`

#### Automated

- [x] 2.1 `WriteSettingsJSON` writes only the `env` block (no other keys) — fbd432a
- [x] 2.2 `dryRun` prints JSON and writes no file — fbd432a
- [x] 2.3 Merge/malformed tests removed; overwrite-discards-keys test added; `go test ./internal/envinject` passes — fbd432a

### Phase 3: `freedius configure` subcommand

#### Automated

- [x] 3.1 `configure` dispatch intercepted before flat flag parse; `printUsage` mentions it — ff72057
- [x] 3.2 `--dry-run --config-dir <tmp>` writes nothing, exits 0, valid JSON — ff72057
- [x] 3.3 first run writes `settings.json`, no backup when absent — ff72057
- [x] 3.4 second run creates a new timestamped `.bak` and overwrites — ff72057
- [x] 3.5 `--restore` restores newest backup — ff72057
- [x] 3.6 `--yes` skips stdin prompt — ff72057
- [x] 3.7 `go build ./...`, `go vet ./...`, `mage lint` pass — ff72057

#### Manual

- [ ] 3.8 real run backs up `~/.claude/settings.json`, `claude` routes through freedius, `--restore` returns original

### Phase 4: Docs + roadmap

#### Automated

- [x] 4.1 README contains `freedius configure` + backup/`--restore` note — aa13485
- [x] 4.2 `roadmap.md` has the `claude-settings-injection` / `S-04a` row — aa13485
- [x] 4.3 `mage lint` passes — aa13485

#### Manual

- [ ] 4.4 docs read correctly and one-liner is easy to find
