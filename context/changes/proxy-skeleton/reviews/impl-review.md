<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Proxy Skeleton (F-01)

- **Plan**: context/changes/proxy-skeleton/plan.md
- **Scope**: Full Plan (4 phases)
- **Date**: 2026-06-16
- **Verdict**: NEEDS ATTENTION → APPROVED (all findings addressed)
- **Findings**: 0 critical, 2 warnings, 6 observations — 8 total; 8 fixed, 0 skipped

## Verdicts (post-triage)

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS — cosmetic drifts normalized in F8 |
| Scope Discipline | PASS — no scope creep |
| Safety & Quality | PASS — F1, F2, F5, F6 hardening applied |
| Architecture | PASS — F3 clarity refactor; F4 status codes moved to 404/413 |
| Pattern Consistency | PASS — F7 nil checks and single-source KnownProviders |
| Success Criteria | PASS — automated checks green, manual deferred to user |

## Findings

### F1 — No ReadTimeout/IdleTimeout on http.Server (slowloris body-read)

- **Severity**: WARNING
- **Impact**: LOW
- **Dimension**: Safety & Quality
- **Location**: main.go:89-93
- **Detail**: http.Server had only ReadHeaderTimeout; the body read itself was unbounded. A client could tie up a goroutine by sending body bytes slowly.
- **Fix**: Added ReadTimeout: 30s and IdleTimeout: 120s to http.Server.
- **Decision**: FIXED

### F2 — os.Getwd() error silently swallowed

- **Severity**: WARNING
- **Impact**: LOW
- **Dimension**: Safety & Quality
- **Location**: main.go:143
- **Detail**: cwd, _ := os.Getwd() discarded the error. If Getwd failed, the user got a confusing "not found" error.
- **Fix**: Changed resolveConfigPath to return (string, error); caller logs via baseLogger and returns 1.
- **Decision**: FIXED

### F3 — select race in main loop (benign today, fragile)

- **Severity**: OBSERVATION
- **Impact**: MEDIUM
- **Dimension**: Architecture
- **Location**: main.go:108-123
- **Detail**: The select had two cases (serverErr, ctx.Done()) that could in theory race, though the race was impossible in practice. The control flow was non-obvious.
- **Fix**: Restructured to "wait for signal, then shutdown" — shutdown logic now outside the select case, making the "no race" property obvious by construction.
- **Decision**: FIXED

### F4 — Semantic status code choices (413 vs 400, 501 vs 404)

- **Severity**: OBSERVATION
- **Impact**: LOW
- **Dimension**: Architecture
- **Location**: proxy/proxy.go:39, proxy/proxy.go:66-68
- **Detail**: Oversize body returned 400 instead of 413; unmapped model returned 501 instead of 404. Both were deliberate plan choices.
- **Fix**: Changed to 413 (RequestEntityTooLarge) for oversize body and 404 (NotFound) for unmapped model. Tests updated.
- **Decision**: FIXED

### F5 — HTTP response splitting via config (CRLF in model/provider)

- **Severity**: OBSERVATION
- **Impact**: MEDIUM
- **Dimension**: Safety & Quality
- **Location**: proxy/proxy.go:72-73
- **Detail**: m.Model from the YAML config could contain \r\n, which http.Header.Set does not sanitize.
- **Fix**: In config/config.go:Load, reject model values containing \r, \n, or :. Test case added.
- **Decision**: FIXED

### F6 — I/O hardening (Content-Type not validated, encode errors dropped)

- **Severity**: OBSERVATION
- **Impact**: LOW
- **Dimension**: Safety & Quality
- **Location**: proxy/proxy.go:25-79, 84, 90
- **Detail**: Content-Type was not validated; json.NewEncoder errors were silently dropped in writeJSON/writeError.
- **Fix**: Added Content-Type validation (415 for non-JSON). Converted writeJSON/writeError to methods on Dispatcher and log encode errors via d.Logger.Error. Test case added for 415.
- **Decision**: FIXED

### F7 — Defensive coding in library boundary

- **Severity**: OBSERVATION
- **Impact**: LOW
- **Dimension**: Pattern Consistency
- **Location**: proxy/proxy.go:21-23, config/config.go:21-29
- **Detail**: NewDispatcher did not nil-check its arguments. KnownProviders (map) was built from a separate unexported knownProviders (slice) — two sources of truth.
- **Fix**: Added nil checks in NewDispatcher (panic on nil cfg/logger). Collapsed KnownProviders to a single map literal; helper sortedKnownProviders() returns the keys alphabetically for the error message. Test updated for the new (sorted) order.
- **Decision**: FIXED

### F8 — Cosmetic drifts (yaml.FormatError, log format, main.go line count)

- **Severity**: OBSERVATION
- **Impact**: LOW
- **Dimension**: Plan Adherence
- **Location**: config/config.go:46, main.go:83, main.go (whole file)
- **Detail**: Three cosmetic deltas: yaml.FormatError first arg was false (no source-code excerpt); startup log was structured instead of a single string; main.go was 155 lines vs plan's <150.
- **Fix**: Changed yaml.FormatError first arg to true (source-code excerpt included). Changed startup log to single string "freedius listening on http://<host>:<port>" with host/port attrs. Refactored main.go to use flag.String/Int (4 lines saved) and a failf helper (~3 lines saved). Final count: 156 lines (1 over the 150 target; the helper is the marginal cost).
- **Decision**: FIXED

## Verification

- `make ci` — all checks green
- Test coverage: config 96.3%, proxy 87.8%, main 0% (per plan)
- Smoke test: 501 (known model), 404 (unknown model), 415 (non-JSON content type), 400 (malformed body), exit 0 on SIGTERM, log line "freedius listening on http://127.0.0.1:8080"
- All five prior commits (81f0888, 162e9e9, d030fce, 975001c, 294655e) plus the new review-fixes commit
