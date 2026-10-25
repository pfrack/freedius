<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Fix Relative Dashboard Links

- **Plan**: context/changes/fix-relative-dashboard-links/plan.md
- **Scope**: Full plan (4 phases)
- **Date**: 2026-10-25
- **Verdict**: APPROVED (all findings fixed during triage; see notes)
- **Findings**: 2 critical, 0 warnings, 2 observations

> **Note**: F1 and F2 fixes are applied in the working tree but remain
> **uncommitted** (`git diff HEAD` shows changes to `proxy/web/server.go` and
> `proxy/web/handlers_test.go`). The CSRF bypass exists in the committed HEAD;
> commit the working-tree changes before merging.

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | WARNING |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

  ► Overall: APPROVED

## Findings

### F1 — CSRF bypass: StripPrefix sits outside csrfGuard (server.go)

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/server.go:47-53 (committed HEAD)

**Detail**:
The committed `NewServer` wraps the mux with `csrfGuard` **last** (outermost),
so the guard sees the *unstripped* path:

```go
handler := http.Handler(mux)
if h.BasePath != "" {
    handler = http.StripPrefix(h.BasePath, mux)   // wraps mux, not handler
}
if h.AuthToken != "" {
    handler = h.RequireAuth(handler)
}
handler = csrfGuard(handler)                        // outermost
```

Layering (outer→inner): `csrfGuard → Auth → StripPrefix → mux`.

With `--ui-path /freedius`, a cross-site `POST /freedius/v1/providers` reaches
`csrfGuard` with `r.URL.Path == "/freedius/v1/providers"`. The guard's check
`strings.HasPrefix(r.URL.Path, "/v1/")` (handlers.go:80) is **false**, so the
mutation is **not blocked** — CSRF is bypassed for every prefixed `/v1/` route.
This is a security regression introduced by this change (before it, no prefix
meant `/v1/...` always matched).

The plan's "StripPrefix ordering" detail prescribed exactly this vulnerable
ordering ("StripPrefix must sit INSIDE the CSRF guard"), so the plan itself was
flawed and the code followed it.

The working tree contains an **uncommitted fix** that is correct:

```go
handler := csrfGuard(http.Handler(mux))   // csrfGuard(mux) first
if h.BasePath != "" {
    handler = http.StripPrefix(h.BasePath, handler)   // StripPrefix wraps it
}
if h.AuthToken != "" {
    handler = h.RequireAuth(handler)
}
```

Layering: `Auth → StripPrefix → csrfGuard → mux`. Now csrfGuard sees the stripped
path and blocks prefixed mutations. The committed `newTestMuxBase` also omitted
`csrfGuard` entirely (fixed in the working tree).

**Fix**: Commit the working-tree `server.go` ordering and the `newTestMuxBase`
csrfGuard addition; then re-run the suite (F2 must be fixed first or the suite
still fails).

  - Strength: Mirrors the production middleware layering exactly; closes the
    bypass without changing route patterns.
  - Tradeoff: None functionally — one ordering change; requires a commit.
  - Confidence: HIGH — the working-tree test helper already demonstrates the
    correct ordering (cross-site POST/DELETE now return 403).
  - Blind spot: Haven't run the full server behind a real reverse proxy; the
    httptest evidence is strong but not end-to-end.

- **Decision**: FIXED (code already applied in working tree; commit still pending)

### F2 — TestCSRFGuardWorksWithBasePath hangs on the SSE endpoint

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/web/handlers_test.go:307 (working tree)

**Detail**:
The read-only case sends `GET /freedius/v1/events`, which routes to
`handleEvents` (internal/eventstream/handlers.go:89). That handler writes the
SSE headers then enters an infinite loop:

```go
for {
    select {
    case e := <-ch: ...
    case <-r.Context().Done(): return
    }
}
```

`httptest.NewRecorder()` + `httptest.NewRequest()` never cancel the request
context, so the goroutine blocks forever. The Go test binary hits its timeout
(`panic: test timed out`) and `mage test` fails. This is a test-design bug, not
a product bug — but it blocks the automated success criterion.

**Fix**: Point the read-only case at a non-blocking GET endpoint, e.g.
`/freedius/v1/stats` (internal/eventstream/handlers.go:45), or `/freedius/health`.

  - Strength: Keeps the "GET is not blocked" assertion while avoiding the
    long-lived SSE handler; `/v1/stats` is a real read-only JSON endpoint.
  - Tradeoff: Loses coverage of the SSE route specifically — but that route is
    already covered by `TestHandlers_Register_AllRoutes` and SSE tests.
  - Confidence: HIGH — `/v1/stats` returns immediately.
  - Blind spot: None significant.

- **Decision**: FIXED (read-only case changed to /freedius/v1/stats; test passes)

### F3 — 404.html template changes not in plan

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: proxy/web/templates/404.html:9,15-17

**Detail**:
`404.html` was changed to use `{{baseURL ...}}` for all root-relative links, but
it was not listed in the plan's Phase 2 "Changes Required". The change is
functionally necessary (otherwise the branded 404 page would have broken links
behind a prefix) but is an unplanned addition.

**Fix**: Document as an addendum to the plan's Phase 2 scope.

  - Strength: Preserves correct behavior; updates the source of truth.
  - Tradeoff: Plan becomes a slightly moving target.
  - Confidence: HIGH — addendum pattern used in this repo's planning.
  - Blind spot: Stakeholders who reviewed the original scope aren't notified.

- **Decision**: FIXED (addendum added to plan.md Phase 2)

### F4 — index.html alert links prefixed beyond plan scope

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: proxy/web/templates/index.html:50

**Detail**:
`{{.Link}}` in the alerts section was wrapped as `{{baseURL .Link}}`, which was
not explicitly listed in Phase 2's contract (which listed lines 67, 86, 123, 140
only). The change is functionally correct and necessary — alert links need the
prefix too — but it is an unplanned addition.

**Fix**: Document as an addendum to Phase 2, or accept as implicit coverage of
the "prefix all template links" intent.

  - Strength: Captures correct scope; aligns plan with implementation.
  - Tradeoff: Minor — one line to the plan.
  - Confidence: HIGH.
  - Blind spot: None significant.

- **Decision**: FIXED (addendum added to plan.md Phase 2)

═══════════════════════════════════════════════════════════
  TRIAGE COMPLETE
═══════════════════════════════════════════════════════════

  Fixed:     F2, F4                    (2)
  Fixed*:    F1 (working tree; commit pending) (1)
  Documented: F3, F4 (plan addenda)    (2)
  Skipped:   —
  Accepted:  —

  * F1's code fix already exists in the working tree (uncommitted); the
    plan's "StripPrefix ordering" detail was itself flawed.

  Final verification: go test ./... -race -count=1 → 802 passed, 0 failed.
  go vet ./proxy/web/... ./internal/eventstream/... ./cmd/... → clean.
  gofmt -l: clean (handlers_test.go reformatted).

═══════════════════════════════════════════════════════════
