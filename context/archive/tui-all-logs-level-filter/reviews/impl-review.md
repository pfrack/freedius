<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: TUI Log Tab — all slog lines + level-cycle filter

- **Plan**: context/changes/tui-all-logs-level-filter/plan.md
- **Scope**: All 3 phases
- **Date**: 2026-06-20
- **Verdict**: NEEDS ATTENTION
- **Findings**: 1 critical, 3 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Re-entrant slog.Warn inside ringHandler.Handle

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/logtee.go:117
- **Detail**: ringHandler.Handle calls slog.Warn() on overflow, which re-enters ringHandler.Handle because the default logger uses this handler. Recursion terminates at depth 2 via overflow flag guard, but fragile.
- **Fix**: Replace slog.Warn with direct os.Stderr.WriteString to bypass the slog system entirely.
  - Strength: Eliminates fragile recursion; warning becomes visible
  - Tradeoff: Minor — bypasses slog formatting for one line
  - Confidence: HIGH
  - Blind spot: None significant
- **Decision**: PENDING

### F2 — Log scroll resets on every request event

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix obvious and scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/model.go:234
- **Detail**: case requestEventMsg sets d.logScroll = 0, fighting manual scrolling since events and logs are separate data sources.
- **Fix**: Remove d.logScroll = 0 from case requestEventMsg.
- **Decision**: PENDING

### F3 — No nil-guards on LogSink methods

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix obvious and scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/logtee.go:22-26
- **Detail**: EventBus has nil-guards; LogSink doesn't.
- **Fix**: Add nil-guards to Subscribe(), Snapshot(), EventCount().
- **Decision**: PENDING

### F4 — formatH LevelInfo filters Debug, producing empty lines in TUI

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/logtee.go:73
- **Detail**: NewRingHandler creates formatH with LevelInfo. Debug records produce empty Line in LogEntry because TextHandler skips below threshold.
- **Fix**: Use HandlerOptions{Level: LevelDebug} so formatH renders all records.
- **Decision**: PENDING

### F5 — Snapshot allocates cap(ch) rather than len(ch)

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Performance
- **Location**: proxy/logtee.go:42
- **Detail**: make([]LogEntry, 0, cap(s.ch)) always allocates for cap even when channel is nearly empty.
- **Fix**: Use len(s.ch) for pre-allocation hint.
- **Decision**: PENDING

### F6 — renderLogTab scroll offset doesn't account for filter

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW
- **Dimension**: Reliability
- **Location**: proxy/tui/views.go:42-58
- **Detail**: Scroll offset counts raw entries, filter silently skips during render, making scroll feel janky.
- **Fix**: Filter entries before computing the visible window.
- **Decision**: PENDING
