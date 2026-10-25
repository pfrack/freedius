# Follow-ups from Implementation Review

## 1. Commit working-tree fixes for CSRF bypass (F1)

**Status**: Applied in working tree, NOT committed.

The committed HEAD (`0cb54d7`) has a **CSRF bypass** in `NewServer`
(`proxy/web/server.go`): `csrfGuard` wraps the mux *before* `StripPrefix`, so
it sees the unstripped path (`/freedius/v1/...`) and `HasPrefix(path, "/v1/")`
returns false → cross-site mutations bypass CSRF.

The working tree corrects the ordering to `Auth → StripPrefix → csrfGuard → mux`
and adds `csrfGuard` to the `newTestMuxBase` test helper.

**Action**: `git add proxy/web/server.go proxy/web/handlers_test.go && git commit`
with a message like `fix(web): place csrfGuard inside StripPrefix to prevent CSRF bypass behind base path`.

## 2. F2 fix already applied

`TestCSRFGuardWorksWithBasePath`'s read-only case was changed from
`/freedius/v1/events` (SSE, blocking) to `/freedius/v1/stats` (non-blocking).
Verified: 802 tests pass with `-race`.

## 3. Plan addenda (F3, F4)

Both unplanned template changes (`404.html`, `index.html` alert links) are now
documented as Phase 2 addenda in `plan.md`.
