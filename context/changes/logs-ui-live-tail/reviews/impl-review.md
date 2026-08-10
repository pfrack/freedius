<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Logs UI Live-Tail Restoration

- **Plan**: context/changes/logs-ui-live-tail/plan.md
- **Scope**: Full plan (all 3 phases)
- **Date**: 2026-08-09
- **Verdict**: REJECTED (updated 2026-08-09 during triage — F5 raised)
- **Findings**: 1 critical · 1 warning · 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | FAIL |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | FAIL |

## Findings

### F5 — Live tail still broken: `#log` has `sse-connect` but no `sse-swap`/`hx-trigger`

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence / Success Criteria
- **Location**: proxy/web/templates/logs.html:91-96
- **Detail**: The change's whole objective — restore the live SSE tail — is NOT met. `#log` carries only `hx-ext="sse"` + `sse-connect="/v1/logs"`. The vendored `htmx-ext-sse@2.2.2` registers per-event EventSource listeners only for elements that also carry `sse-swap` (or an `hx-trigger` of the form `sse:<event>`); with neither present, no listener for the `log` event is ever attached. The EventSource *does* open, so `htmx:sseOpen` fires and the dot flips to `live` — which is exactly why test #2 passes while the tail stays dead. The `htmx:sseMessage` handler at logs.html:155 is therefore never invoked and no streamed line is appended.
  Proof: (a) `curl -N http://127.0.0.1:8083/v1/logs` streams the post-load line correctly (`event: log` / `data: {"level":"info","line":"...request complete...path=/__probe_check..."}`) — server side is healthy; (b) the browser's `#log` child count stays at 3 (the server-rendered snapshot) for the full 15s poll; (c) the extension source shows listener registration gated on `sse-swap` / `hx-trigger`.
  This also invalidates the plan's Phase 1/2 manual criteria that were checked off, and means test #2 is a false-positive guard.
- **Fix A ⭐ Recommended**: Add `sse-swap="log"` to `#log` and let the existing `htmx:sseBeforeMessage` / `htmx:sseMessage` handler do the rendering, suppressing the extension's default swap (e.g. return `false` from a `htmx:sseBeforeMessage` listener, or point `hx-swap="none"` at the element) so the custom JSON-parsing renderer at logs.html:155 remains the single append path.
  - Strength: Smallest change that makes the extension register a `log` listener; keeps the existing level-classing/`isSystemLog`/`appendLogLine` logic and the pause-on-scroll behavior intact.
  - Tradeoff: Must explicitly neutralize the extension's built-in swap, or raw JSON will be injected into `#log` alongside the parsed line.
  - Confidence: HIGH — registration path confirmed by reading the vendored extension.
  - Blind spot: Interaction between `hx-swap="none"` and the extension's `getSwapSpecification` path not yet exercised in a browser.
- **Fix B**: Drop the htmx SSE extension for this page and open a plain `new EventSource('/v1/logs')` in logs.html's script, wiring `addEventListener('log', ...)` to the existing renderer and `onopen`/`onerror` to `setLiveState`.
  - Strength: Removes an indirection that has now silently failed twice; ~10 lines, no vendored dependency, trivially testable.
  - Tradeoff: Abandons the plan's stated htmx-extension approach and leaves `htmx-sse.min.js` vendored-but-unused (or needing removal); loses the extension's automatic reconnect backoff unless reimplemented.
  - Confidence: HIGH — the page already hand-rolls all rendering; the extension only supplies connect+reconnect.
  - Blind spot: Whether other pages (e.g. `/v1/events` consumers) rely on the vendored extension.
- **Decision**: FIXED via Fix A. Two edits to `proxy/web/templates/logs.html`: (1) added `sse-swap="log"` + `hx-swap="none"` to `#log` so the extension registers a `log` listener while its built-in swap stays inert; (2) fixed the message guard, which rejected every event by testing `e.detail.eventName` — the extension forwards the raw `MessageEvent`, whose SSE event name is `detail.type`. The guard is now `detail.type || detail.eventName` (back-compatible). Verified: `npx playwright test tests/logs-tail.spec.ts` → 2 passed (test #1 in 515ms, previously timing out at 15s), `go build ./...` clean, `go test ./proxy/web/...` ok. Note the second layer was invisible until the first was fixed — the guard bug would have kept the tail dead even under Fix B.

### F1 — e2e test #1 does not guard the SSE fix

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: e2e/tests/logs-tail.spec.ts:10-12
- **Detail**: Test #1 (`streamed log lines appear in #log over SSE on load`) asserts a `<pre class="log-*">` element is attached with non-empty text within 15s. But `proxy/web/handlers.go:473` renders the server-side ring-buffer snapshot into `<pre class="log-*">` elements on the *initial* page load (confirmed at handlers.go:440-479, and the `{{range .Entries}}` block at logs.html:100). On a fresh server, startup logs ("listening", "loaded config") already populate the ring buffer, so the initial HTML already contains `<pre class="log-*">` lines. The assertion passes **even if `hx-ext="sse"` is never registered** (i.e. the very bug this plan fixes). Only test #2 (`.log-live-dot` reaching `data-state="live"`) actually catches a missing extension, because `htmx:sseOpen` never fires without it.
- **Fix**: Make test #1 assert a line that arrives *after* load — e.g. trigger new proxy activity (the Playwright harness can hit an endpoint or send a request) and assert the `#log` child count increases, or wait for an `htmx:sseMessage`-derived node distinct from the server-rendered snapshot.
  - Strength: Turns the regression guard from cosmetic into real; matches the plan's stated intent ("prove the tail streams").
  - Tradeoff: Slightly more involved setup (needs a way to generate a post-load log line in the test).
  - Confidence: HIGH — the initial-render path is confirmed in handlers.go.
  - Blind spot: None significant.
- **Decision**: FIXED (applied in working tree, uncommitted; +23/-4 vs HEAD). Verified by running the suite: the strengthened assertion now FAILS, correctly exposing F5 — the fix works as a regression guard. It will go green only once F5 is resolved.

### F2 — `mage test` gates cannot be re-verified green in the shared tree

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: (environmental) repo working tree
- **Detail**: The plan's automated criterion 3.3 (`mage test` passes) was green earlier this session, but `go test -race ./...` is currently RED with 3 failures: `config_test.go:568` (providerDefaults 18 vs want 16), `genproviders/main_test.go:305` (18 vs 16), `main_test.go:507` (valid-config expects 16). These are caused by the concurrently-running `nim-nous-kilo-defaults` change, which added 2 providers to `providers.yaml` without updating its own hardcoded counts. None originate from logs-ui (which touches zero Go files). The same contamination trips the pre-commit `generateCheck` gate (dirty `internal/envinject/snippet.go`), which is why p3 could not be committed as its own commit.
- **Fix**: Finish/clean the `nim-nous-kilo-defaults` change (or revert it) so the tree is green and logs-ui-only, then re-run `mage test` and the dedicated p3 commit.
  - Strength: A clean tree is a prerequisite for any correct CI signal here.
  - Tradeoff: Requires coordinating with the other in-flight change.
  - Confidence: HIGH — failures are provably unrelated to logs-ui diff.
  - Blind spot: Haven't confirmed whether nim-nous is still actively being implemented.
- **Decision**: ACCEPTED — out of scope for this change. Re-verified during triage (2026-08-09): the three provider-count failures (`config_test.go:568`, `genproviders/main_test.go:305`, `main_test.go:507` — "18 vs 16") are now GONE. One contaminating failure remains, still owned by `nim-nous-kilo-defaults`: `TestStarterTemplate_ValidConfig` — `mapping "opus" fallback[2] has unsafe "model_string" value (must not contain CR, LF, or colon)`, caused by the `hy3:free` model string in the uncommitted `starter.yaml` hitting the config validator's colon ban. logs-ui touches zero Go files, so its own gates remain uncertifiable in this shared tree; `go build ./...` and `go test ./proxy/web/...` were confirmed green in isolation. Resolution belongs to the nim-nous change.

### F3 — p3 artifacts co-mingled into a mislabeled commit

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria (process)
- **Location**: commit e715c54 (message: `feat(nim-nous-kilo-defaults): add nous and kilo provider metadata (p1)`)
- **Detail**: The p3 deliverables (`e2e/tests/logs-tail.spec.ts` + the `plan.md` 3.x `[x]` flips) were absorbed into commit `e715c54`, whose message belongs to the parallel `nim-nous-kilo-defaults` change (tree contamination from running both changes in one session). Consequently p3 has no logs-ui-labeled commit, and `plan.md` Progress rows for Phase 3 carry no SHA suffix. The plan's "each phase has its own Conventional-Commits commit" expectation is not met for p3.
- **Fix**: After the tree is clean (see F2), author a dedicated `feat(logs-ui-live-tail): add logs live-tail e2e + gates (p3)` commit containing `e2e/tests/logs-tail.spec.ts` + the `plan.md` SHA write-back, and run the epilogue (`change.md` → `implemented`).
  - Strength: Restores the per-phase commit contract and a clean `git log`.
  - Tradeoff: Requires untangling e715c54 (the content is already committed, just mislabeled).
  - Confidence: MED — depends on whether e715c54 is later amended/split by the nim-nous work.
  - Blind spot: e715c54 may be rewritten by the nim-nous change's own history cleanup.
- **Decision**: DEFERRED — queued in `context/changes/logs-ui-live-tail/follow-ups/review-fixes.md` (item 1). No git operations performed: the tree still carries the nim-nous `starter.yaml` blocker (F2), which trips the pre-commit gate. Scope has grown since the report was written — the F1 and F5 fixes added new uncommitted logs-ui work (`logs.html`, `logs-tail.spec.ts`), so the deferred commit now covers those too.

### F4 — Vendored `sse.js` instead of planned `sse.min.js`

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/htmx-sse.min.js
- **Detail**: The plan (Phase 1.1) specifies sourcing `https://unpkg.com/htmx-ext-sse@2.2.2/sse.min.js`, but that URL 404s; the vendored file was fetched from `https://unpkg.com/htmx-ext-sse@2.2.2/sse.js` (HTTP 200) and saved under the plan's local filename `htmx-sse.min.js`. The content is the genuine `htmx-ext-sse@2.2.2` extension and defines `htmx.defineExtension('sse')`, satisfying the contract. Benign deviation, already documented in the goal run-report.
- **Fix**: None required. Optionally update the plan's Phase 1.1 source URL to `sse.js` so the source of truth matches reality.
  - Strength: Keeps plan/implementation in sync for future reviews.
  - Tradeoff: Trivial.
  - Confidence: HIGH.
  - Blind spot: None.
- **Decision**: FIXED. `plan.md` Phase 1.1 now cites `https://unpkg.com/htmx-ext-sse@2.2.2/sse.js` with a note that the `sse.min.js` path 404s and the local filename stays `htmx-sse.min.js`; the Progress row 1.1 wording was corrected to match.
