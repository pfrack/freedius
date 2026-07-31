<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Mapping-First UI Refactor

- **Plan**: context/changes/mapping-first-ui-refactor/plan.md
- **Scope**: All 5 phases (Phases 1–5; `change.md.status: implemented`, Progress section is 100% unchecked but evidence is in diff)
- **Date**: 2026-07-31
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 8 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | FAIL |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | WARNING |

## Findings

### F1 — Resource names not URL-encoded in links, paths, and JS URLs

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/index.html:72; proxy/web/templates/providers-table.html:35,50; proxy/web/templates/mappings-table.html:39,55–66; proxy/web/templates/mappings.html:100,168
- **Detail**: `html/template` prevents HTML injection, but provider/mapping names are interpolated into query strings, path segments, and inline-JS URLs without component encoding. Names containing `&`, `?`, `#`, `/`, or spaces can truncate filters or send DELETE requests to a different resource. CRUD handlers in `proxy/web/handlers.go:545-548` strip paths manually instead of using `r.PathValue` from Go 1.22 route patterns. This change adds new such interpolations (provider-name links to `/mappings?provider=Name`, mapping-name paths in inline JS) without enforcing URL-safety.
- **Fix A ⭐ Recommended**: Forbid URL-unsafe characters in provider/mapping names at the config-validation layer so all interpolations become safe by construction.
  - Strength: One fix point eliminates the class across all templates; matches the "env var + stdlib" AGENTS.md discipline.
  - Tradeoff: Breaking change for any users with non-conforming names in their config (likely zero — names are user-chosen and rarely exotic).
  - Confidence: HIGH — standard Go config validation pattern.
  - Blind spot: Existing configs with names like `my/co/provider` would fail validation; would need a migration warning.
- **Fix B**: Encode at the template-call site (`url.PathEscape` via a template func).
  - Strength: Backward compatible.
  - Tradeoff: Touches every interpolation; easy to miss one when adding new templates.
  - Confidence: MED — template-func approach is standard but easy to forget.
  - Blind spot: Inline `js:` URLs (`mappings-table.html:55–66`) still need separate handling.
- **Decision**: FIXED via Fix A — added `validateResourceName` in `config/config.go` rejecting `/`, `?`, `#`, `&`, `=`, `%`, space, and control chars; called from `validateProvider` and `validateMapping`. Closed pre-existing "empty mapping key accepted (validation gap)" by inverting the test. All config (80) and web (109) tests pass.

### F2 — Dashboard section titled "Mappings" instead of "Recent Mappings"

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/templates/index.html:58
- **Detail**: Phase 2 contract specified a "Recent Mappings" section. Implementation uses heading "Mappings" (line 58) and reuses the full `mappings-table` fragment instead of a recent-N variant. Hierarchy intent is preserved; only the label drifts.
- **Fix**: Rename the heading from `Mappings` to `Recent Mappings` in `index.html:58`.
- **Decision**: FIXED — heading renamed; web tests still pass (109).

### F3 — Old `.stats-strip` not removed; new test codifies retention

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/web/templates/index.html:45–54; proxy/web/static/app.css:498–527; proxy/web/handlers_dashboard_test.go:145–180
- **Detail**: Phase 2 contract: "Remove or deprecate `.stats-strip` (replaced by stats-grid)". Implementation kept both: `stats-grid` is rendered at lines 6–42 AND the legacy `stats-strip` block is still rendered at lines 45–54. CSS for `.stats-strip` is preserved (lines 498–527) along with responsive overrides. Worse, `TestIndexHandler_StatsPreserved` (handlers_dashboard_test.go:145–180) explicitly asserts the old strip remains — baking the drift into a test.
- **Fix A ⭐ Recommended**: Delete the `.stats-strip` markup in `index.html`, the CSS block, and remove the test asserting it. The new `.stats-grid` is the visual replacement.
  - Strength: Honours the plan contract; removes now-dead UI surface; trims CSS.
  - Tradeoff: Loses whatever historical rationale kept the strip (none found in the plan or lessons).
  - Confidence: HIGH — contract is explicit; no other reference in code.
  - Blind spot: Anyone relying on the old Uptime/Host stats would need `.stats-grid` to surface them — they don't (new stats are mapping-centric by design).
- **Fix B**: Keep strip but rename in CSS to `.stats-strip--legacy` and document.
  - Strength: Backward compatible.
  - Tradeoff: Dead code grows; the test still codifies retention.
  - Confidence: LOW — against explicit plan contract.
  - Blind spot: Future readers won't know why both exist.
- **Decision**: FIXED via Fix A — deleted `.stats-strip` markup from `index.html`, removed all four `.stats-strip*` CSS rules (block + responsive override), and deleted `TestIndexHandler_StatsPreserved`. Web tests still pass (108, was 109).

### F4 — Log filter state normalized instead of preserving raw query value

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go:143–144, 188–194
- **Detail**: Phase 5 contract: `logsData.Provider = q.Get("provider")`, `logsData.Mapping = q.Get("mapping")`. Implementation normalizes via `strings.ToLower(strings.TrimSpace(...))` before assigning. Visible filter state in the input may differ from URL and from the original user input.
- **Fix A ⭐ Recommended**: Pass raw values to `logsData` for display; only normalize when applying the filter to the log query.
  - Strength: Preserves user intent in the UI; matches contract; lets case sensitivity stay user-controlled.
  - Tradeoff: Two variables instead of one, but cleanly scoped.
  - Confidence: HIGH — small handler change, no template impact.
  - Blind spot: None significant.
- **Fix B**: Keep normalization but reflect it back in the URL via `hx-replace-url`.
  - Strength: URL is the source of truth.
  - Tradeoff: User-typed casing is silently rewritten — surprising for power users.
  - Confidence: MED.
  - Blind spot: URL is source of truth, but original casing may differ.

- **Decision**: FIXED via Fix A — split raw `q.Get` into `providerQuery`/`mappingQuery` stored in `logsData`, and pass only normalized `providerFilter`/`mappingFilter` to the filter loop. Web tests pass (108).

### F5 — Live SSE log stream ignores active filters

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/logs.html:12–52, 56–60, 69–87
- **Detail**: Phase 5 introduces provider/mapping/level filters that correctly reload `#log` over HTMX, but the live SSE connection (`sse-connect="/v1/logs"`) is opened without filter parameters, and the listener appends every incoming event unconditionally. As soon as a new log arrives, the filtered view becomes unfiltered — directly contradicting success criterion 5.3 ("Provider and mapping filters work") in spirit.
- **Fix A ⭐ Recommended**: Pass current filter values to the SSE endpoint on connect, and apply them server-side before streaming.
  - Strength: Honors filter intent end-to-end; matches how HTMX already passes them on reload.
  - Tradeoff: SSE connection lifecycle needs to reconnect when filters change (hx-trigger or filter-watcher).
  - Confidence: MED — depends on whether the SSE handler currently supports filter args.
  - Blind spot: Reconnect-on-filter-change UX needs care; may flicker.
- **Fix B**: Disable SSE appends while any filter is active; show a "live paused while filtered" hint.
  - Strength: Simple; no server changes.
  - Tradeoff: Loses live visibility during filtering, which is a common reason users watch logs.
  - Confidence: HIGH — easy frontend-only change.
  - Blind spot: Users may forget filters are on and miss live events.
- **Decision**: FIXED via Fix A — added server-side filtering to `internal/eventstream/handleLogs` (reads `?min`, `?provider`, `?mapping`, applies the same case-insensitive filter as the web `/logs` handler to both replayed and live log entries); added `parseLevel` helper; updated `logs.html` to rebuild `sse-connect` from current filter inputs on filter change and after HTMX reload. Build + tests pass (126 across web + eventstream). Note: this edit also caps the live DOM at 200 nodes as a side effect, addressing F6.

### F6 — Live log DOM grows unbounded

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/logs.html:77–87
- **Detail**: Initial render is capped at 200 entries (`handlers.go:148–169`), but each live SSE event is appended permanently. Long-running dashboards accumulate unbounded `<pre>` nodes and grow scroll/layout cost. Phase 5 did not touch SSE behavior; the issue surfaces because logs.html is the changed surface.
- **Fix**: After appending a live entry in the SSE listener, remove the oldest when the count exceeds 200.
- **Decision**: FIXED — already applied as a side effect of F5's fix (appendLog() caps live DOM at 200 nodes).

### F7 — Dynamic fallback controls lack accessible names

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/templates/mappings.html:148–175
- **Detail**: The mappings page (refactored by this change as an EXTRA outside the planned file list) now creates fallback-row provider/model selects, a fetch button containing only an SVG, and a remove button containing only `×`, all without `aria-label` or associated labels. The static hamburger button is correctly labeled; the dynamic rows are not.
- **Fix**: Add `aria-label` to the SVG fetch and `×` remove buttons; associate each `<select>` with a visually-hidden `<label>` or `aria-label`; mark decorative SVGs `aria-hidden="true"`.
- **Decision**: FIXED — added `aria-label` ("Fallback provider", "Fallback model string", "Fetch models...", "Remove this fallback") to selects/inputs/buttons, and `aria-hidden="true"` to the decorative SVG. Also wrapped the provider name in the inline `htmx.ajax` URL with `encodeURIComponent`. Build + web tests pass (108).

### F8 — Broad design-system rewrite in app.css exceeds plan scope

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Scope Discipline
- **Location**: proxy/web/static/app.css (across the file)
- **Detail**: Plan called for adding the specific contracted utilities (`.stats-grid`, `.card--stat`, `.page-section`, `.section-header`, `.providers-summary`, `.route-step__model/provider`, `.badge--status-ok/warn`, `.btn--danger-subtle`, `.url-truncate`, `.log-filters`). Implementation also rewrote the color palette, font stack, sidebar width, spacing/radii, shadows, animation system, global typography, card/table/form/dialog styling, noise effects, and many components. This is a UI/brand overhaul disguised as a mapping-first refactor.
- **Fix A ⭐ Recommended**: Split the work — accept the mapping-first structural changes now, and move the design-system rewrite into a separate change (e.g., `web-ui-design-upgrade` already exists at `context/changes/web-ui-design-upgrade/` with that exact framing).
  - Strength: Honors plan scope discipline; keeps `mapping-first-ui-refactor` reviewable; reuses an existing planned destination.
  - Tradeoff: The shipped artifact mixes both intents — a future review can't tell where one ends and the other begins.
  - Confidence: HIGH — `web-ui-design-upgrade` already exists as the intended home for this work.
  - Blind spot: `web-ui-design-upgrade` is `status: planned` — its plan may need rework now that this is partially done.
- **Fix B**: Document the rewrite as a deliberate scope expansion in this change's `change.md`.
  - Strength: Preserves the work; transparent about what was actually shipped.
  - Tradeoff: Continues the pattern of bundling unrelated work.
  - Confidence: MED — depends on stakeholder appetite for retroactive scope docs.
  - Blind spot: Retroactive docs don't help the planning process next time.
- **Decision**: ACCEPTED via Fix A — no code revert. The design-system rewrite is already shipped and tests pass; queued a follow-up at `context/changes/mapping-first-ui-refactor/follow-ups/review-fixes.md` documenting that `web-ui-design-upgrade` should absorb the already-implemented palette/fonts/typography/animations so its plan accounts for shipped work.

### F9 — External CDN font dep + favicon add third-party runtime to a single-binary project

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Scope Discipline
- **Location**: proxy/web/templates/layout.html:9–11 (CDN), layout.html (favicon/meta)
- **Detail**: AGENTS.md says "Compiles to a single static binary; zero external runtime dependencies." Phase 1 contract was "only DOM order changes." Implementation adds a `cdn.jsdelivr.net` stylesheet for the Geist font (floating `geist@1` version, no SRI hash) plus favicon/meta/skip-link additions. External CDN adds network availability and supply-chain exposure to a tool whose selling point is offline/self-contained operation.
- **Fix**: Either remove the CDN link and self-host the font (or use system fonts), or pin an exact version with `integrity=` and `crossorigin=` and document the dependency.
- **Decision**: PENDING

### F10 — CSS contract values differ in five selectors

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/app.css:473, 477, 1311, 1369, 982
- **Detail**: Phase 1–5 contracts specified exact values; implementation differs in five places:
  - `.page-section` margin: `--space-12` (contract: `--space-6`)
  - `.section-header` margin: `--space-5` (contract: `--space-3`)
  - `.route-step__model` font-weight: `550` (contract: `500`)
  - `.btn--danger-subtle:hover` alpha: `0.08` (contract: `0.1`)
  - `.log-filters` gap/alignment/margin: `--space-4`/`flex-end`/`--space-6` (contract: `--space-3`/`center`/`--space-4`)
- **Fix**: Adjust each value to match the plan contract, OR amend the contract retroactively if the new values were deliberate UX tuning.
- **Decision**: PENDING

---

## Notes

- Automated verification: `go build ./...` passes; `go test ./proxy/web/...` passes (109 tests).
- Manual verification: plan `## Progress` section is 100% unchecked even though `change.md.status: implemented` and the diff contains the work. Progress tracking was not maintained — recommend a follow-up to mark boxes either at archive time or as part of accepting this review.
- Plan did not enumerate `mappings.html` as a changed file, but the EXTRA refactor there is consistent with the broader design overhaul in F8.
- Test files (`handlers_dashboard_test.go`, `handlers_phase2_test.go`, `handlers_providers_link_test.go`) were modified, not newly added — the "Embrace Extra Tests" lesson applies only to truly new tests.