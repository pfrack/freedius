<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider Badge Drawer (+ Logs Filter Fix)

- **Plan**: `context/changes/provider-badge-drawer/plan.md`
- **Scope**: Full plan — Phases 1-4 (all automated Progress boxes `[x]`; all 4 manual boxes pending)
- **Commits**: `b7de758..37985bc` (5 commits, 11 files, +955/-11)
- **Date**: 2026-08-10
- **Verdict**: NEEDS ATTENTION → all 8 findings triaged and FIXED (see Triage Outcome)
- **Findings**: 0 critical, 6 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | WARNING |

### Automated criteria — verified by this review

| Command | Result |
|---|---|
| `mage test` (race) | PASS — 8 packages ok, `proxy/web` 70.1% coverage |
| `mage lint` | PASS — 0 issues |
| `mage build` | PASS — binary produced |

All 14 automated Progress boxes are legitimately `[x]`. All 4 manual boxes (1.5, 2.6, 3.2, 4.5) remain `[ ]` — correctly pending, no rubber-stamping detected.

### Scope guardrails — 4/4 respected

- SSE endpoint untouched: `git diff b7de758^..HEAD -- internal/eventstream/` is empty.
- No live/SSE stats in the drawer — static one-shot fragment, no `sse-*`/`hx-trigger` attrs.
- No models / mapping count in the drawer — only Status, Protocol, Base URL, API Key Env, edit link.
- `/providers` `<details>` view untouched — `providers.html` not in the diff.

### Security — specifically cleared

- `loadFragmentTemplate` uses **`html/template`** (`proxy/web/embed.go:10`), so all provider-derived interpolations are contextually auto-escaped. No `template.HTML`/`template.URL` conversions in new code.
- `EditLink` correctly double-layered: `url.QueryEscape` (handlers.go:1025) then `html/template` `urlFilter`/`urlNormalizer` in the `href` context.
- API key **value** never leaves the handler — only the `EnvPresent` bool reaches the struct, template, or logs (handlers.go:1011-1015).
- No new `innerHTML` sink — `logs.html` still renders via `pre.textContent` only.
- No double-`WriteHeader`: every error path returns before `w.Header().Set` at handlers.go:1034, and the post-`ExecuteTemplate` error is logged only. Correctly follows the *Adapter Return Contract* lesson in `context/foundation/lessons.md`.
- Unauthenticated `/v1/` read surface is by design (local-only dashboard); new route matches sibling posture. Not flagged.

## Findings

### F1 — "Edit on Providers page" link does not pre-filter the providers table

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go:1025 (link built) → proxy/web/handlers.go:512 (`handleProviders`)
- **Detail**: The drawer emits `EditLink = "/providers?provider=" + url.QueryEscape(name)`, matching the plan's *string* contract. But `handleProviders` never reads `r.URL.Query()` — verified: the only query consumers in the file are `handleLogs` (:404) and the two mappings handlers (:615, :871). No client-side handling either (`grep URLSearchParams|location.search` across `app.js` and all templates returns nothing). The param is inert. Plan Phase 2 manual criterion 2.6 explicitly requires *"links to `/providers?provider=<name>` **with the table pre-filtered**"* and the Desired End State promises the same. Mitigating: box 2.6 is still `[ ]`, so this is honestly pending rather than falsely claimed.
- **Fix A ⭐ Recommended**: Read `?provider=` in `handleProviders` and filter `rows` to the matching name (or highlight it), mirroring how `handleLogs` reads its query params at handlers.go:404.
  - Strength: Delivers the end state the plan and frame brief promised; the pattern already exists one function away in the same file.
  - Tradeoff: Adds a small feature to a phase already marked implemented; needs its own test.
  - Confidence: HIGH — `handleLogs` is a direct in-repo template for query-param filtering.
  - Blind spot: Haven't decided whether "pre-filtered" should mean filter-to-one or scroll/highlight; the plan doesn't specify.
- **Fix B**: Drop the pre-filter promise — amend manual criterion 2.6 to "links to the providers page" and note the param as reserved.
  - Strength: Zero code change; the link is still a useful navigation affordance.
  - Tradeoff: Ships a URL param that does nothing, which future readers will mistake for working behavior.
  - Confidence: MEDIUM — depends whether pre-filtering was load-bearing for the operator workflow in `frame.md`.
  - Blind spot: Haven't checked whether the providers table is long enough for pre-filtering to matter in practice.
- **Decision**: FIXED via Fix A — `handleProviders` now reads `?provider=` and filters rows (case-insensitive substring, mirroring `handleLogs`). handlers.go:511-517, :529-531.

### F2 — `handleProviderDetail` omits the `h.Stats != nil` guard every sibling has

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency (also Reliability)
- **Location**: proxy/web/handlers.go:1008
- **Detail**: `statusSlug := deriveProviderStatus(h.Stats.ProviderSnapshot()[name])` dereferences `h.Stats` unguarded. Every sibling guards it — `handleMappingDetail` (:951), the dashboard (:183), `handleProviders` (:524-526), and :805 all wrap the call in `if h.Stats != nil`. This only works today because `(*StatsCollector).ProviderSnapshot` carries an internal `if sc == nil { return nil }` (proxy/stats_collector.go:234-236). The new tests construct `eventstream.Handlers` **without** `Stats` (handlers_provider_detail_test.go:34-39), so they pass purely on that nil-receiver accident. Remove that tolerance and this handler panics on every request.
- **Fix**: Mirror the sibling contract explicitly — `var ps proxy.ProviderStats; if h.Stats != nil { ps = h.Stats.ProviderSnapshot()[name] }; statusSlug := deriveProviderStatus(ps)`.
- **Decision**: FIXED — explicit `if h.Stats != nil` guard added, matching siblings. handlers.go:1012-1016.

### F3 — SSE filter can throw uncaught from the `catch` branch and misclassify parsed lines

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality (Reliability)
- **Location**: proxy/web/templates/logs.html:173-177 (unguarded reads), :205 (call inside `try`), :214 (call inside `catch`)
- **Detail**: `logLinePassesFilters` performs five unguarded DOM reads, e.g. `document.getElementById('provider-filter').value`. It is invoked both inside the `try` (:205) and inside the `catch` (:214). Two consequences: (a) a `TypeError` at :205 is swallowed by the `catch`, which then **misclassifies a successfully parsed log entry** as an unparseable raw payload; (b) the same throw at :214 is outside any `try` and escapes as an uncaught exception in the SSE listener — a regression from the previous code, where the catch branch could not throw. Not reachable today (all five ids are always present on `/logs`; the filter form targets `#log` and is never itself swapped), but the structure is fragile.
- **Fix**: Guard the reads (`var el = document.getElementById(id); return el ? el.value : ''`) and hoist the single filter check above the `try/catch` so it runs exactly once per line.
- **Decision**: FIXED — added `filterValue(id)` helper with a null guard; restructured the SSE listener so only `JSON.parse` is inside the `try` and the filter check runs exactly once, outside it. logs.html:172-175, :205-222.

### F4 — 404 test's negative assertion is tautological

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/web/handlers_provider_detail_test.go:148
- **Detail**: `if strings.Contains(body, "provider-drawer")` is meant to assert the unknown-provider path renders no drawer fragment. But `provider-drawer` only ever appears as the `{{define}}` name — it is never emitted into rendered output. The success test correctly asserts on `drawer__content` (:55) instead. This check can never fail, so it provides false safety: a handler bug that rendered the drawer on a 404 would slip through.
- **Fix**: Assert absence of `drawer__content` (the string the success case actually checks for).
- **Decision**: FIXED — negative assertion now checks absence of `drawer__content`. handlers_provider_detail_test.go:151.

### F5 — The Phase 3 fix (client-side JS filter) has zero test coverage

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: proxy/web/handlers_logs_filter_test.go:27-30
- **Detail**: The file's docstring claims it guards the SSE regression — *"guarding against the regression the SSE live tail used to leak."* But it exercises `handleLogs`, the **server-side** predicate (handlers.go:422-453), which never had the bug. The actual fix in `158ddb8` is entirely client-side (`logLinePassesFilters`, logs.html:159-188), and the diff adds no `e2e/` test. So the new code is untested and the two implementations can drift silently — they already diverge on invalid `min` values (server 400s via `parseMinLevel` at :769-788; client defaults unknown labels to rank 1 at logs.html:165). Note the predicates were otherwise verified equivalent across all five dimensions.
- **Fix A ⭐ Recommended**: Add a Playwright case in `e2e/` that sets a filter and asserts a non-matching streamed line is dropped from the live tail.
  - Strength: Covers the actual defect at the layer it was fixed; `e2e/` infrastructure already exists (`mage e2e`).
  - Tradeoff: E2E tests are slower and more brittle than the Go unit tests already in place.
  - Confidence: HIGH — this is exactly the class of browser-level behavior `e2e/` exists for.
  - Blind spot: Haven't checked whether the existing e2e harness can drive the SSE stream deterministically.
- **Fix B**: Correct the docstring to state it pins the server predicate the JS mirrors, and accept the JS as manually verified via box 3.2.
  - Strength: Honest about what the test does; the parity test still has real value as a drift anchor.
  - Tradeoff: Leaves the fixed code permanently unguarded by CI.
  - Confidence: MEDIUM — acceptable only if manual box 3.2 is actually walked before archive.
  - Blind spot: None significant.
- **Decision**: FIXED via Fix A — added `e2e/tests/logs-filter-tail.spec.ts`, which fires a non-matching then a matching probe and asserts only the matching line reaches `#log`. Deliberate-break verified: disabling the client filter makes the test fail (`locator resolved to 1 element - unexpected value "1"`). Docstring in handlers_logs_filter_test.go also corrected to point at the e2e guard.

### F6 — `.provider-badge` CSS not updated after `<a>` → `<button>`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.css:1648-1661 (file has 0 lines in the diff)
- **Detail**: index.html:123 changed the badge from `<a href>` to `<button type="button">`, but the stylesheet was not touched. There is no global `button { font: inherit }` reset (the only `font: inherit` is at :680, scoped to `.form-input`/`.form-select`; `.btn` opts in separately at :568-582). `.provider-badge` sets `font-size` but **not** `font-family`, so badge labels now render in the UA default button font instead of the Geist stack. It also lacks `cursor: pointer` — implicit for `<a href>`, absent for `<button>` — so a clickable element now shows the default arrow. The `text-decoration: none` at :1657 is now dead. Since this change is entirely a UX fix, a visible regression in the changed element matters.
- **Fix**: Add `font-family: inherit;` and `cursor: pointer;` to `.provider-badge`, and drop the dead `text-decoration: none`.
- **Decision**: FIXED — `.provider-badge` gains `font-family: inherit` and `cursor: pointer`; dead `text-decoration: none` removed. app.css:1648-1663.

### F7 — New tests reinvent fixtures, skip the table-driven convention, and leak process env

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/handlers_provider_detail_test.go:88, :34-39/:72-77/:89-94/:111-116/:127-132
- **Detail**: Three related hygiene issues. (a) `os.Unsetenv("NIM_API_KEY_FOR_TEST")` at :88 is redundant — the preceding subtest uses `t.Setenv` (:71), which auto-restores — and unlike `t.Setenv` it registers no cleanup, permanently mutating process env for the rest of the package run and coupling subtests to declaration order. (b) The same five-line `&eventstream.Handlers{...}` literal is duplicated 5× here plus once in `handlers_dashboard_test.go:381-386`, while `newRenderHandlers(cfg)` already exists (handlers_phase1_test.go:22-28, used 19×) and differs only in lacking `LogSink`. (c) AGENTS.md states *"Table-driven tests preferred for handler logic"* and the plan said "table tests", but both new files use sequential `t.Run` bodies / sequential `if`s — the four env-presence subtests are a textbook table over `{name, cfg, envValue, wantSubstring}`.
- **Fix**: Use `t.Setenv(..., "")` instead of `os.Unsetenv`; extend `newRenderHandlers` with a `LogSink`; collapse the env-presence subtests into one table.
- **Decision**: FIXED — `os.Unsetenv` replaced by `t.Setenv(key, "")`; `newRenderHandlers` extended with a `LogSink` and reused in both new tests (removing 6 duplicated fixture literals); env-presence subtests collapsed into a 3-case table.

### F8 — `openDrawer` never closes a sibling drawer; `openDrawerEl()` returns the first match

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/web/static/app.js:39-46, :51-61
- **Detail**: The generalization is otherwise correct — `DRAWER_IDS` + `openDrawerEl()` replaced every hardcoded `#mapping-drawer`, and the focus trap correctly passes the resolved open drawer into `trapDrawerTab(evt, drawer)` (:112) which queries `drawer.querySelectorAll(...)` (:127) with no hardcoded id. The gap: `openDrawer` doesn't clear `.drawer--open` from a sibling, and `openDrawerEl()` uses `querySelector` (first DOM match = `#mapping-drawer`, index.html:177). If both were ever open, Escape/overlay-click would close only the mapping drawer, leaving the provider drawer visible with no overlay and a clobbered `drawerOpener`. Unreachable today: the visible overlay is `inset: 0; pointer-events: auto` at `z-index: 50` (app.css:1834-1851), so badges are unclickable while any drawer is open. Mapping-drawer behavior in isolation is byte-identical.
- **Fix**: Optional hardening — have `openDrawer(drawer)` first clear `.drawer--open` from every other `.drawer`.
- **Decision**: FIXED — `openDrawer` now clears `.drawer--open` and content from any other open drawer before opening. app.js:52-64.

## Notes

- Untracked and never committed: `context/changes/provider-badge-drawer/frame.md` and `plan-brief.md` — the former is cited in plan.md:408 References. Also untracked: `context/domain/`. No code impact, but the plan references a file not in history.
- Accepted, not flagged: `statusSlugToLabel` helper (handlers.go:1042-1053) implements the plan's inline "small map" as a named function; the `name == ""` → 400 `bad_path` guard (:996-999) is unreachable through the route pattern but mirrors `handleMappingDetail`:916-919; the `log-empty` removal was relocated into `appendLogLine` (logs.html:128-129) rather than skipped at the call site — equivalent outcome; the catch-path filter with a synthetic `{level:'info'}` payload exceeds the plan's contract but matches its intent.
- Per-request full map copies in `handleProviderDetail` (`ProvidersSnapshot()` + `ProviderSnapshot()` for two single-key lookups) are consistent with `handleMappingDetail` and the rest of the file on a click-driven local endpoint. Not a finding.

## Triage Outcome

All 8 findings triaged and fixed on 2026-08-10. Nothing skipped, dismissed, or accepted as risk.

| Finding | Decision |
|---|---|
| F1 — inert `?provider=` on /providers | FIXED (Fix A — server-side filter) |
| F2 — missing `h.Stats != nil` guard | FIXED |
| F3 — SSE filter throw / misclassification | FIXED |
| F4 — tautological 404 assertion | FIXED |
| F5 — Phase 3 fix untested | FIXED (Fix A — new e2e spec, deliberate-break verified) |
| F6 — `.provider-badge` CSS after `<a>`→`<button>` | FIXED |
| F7 — test fixture/env/table hygiene | FIXED |
| F8 — `openDrawer` sibling-drawer hardening | FIXED |

### Files touched during triage

- `proxy/web/handlers.go` — `?provider=` filter in `handleProviders`; nil-safe `h.Stats` in `handleProviderDetail`
- `proxy/web/templates/logs.html` — `filterValue` helper; SSE listener restructured
- `proxy/web/static/app.js` — `openDrawer` closes sibling drawers
- `proxy/web/static/app.css` — `.provider-badge` font/cursor
- `proxy/web/handlers_provider_detail_test.go` — table-driven, shared fixture, real negative assertion
- `proxy/web/handlers_logs_filter_test.go` — docstring corrected
- `proxy/web/handlers_dashboard_test.go` — uses shared fixture
- `proxy/web/handlers_phase1_test.go` — `newRenderHandlers` carries a `LogSink`
- `e2e/tests/logs-filter-tail.spec.ts` — **new**, guards the client-side SSE filter

### Post-triage verification

| Command | Result |
|---|---|
| `mage test` (race) | PASS — 8 packages ok, `proxy/web` 70.0% |
| `mage lint` | PASS — 0 issues |
| `mage build` | PASS |
| `npx playwright test` (full e2e) | PASS — 61/61 |

Deliberate-break check on the new e2e guard: with `logLinePassesFilters` short-circuited, `logs-filter-tail.spec.ts` fails on the non-matching line (`unexpected value "1"`), then passes again once restored.

### Still open (not findings)

The four manual Progress boxes (1.5, 2.6, 3.2, 4.5) remain `[ ]` and require a running instance. Note that 2.6's pre-filter clause is now actually implemented (F1), so it is verifiable as written.
