<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Safe Claude Code settings injection (backup + freedius env)

- **Plan**: context/changes/claude-settings-injection/plan.md
- **Scope**: All phases (1–4), branch `feat/claude-settings-injection` @ 8174086 (stacked on PR #44 head)
- **Date**: 2026-08-09
- **Verdict**: REJECTED
- **Findings**: 1 critical · 2 warnings · 5 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | FAIL |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — `--restore` returns the freedius block, not the user's original, after a 2nd run

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: cmd/freedius/configure.go:70, internal/envinject/settings.go:101
- **Detail**: `runConfigure` backs up *unconditionally* on every invocation. Run #1 backs up the user's real `settings.json` and writes the freedius block. Run #2 then backs up the *already-overwritten* freedius block. `RestoreSettingsJSON` selects `slices.Max(backups)` → the newest → the freedius block. So after two runs, `freedius configure --restore` returns the freedius env block, NOT the original settings — directly contradicting README.md:78-81 ("`freedius configure --restore` brings them back"). Data is not lost (older `.bak` still on disk), but the single documented undo path silently returns the wrong content. `configure_test.go` asserts two backups exist but never restores after two runs — the exact gap.
- **Fix A ⭐ Recommended**: In `runConfigure`, skip the backup when the current `settings.json` already equals the freedius-authored block (compare marshalled `envBlock`), so only the user's *original* is ever backed up. Then `--restore` (newest) correctly recovers it.
  - Strength: Preserves the documented guarantee with minimal change; keeps the backup-before-write UX.
  - Tradeoff: Requires computing/Comparing the freedius block; one small helper.
  - Confidence: HIGH — the env block is already produced by `envBlock`.
  - Blind spot: Edge case where the user manually edited the freedius block (rare).
- **Fix B**: Restore the oldest non-freedius backup instead of `slices.Max`.
  - Strength: No change to backup path.
  - Tradeoff: Fragile heuristic; a freedius block could sort oldest in odd cases.
  - Confidence: MEDIUM.
  - Blind spot: Assumes only one non-freedius backup exists.
- **Decision**: FIXED via Fix A (commit on feat/claude-settings-injection)

### F2 — Backup/restore widen file permissions to 0o644, exposing secrets

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: internal/envinject/settings.go:51, settings.go:110
- **Detail**: `os.WriteFile(dst, data, 0o644)` hardcodes world-readable mode for the `.bak` file, which is a copy of the user's `~/.claude/settings.json` — whose `env` block commonly holds a real `ANTHROPIC_API_KEY`. Verified: a `0600` source produced a `0644` backup. The `#nosec G306` comment claims the mode "mirrors settings.json's permissions", which the code does not do. Restore writes the live file back at `0644` too. `WriteSettingsJSON`'s own `0o644` at :157 is justified (Claude Code must read it) — the backup does not need to be world-readable.
- **Fix**: `os.Stat(src)` and reuse `fi.Mode().Perm()` for both the backup and restore writes; fall back to `0o600` for the backup. Correct the `#nosec` comment.
  - Strength: Removes the secret-exposure class; matches `shellrc.go`/`main.go` stat-and-reuse pattern.
  - Tradeoff: Minor — a few lines; needs a test for the permission path.
  - Confidence: HIGH — identical pattern used elsewhere in the repo.
  - Blind spot: None significant.
- **Decision**: FIXED (commit on feat/claude-settings-injection)

### F3 — `RestoreSettingsJSON` is destructive with no undo

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: internal/envinject/settings.go:110
- **Detail**: Restore overwrites the live `settings.json` without backing it up first — the inverse of the care taken in `runConfigure`. A mistaken `--restore` is unrecoverable. Compounds F1 (a wrong restore is now likely).
- **Fix**: Call `BackupSettingsJSON` at the top of `RestoreSettingsJSON` (or from `runConfigure` before the restore branch) so the pre-restore state is preserved.
  - Strength: Makes restore itself reversible; consistent with the backup-first design.
  - Tradeoff: One extra `.bak` written on restore.
  - Confidence: HIGH.
  - Blind spot: None significant.
- **Decision**: FIXED (commit on feat/claude-settings-injection) — restore now snapshots the live file to a separate `settings.json.prerestore.<ts>` (distinct prefix, so it does not shadow the user's real `.bak` and break F1's "newest = original" invariant).

### F4 — `configure --help` prints server usage; `printConfigureUsage` is near-dead

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/main.go:58, cmd/freedius/configure.go:34, configure.go:125-146
- **Detail**: `handleEarlyArgs` scans all args for `--help`/`-h` and returns before the `configure` dispatch, so `freedius configure --help` prints the top-level server flag list. The 22-line `printConfigureUsage` (which re-declares all five flags just for `PrintDefaults()`) is only reachable on a subcommand parse error.
- **Fix**: Move the subcommand dispatch above `handleEarlyArgs` (or skip early-args when `args[0]` is a known subcommand) and route `--help` to `printConfigureUsage`. Add `TestRunConfigure_HelpPrintsConfigureUsage`.
- **Decision**: FIXED (commit on feat/claude-settings-injection) — configure dispatch now runs before handleEarlyArgs; added TestRunConfigure_HelpReturnsZero + TestPrintConfigureUsage_WritesFlags.

### F5 — Backup path selection is TOCTOU; silent clobber after 100 collisions

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: internal/envinject/settings.go:60-72
- **Detail**: stat-then-write between `uniqueBackupPath` and `os.WriteFile`. Concurrent runs in the same second can pick the same path, losing a backup. The `i < 100` loop falls through to `return base` (settings.go:71), silently clobbering an existing backup. Unlikely for an interactive CLI but the fallback should error instead.
- **Fix**: Use `os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)`; loop until success or return a real error. Removes the race.
- **Decision**: PENDING

### F6 — Fixed `.tmp` name and no fsync; orphan tmp on failed rename

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: internal/envinject/settings.go:155
- **Detail**: `path + ".tmp"` is constant, so concurrent runs interleave, and a failed rename at :160 leaves an orphan `settings.json.tmp` in `~/.claude/`.
- **Fix**: Use `os.CreateTemp(dir, "settings.json.*.tmp")` + cleanup on error. (The atomic write-then-rename here is a genuine improvement over `shellrc.go:119` — consider propagating that direction.)
- **Decision**: PENDING

### F7 — Dry-run misleading + local-time timestamps

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: cmd/freedius/configure.go:58, internal/envinject/settings.go:49
- **Detail**: Dry-run unconditionally prints "would back up …" even when there is nothing to back up (the real path at :70-74 distinguishes the no-file case). Backup timestamps use `time.Now().Format(...)` without `.UTC()` — a backward DST shift or clock correction breaks the lexicographic-equals-chronological invariant that `slices.Max` (settings.go:101) depends on.
- **Fix**: Mirror the no-file conditional in the dry-run message; use `time.Now().UTC().Format(...)`.
- **Decision**: PENDING

### F8 — Duplicated flag declarations + resolver

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/configure.go:98-107, configure.go:125-146
- **Detail**: `configureDir` re-implements the `$HOME/.claude` default that `envinject.resolveConfigDir` already owns (unexported, so unreachable from `configure`). `printConfigureUsage` builds a *second* FlagSet re-declaring all five flags just for `PrintDefaults()` — a drift hazard (flag defs must be kept in sync by hand).
- **Fix**: Export `envinject.ResolveConfigDir` and reuse it; have `printConfigureUsage` reuse the primary FlagSet's `PrintDefaults()` instead of re-declaring.
- **Decision**: PENDING

## Notes

- **By-design, not a finding**: the safety review's point that `configure` ignores `FREEDIUS_PORT`/`FREEDIUS_HOST` is the plan's explicit "What We're NOT Doing" decision (fixed `127.0.0.1:8082`). Dismissed as intended scope.
- Benign plan "drifts" from Agent 1 (renamed `MalformedEnvReplaced`→`MalformedFileReplaced` test; dry-run not calling `BackupSettingsJSON`) are correct resolutions of a self-contradictory plan section, not defects.
- Automated success criteria pass: `go test ./internal/envinject ./cmd/freedius` → 58 passed; `go vet ./...`, `mage lint` → 0 issues. Manual criteria 3.8 / 4.4 remain unchecked (require a real terminal run + human read) — expected for a backup-into-$HOME feature.
