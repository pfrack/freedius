# Safe Claude Code settings injection — Plan Brief

> Full plan: `context/changes/claude-settings-injection/plan.md`
> Frame brief: `context/changes/claude-settings-injection/frame.md`

## What & Why

Operators want to run Claude Code through freedius but are afraid of clobbering
their custom `~/.claude/settings.json`. This adds a backup-first `freedius
configure` command that writes freedius's env block as a clean `{"env": {...}}`
and a `freedius configure --restore` to undo it — so pointing Claude Code at
freedius is a one-liner, not a risky manual edit.

## Starting Point

freedius already prints the env-var snippet to stderr on startup
(`envinject.Snippet`), and an unused `WriteSettingsJSON` exists that *merges* into
`settings.json`. Nothing writes or backs up the file today, and the CLI is
flat-flag only with no subcommand dispatch.

## Desired End State

An operator runs `freedius configure` in the same shell as `claude`; freedius
backs up `settings.json` to a timestamped `.bak`, writes its env block, and
`claude` transparently routes through the local proxy. The original settings are
one `freedius configure --restore` away.

## Key Decisions Made

| Decision              | Choice                              | Why (1 sentence)                                                                 | Source           |
| --------------------- | ----------------------------------- | -------------------------------------------------------------------------------- | ---------------- |
| Surface               | `freedius configure` subcommand     | Matches terminal-first design; avoids web-server writing into `$HOME/.claude`.   | Frame / Plan     |
| Write semantics       | Backup + overwrite (freedius-only)  | User's explicit preference; original fully restorable from backup.               | Plan (reframes frame footer) |
| Backup scope          | `settings.json` only                | Only file freedius touches; lighter than whole-dir backup.                      | Plan             |
| Injected address      | Fixed `127.0.0.1:8082`              | Matches default bind; no extra flags.                                            | Plan             |
| Restore               | `--restore` flag                    | One-command undo, no manual `cp`.                                                | Plan             |
| Re-run behavior       | Always back up                      | Simple + full history; multiple `.bak` accumulate.                              | Plan             |
| Confirm UX            | Summary + `y/N` (skip with `--yes`) | Safe for a file-overwrite; still scriptable.                                     | Plan             |
| Client shape          | Anthropic-only now                  | Only Claude Code is used; structured for a future `--client` switch, not built. | Plan             |
| `--config-dir`        | Supported (default `$HOME/.claude`) | Enables testing against a temp dir.                                              | Plan             |
| `--dry-run`           | Exposed                             | Reuses existing dryRun plumbing for a safe preview.                             | Plan             |

## Scope

**In scope:** `BackupSettingsJSON`/`RestoreSettingsJSON` in `internal/envinject`;
overwrite-only `WriteSettingsJSON` (+ test fixes); `freedius configure`
subcommand with `--config-dir`/`--restore`/`--dry-run`/`--yes`; README + roadmap
entry.

**Out of scope:** Web UI button; `--client` switch; `--host`/`--port` flags on
`configure`; whole-directory backup; shell-rc writing from this command; key
merging/preservation.

## Architecture / Approach

Three layers: (1) backup/restore primitives in `internal/envinject`; (2) an
overwrite-only writer reusing the existing atomic tmp+rename; (3) a `configure`
subcommand intercepted before the flat flag parse in `main.go` that orchestrates
backup → summary → confirm → write (or restore). The CLI reuses `defaultHost`/
`defaultPort` and the existing `envBlock`. All filesystem logic lives in the
package so it's unit-testable independent of the binary.

## Phases at a Glance

| Phase | What it delivers                          | Key risk                                    |
| ----- | ----------------------------------------- | ------------------------------------------- |
| 1     | Backup/restore primitives + tests         | Timestamp format must sort newest-first     |
| 2     | Overwrite-only writer + test fixes        | Reversing merge breaks two existing tests   |
| 3     | `configure` subcommand + CLI tests        | Subcommand must precede flat flag parse      |
| 4     | README + roadmap entry                    | None technical; doc accuracy                |

**Prerequisites:** None — builds on existing `envinject` package and CLI constants.
**Estimated effort:** ~1 session across 4 phases (small, well-bounded change).

## Open Risks & Assumptions

- Injected address is the fixed default; custom binds need a manual edit or a future flag.
- `settings.json` `env` schema assumes Claude Code's string→string `env` object (unchanged).
- Overwrite discards pre-existing custom keys by design — restore is the only way back.

## Success Criteria (Summary)

- `freedius configure` backs up then writes freedius's env block; `claude` routes through freedius.
- `freedius configure --restore` returns the original `settings.json` exactly.
- `go test ./...` and `mage lint` pass, including the rewritten envinject tests.
