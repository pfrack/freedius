# Downgrade checkRequiredEnvVars to Warning — Plan Brief

> Full plan: `context/changes/lazy-api-key-check/plan.md`
> Research: `context/changes/lazy-api-key-check/research.md`

## What & Why

Downgrade the startup `checkRequiredEnvVars` function from a fatal error to a structured warning log. Currently, freedius refuses to start if any mapped provider's API key env var is missing — even though the adapters already handle this gracefully at request time and the web dashboard shows "Key Missing" badges. This blocks users from exploring the web UI and configuring providers interactively.

## Starting Point

`checkRequiredEnvVars` in `cmd/freedius/main.go:397-411` iterates providers referenced by mappings and fatally exits on the first missing env var. The function was already narrowed from checking ALL providers (lessons.md documents this evolution). Both runtime adapters independently check keys at request time and return typed errors that enable fallback.

## Desired End State

freedius starts regardless of missing API keys. Structured warnings appear in the startup log for each missing key. Web dashboard badges and request-time errors continue to work unchanged. Users can explore the UI, add mappings, and set keys without restart-restart-restart cycles.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|----------|--------|-------------------|--------|
| Approach | Downgrade to warning (keep function) | Preserves discoverability without blocking startup. | Plan |
| Log granularity | One `slog.Warn` per missing key | Structured logging — each is independently filterable, matches codebase slog convention. | Plan |
| Test strategy | Inject logger, assert warning attributes | Proves warnings fire correctly; keeps regression safety. | Plan |
| Iteration behavior | Log all missing keys (no early return) | Eliminates "fix one, restart, find next" cycle. | Plan |

## Scope

**In scope:**
- Change function signature to accept `*slog.Logger`
- Replace error return with warning emissions
- Remove early return (log all missing keys)
- Adapt 3 existing tests

**Out of scope:**
- Runtime adapter behavior (already correct)
- Web UI changes (already real-time)
- Env var deduplication
- "Set API key" web dashboard feature

## Architecture / Approach

Single function change: `checkRequiredEnvVars(cfg) error` → `checkRequiredEnvVars(logger, cfg)`. The call site at `main.go:141` drops the error check. Tests inject a `slog.JSONHandler` writing to a buffer and parse the output to assert warnings. No new packages, no new files, no architectural changes.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Downgrade checkRequiredEnvVars | Non-blocking startup with structured warnings | None — adapters already handle missing keys at runtime |

**Prerequisites:** None — this is a standalone change with no dependencies.
**Estimated effort:** ~1 session, single phase.

## Open Risks & Assumptions

- Assumption: no downstream code checks the return value of `checkRequiredEnvVars` beyond the one call site at `main.go:141` (verified — it's only called once).
- Risk: Users accustomed to the startup error may not notice the warning in logs. Mitigated by the web UI's "Key Missing" badge.

## Success Criteria (Summary)

- freedius starts successfully with missing API keys
- Structured warnings appear in startup log for each missing key
- Existing runtime error handling and web UI badges remain unchanged
