<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Magefile Migration

- **Plan**: `context/changes/magefile/plan.md`
- **Scope**: All phases (1-2)
- **Date**: 2026-06-21
- **Verdict**: APPROVED
- **Findings**: 0 critical, 5 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS ✅ |
| Scope Discipline | PASS ✅ |
| Safety & Quality | WARNING ⚠️ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | PASS ✅ |

## Findings

### W1 — `sh -c` shell fork for error message

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:69`
- **Detail**: `LintGolangci` uses `sh.RunV("sh", "-c", ...)` just to print an error message and `exit 1`. Forks a shell when Go can handle this directly.
- **Fix**: Replace with `fmt.Fprintln(os.Stderr, "golangci-lint not found...")` + `return fmt.Errorf("golangci-lint not found")`.

### W2 — Per-file subprocess spawning in Format

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — repo is small (~30 .go files); acceptable performance
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:126-146`
- **Detail**: Each `.go` file triggers 4 subprocesses (gofmt, goimports, golines, gci). For small repos this is fine but doesn't scale.
- **Fix**: Collect all `.go` paths first, then invoke each tool once with all files as args.

### W3 — gci section names may require v0.13+

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — `go install` gets latest; only hits if gci is pre-installed as older version
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:142-145, 171-174`
- **Detail**: `-s "alias" -s "localmodule"` sections require gci v0.13+. `InstallGci` doesn't pin a version.
- **Fix**: Pin `@v0.13.5` in `InstallGci`, or document the minimum version.

### W4 — Silently discarded git ls-files error

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — failure is extremely rare; consequence is empty untracked list (safe)
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:156`
- **Detail**: `untracked, _ := sh.Output(...)` discards the error from `git ls-files --others`.
- **Fix**: Use `mg.Warn(err)` if error is non-nil, or propagate.

### W5 — Hardcoded `.git/hooks/pre-commit` path

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — works for 99% of setups; git worktrees need fixing
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:93`
- **Detail**: `sh.Copy(".git/hooks/pre-commit", ...)` fails in git worktrees where `.git` is a file.
- **Fix**: Use `git rev-parse --git-path hooks/pre-commit` to resolve dynamically.

### O1 — Format/FormatChanged handling of magefiles

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Pattern Consistency
- **Location**: `magefiles/mage.go:183-191`
- **Detail**: `isVendoredOrGenerated` skips `magefiles/` — correct because both targets operate from repo root and paths are relative.

### O2 — Pre-commit hook only lints, doesn't format

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Pattern Consistency
- **Location**: `scripts/pre-commit:5`
- **Detail**: The hook runs `mage lint` but not `mage formatChanged`. A format-first-then-lint workflow is more common.

### O3 — FormatChanged fails in empty repos

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Safety & Quality
- **Location**: `magefiles/mage.go:152`
- **Detail**: `git diff --name-only HEAD` errors in a repo with no commits. Could fall back to the empty tree hash.

### O4 — InstallHooks copies instead of symlinking

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Pattern Consistency
- **Location**: `magefiles/mage.go:93-97`
- **Detail**: Copy means hook silently diverges if `scripts/pre-commit` is later updated. Symlink or doc reminder needed.
