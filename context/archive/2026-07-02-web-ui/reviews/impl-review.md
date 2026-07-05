<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Replace Bubble Tea TUI with embedded web UI

- **Plan**: context/changes/web-ui/plan.md
- **Scope**: Phases 1–4 (all phases complete per Progress)
- **Date**: 2026-07-04
- **Verdict**: APPROVED (post-triage)
- **Findings**: 5 critical (4 fixed, 1 withdrawn), 6 warnings (all fixed), 2 observations (both fixed)

## Verdicts

> Headline verdicts were REJECTED pre-triage. After triage every actionable
> finding was resolved (FIXED) or withdrawn (F1 was a false positive from
> misreading test-tool output). Verdicts below reflect the post-triage state.
> `mage vet`, `mage lint`, `mage test` (all packages green with race
> detection), and `mage govulncheck` all pass. `mage build` produces a 21.4 MB
> binary. `go list -m all | grep -c charm` returns 0.

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS (F3, F4, F8, F9, F10 fixed; F1 withdrawn) |
| Scope Discipline | PASS (extras documented: index.html, types.go pulled forward) |
| Safety & Quality | PASS (F2, F5, F12, F13 fixed; F1 withdrawn) |
| Architecture | PASS (F7 cache restores parse-once intent; auth mux-wrap adds one choke point) |
| Pattern Consistency | PASS (F6 json.Marshal discipline restored) |
| Success Criteria | PASS (F4 charm.land dropped; F11 rollback tests added; tests + lint + govulncheck green) |

## Findings

### F1 — `handleDeleteProvider` deadlocks: RLock under Lock

- **Severity**: ❌ CRITICAL (WITHDRAWN — false positive)
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:288
- **Detail**: Originally reported as a self-deadlock based on an rtk `go test` timeout at 90s where the proxy/web package showed "1 failed". Re-investigation: three consecutive runs of `go test -count=1 -timeout 30s ./proxy/web/...` at HEAD pass all 50 tests in seconds. The original `handleDeleteProvider` uses `cfg.RLock()` for the in-use check (RLock-under-RLock is legal in Go's RWMutex — multiple readers can stack), then `cfg.RUnlock()`, then `cfg.Lock()` for the actual delete — different from what the stale stack-trace snippet suggested. The earlier 90s "hang" was rtk/mage invocation overhead, not a code deadlock. No fix needed.
- **Decision**: DISMISSED — false positive from misreading test-tool output.

### F2 — Writeback + page routes bypass `FREEDIUS_UI_TOKEN`

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:23,36-69
- **Detail**: `eventstream.Handlers.Register` wraps the four SSE/JSON routes (`/v1/events`, `/v1/logs`, `/v1/stats`, `/v1/config`) with `requireAuth` (handlers.go:37-40). But the page handlers (`GET /`, `GET /logs`, `GET /providers`, `GET /mappings`) and every writeback route (`POST /v1/providers`, `PUT/DELETE /v1/providers/{name}`, `/v1/mappings`×3) are mounted directly in `SetupMux` (handlers.go:36-69) with no auth wrap. So when `FREEDIUS_UI_TOKEN` is set, the SSE stream is gated but the dashboard pages, the log snapshot page (which echoes the 10k ring buffer), and every CRUD endpoint that writes `~/.config/freedius/config.yaml` are publicly accessible. README.md:35 advertises "Set `FREEDIUS_UI_TOKEN` to require authentication on all dashboard routes" — false. The plan's Critical Implementation Detail #2 ("Auth must gate all routes when set, not just writeback — logs can leak upstream API keys via error messages") is explicitly violated.
- **Fix**: Export `eventstream.Handlers.RequireAuth(http.Handler) http.Handler` and wrap every `SetupMux` route through it. Simplest: build the mux with a single top-level middleware that gates all paths when `AuthToken != ""` (one choke point instead of per-route).
  - Strength: One middleware covers all routes; matches plan intent ("auth gates all routes when set").
  - Tradeoff: Slightly larger surface to test — add a test asserting allulo routes 401 without token when token is configured.
  - Confidence: HIGH — the gap is structural and documented in the plan.
  - Blind spot: Doesn't verify whether the existing auth_test.go could be reused — likely yes since `Register`'s wrapping tests already prove the middleware works.

### F3 — `proxy/web/static/htmx.min.js` is a 10-line placeholder, not the vendored bundle

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/htmx.min.js:1-10
- **Detail**: Plan §1.9 contract: file is the concatenation of `htmx.org@2.0.4/dist/htmx.min.js` + `htmx.org-ext-sse@2.2.2/sse.js`, pinned versions, with a sidecar `htmx.min.js.sha256` for drift detection (Critical Implementation Detail #6). Actual file is a stub: header comment + `(function(){var htmx=function(){...})() // core — shortened for embed` + a note that "Inline expansion would be 0-compress." No SHA256 sidecar exists. The dashboard's `/logs` page declares `hx-ext="sse" sse-connect="/v1/logs"` (logs.html:13) and the providers/mappings modals post via `hx-post`/`hx-target` (providers.html:26, mappings.html:22-34) — none of this works without a real htmx runtime. Manual Phase-2 verification ("Browser: /logs shows recent entries … new log lines stream in live via SSE") is therefore false in any real browser.
- **Fix**: Fetch the two pinned files (`https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js`, `https://unpkg.com/htmx.org-ext-sse@2.2.2/sse.js`), concatenate them in that order into `proxy/web/static/htmx.min.js`, and generate `htmx.min.js.sha256` via `sha256sum`. Commit both. Updated embed_test.go (which only checks the file exists, not its content) will still pass.
  - Strength: Satisfies the load-bearing pinning + drift-detection contract; restores the dashboard's load-bearing JS for SSE tail + CRUD forms.
  - Tradeoff: Adds ~25 KB to the binary; matches the plan's stated "single static binary" budget.
  - Confidence: HIGH — the vendors and version pins are named in the plan.
  - Blind spot: Don't have a way here to download the files from the review context — needs an `npm pack`-equivalent or direct curl; verify with `grep -c "htmx"` in the result.
- **Decision**: FIXED via Fix now. Real htmx.org@2.0.4 (50 KB) + htmx-ext-sse@2.2.2 (8.8 KB) concatenated into `proxy/web/static/htmx.min.js`; `htmx.min.js.sha256` (`12c7f474ece0ec9c915171c4170a36dcaedaa144a90f3d2b9d09db66d2cdb8ca`) committed as sidecar. Note: the plan's package name was `htmx.org-ext-sse` (typo); the real npm package is `htmx-ext-sse` (no `.org`). The version pin (2.2.2) and htmx core (2.0.4) were honoured exactly. Embed tests pass.

### F4 — `go.mod` still requires `charm.land/*` (Phase 4.8 skipped)

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: go.mod:6-8
- **Detail**: Plan §4.8 contract: "Run `mage tidy`; commit resulting diff. Verify `go list -m all | grep charm.land` returns empty." Progress §4.3 marks this `[x]` at commit `b504e82`. Actual: `git diff --stat 5374307..HEAD -- go.mod go.sum` is empty (files not touched); `go list -m all | grep charm` returns 11 modules including direct `charm.land/bubbles/v2 v2.1.0`, `charm.land/bubbletea/v2 v2.0.7`, `charm.land/lipgloss/v2 v2.0.4` and 8 transitive `github.com/charmbracelet/*`. The recorded success criterion is rubber-stamped — false. Binary is 21 MB (Dockerfile expects ~5-10 MB reduction); the Dockerfile's `go mod download` will pull `charm.land/*` into the build context unnecessarily. Note the source code does not import `charm.land/*` anywhere (the TUI is deleted), so `go mod tidy` should drop them automatically — meaning this fix is one `mage tidy` commit away.
- **Fix**: Run `go mod tidy` (or `mage tidy`), verify `go list -m all | grep -c charm` returns 0, commit `go.mod` + `go.sum`. Re-record binary size delta in a follow-up commit message.
  - Strength: One-command fix; restores the success criterion and shrinks the binary toward the plan's expected delta.
  - Tradeoff: Triggers a CI re-run; non-functional change.
  - Confidence: HIGH — the deps are unused in source post-Phase 4.
  - Blind spot: Haven't confirmed whether any test still imports `charm.land/*` transitively via other deps; unlikely.
- **Decision**: FIXED via Fix now (triage). `go mod tidy` dropped all 11 charm.land / github.com/charmbracelet modules from go.mod + go.sum. `go list -m all | grep -c charm` returns 0. All 97 tests pass post-tidy. Note: the binary size remained ~21 MB — the plan's predicted "~5-10 MB reduction from charm.land removal" was optimistic; the charm.land packages were already dead-code-eliminated by the linker (no source paths referenced them post-Phase-4). The visible benefit is the `go mod download` cache footprint for Docker builds, not binary size. Plan §4.4's "binary measurably smaller" criterion remains unsatisfied, but that's a plan-spec error not an implementation defect.

### F5 — `text/template` instead of `html/template` → stored XSS via provider names

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/embed.go:12,21
- **Detail**: `loadPageTemplate` imports `text/template` (embed.go:12), which performs NO context-aware HTML escaping. Templates render user-controlled strings — provider `Name`, `Behavior`, `BaseURL`, `APIKeyEnv`, `Protocol`, mapping `Name`/`ProviderName`/`Model`, log `Line` — into HTML. Worse: providers.html (and mappings.html) inject `{{.Name}}` directly into JS event handlers like `onclick="editProvider('{{.Name}}', ...)"`. Form validation (forms.go:113-118) only rejects `\r\n:` — NOT `'`, `"`, `<`, `>`, `\`. A provider named `');alert(1);//` passes validation and breaks out of the JS string literal at edit time → stored XSS executed in the dashboard operator's browser. Combined with F2 (no auth on these routes), an attacker who can reach `:8083` can plant the payload via unauthenticated POST and trigger it on a legitimate operator's next visit. AGENTS.md says the project uses stdlib conventions; `html/template` is the stdlib HTML-safe template engine and is what the plan implicitly assumed (it lists `html/template` in §Overview). `text/template` is the wrong import.
- **Fix**: Change `embed.go:12` import from `"text/template"` to `"html/template"`; rename the return type `*template.Template` (same name from `html/template`). `html/template` auto-escapes in HTML, attribute, JS-string, and (Go 1.21+) JS contexts. Existing embed_test.go (which only checks for `/static/htmx.min.js` literal) still passes.
  - Strength: One-line import swap; restores the plan's stated `html/template`; mitigates a stored-XSS class.
  - Tradeoff: Any template that intentionally used `{{template "x" .}}` with raw HTML needs review — but the current templates don't use raw HTML escapes.
  - Confidence: HIGH — the import is the difference; `html/template`'s escape tables are well-tested.
  - Blind spot: Haven't run the template render tests through the new import to confirm no escape surprises — needs a `mage test` after the swap.
- **Decision**: FIXED via Fix now. Swapped `proxy/web/embed.go` import from `text/template` to `html/template` (return type `*template.Template` is the same type name in both packages, so signature unchanged). All 50 proxy/web tests pass after the swap. Verified by a one-off test rendering `<a onclick="editProvider('{{.Name}}')">{{.Name}}</a>` with payloads `');alert(1);//`, `<script>`, `x\y` — `html/template` correctly escapes `'`→`\u0027` in JS-string context and `<`/`>`→`<`/`>` in HTML context. The stored-XSS class is closed at the template layer; tightening form validators (forms.go:113-118) to additionally reject `'`/`"`/`<`/`>` was deemed unnecessary since `html/template` handles those contexts.

### F6 — `writeJSON` / `writeValidationError` use `json.NewEncoder(w).Encode` (lessons.md §1 drift)

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/handlers.go:442,452
- **Detail**: `writeJSON` and `writeValidationError` call `json.NewEncoder(w).Encode(v)` after `w.WriteHeader(status)`. The new `internal/eventstream/handlers.go` deliberately uses `json.Marshal` + `w.Write(buf)` (handlers.go:171,177) and the file's own doc comment (line 20) calls out lessons.md §1 — "json.Marshal only, never json.NewEncoder." These JSON responses are not SSE frames so the triple-newline bug doesn't fire, but the package-level invariant claimed in the doc comment is violated and the response bodies end with a trailing `\n` (encoder behavior). Mixed encoding patterns within the same web package.
- **Fix**: Replace both occurrences with `buf, _ := json.Marshal(v); _, _ = w.Write(buf)`.
- **Decision**: FIXED via Fix now. Both `writeJSON` and `writeValidationError` now use `json.Marshal(v)` + `w.Write(buf)`. All 68 proxy/web + eventstream tests pass post-edit. Package invariant ("json.Marshal only, never json.NewEncoder") now consistent.

### F7 — Templates parsed per request, not at startup

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/embed.go:21-27, proxy/web/handlers.go:46
- **Detail**: `renderPage` calls `loadPageTemplate(pageFile)` on every page hit, which calls `template.ParseFS(assets, "templates/layout.html", "templates/"+pageFile)` — parsing 2 files from the embed.FS per request. Plan's Performance section explicitly says "Template parsing: at startup, once; cached in `*template.Template` (avoid per-request `ParseFiles`)." On a hot path (e.g., `/logs` retry loop or scan), this re-parses on every render and defeats any error caching.
- **Fix A ⭐ Recommended**: Parse all page templates once in `NewServer` (or a `sync.OnceValue`) into a single `*template.Template` set; `renderPage` does `t.ExecuteTemplate(w, "layout", data)`.
  - Strength: Matches the plan's stated optimization; cuts per-request syscalls to zero.
  - Tradeoff: One structural change in `embed.go`; existing `loadPageTemplate` becomes setup-only.
  - Confidence: HIGH — straightforward stdlib pattern.
  - Blind spot: Need to ensure page-specific `{{define "content"}}` blocks don't collide when parsed together (the per-page call exists precisely to avoid that — test it).
- **Fix B**: Cache per-page templates with `sync.Map[string]*template.Template` populated lazily on first render.
  - Strength: Minimal change; preserves current parse-1-page-at-a-time shape.
  - Tradeoff: First-hit latency still pays a parse; map needs invalidation if assets ever swap (they won't with embed.FS — so this is fine).
  - Confidence: MED — works but Fix A is cleaner.
  - Blind spot: Concurrency of lazy cache init.
- **Decision**: FIXED via Fix A (with sync.Map cache mechanism). Pure single-set parse was infeasible because each page defines `{{define "content"}}` and they'd collide — confirmed by reading the templates. Instead implemented a `sync.Map[string]*template.Template` in embed.go that caches each per-page parse on first render (`LoadOrStore`). Achieves the plan's "parse once, cache" intent: zero syscalls on the hot path post-first-hit, race-safe. Verified by `go test -race ./proxy/web/...` — 50 tests pass with the race detector.

### F8 — `mappings.html` provider dropdown is empty — broken Add Mapping flow

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/web/templates/mappings.html:22-34
- **Detail**: Plan §3.4 contract: "3-field form (name, provider dropdown sourced from `Config.ProvidersSnapshot()`, model)." Actual mappings.html:22-34 form has `<select name="provider_name">` with NO `{{range}}` — the dropdown is rendered empty, so users can't pick a provider through the modal. Add Mapping save will fail with a "provider not found" or "empty provider" validation error every time through the UI. Manual Phase-3 verification §3.5 ("Add/edit/delete a mapping through the UI persists") is therefore false.
- **Fix**: Pass `Providers: collectedProviders(cfg)` into the mappings page data and `{{range .Providers}}<option value="{{.Name}}">{{.Name}}{{end}}` inside the `<select>`. Either populate via the existing `handleMappings` handler's render data (needs a `providersData` field added to `mappingsData` in types.go) or render a small partial.
  - Strength: Restores the planned UX; makes Add Mapping actually work.
  - Tradeoff: Minor template wiring; needs a test.
  - Confidence: HIGH — the providers table already iterates the same data correctly.
  - Blind spot: Whether `handleMappings` currently passes only `Mappings` (it does) — needs the extra field.
- **Decision**: FIXED via Fix now. Added `Providers []providerRow` to `mappingsData` (types.go); `handleMappings` now builds `providerRows` from `cfg.ProvidersSnapshot()` (reusing `providerRow` shape) and passes it; mappings.html's `<select name="provider_name">` now ranges `{{.Providers}}<option value="{{.Name}}">{{.Name}}{{end}}`. All tests pass.

### F9 — `parseMinLevel` silently accepts invalid `?min=` (returns 200, not 400)

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go:151-167, proxy/web/log_filter_test.go:78-89
- **Detail**: Plan §2.9 contract: "`?min=invalid` returns 400 with JSON error." Actual `parseMinLevel` returns `nil` for any unknown string (handlers.go:165), so `handleLogs` renders all entries with 200. The test `TestHandleLogs_InvalidFilter` (log_filter_test.go:78-89) asserts 200 — confirming the divergence is not a test gap but a behavioral drift that was *codified* into the test. Hidden misconfiguration: a typo'd `?min=warrn` silently shows debug logs; an operator chasing a noisy incident page can't tell.
- **Fix**: In `parseMinLevel`, return a sentinel error for non-empty unknown input; in `handleLogs`, on that error respond `400 {"error":"invalid_filter","message":"min must be one of debug|info|warn|error"}`. Update `TestHandleLogs_InvalidFilter` to assert 400 + JSON.
- **Decision**: FIXED via Fix now. `parseMinLevel` signature changed to `(*slog.Level, error)`: returns `nil, nil` for empty (no filter), `(level, nil)` for known, `(nil, error)` for non-empty unknown. `handleLogs` returns `400 {"error":"invalid_filter","message":"min must be one of debug|info|warn|error, got \"<v>\""}` on error. `log_filter_test.go` rewritten: `TestParseMinLevel` now expects `(nil, true)` for `{all, invalid, warrn}`; `TestHandleLogs_InvalidFilter` asserts 400 + JSON `error":"invalid_filter"` + `Content-Type: application/json`. All 98 tests pass across proxy/web + eventstream + cmd/freedius.

### F10 — `handleLogs` `Level: filter.Label` missing — dropdown selection state lost on rerender

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go:95-98, proxy/web/templates/logs.html:8
- **Detail**: Plan §2.6 contract: `handleLogs` renders with `Active:"logs"`, `Entries: filtered`, `Level: filter.Label`. Actual logsData construction (handlers.go:95-98) only sets `pageData` + `Entries` — no `Level` field. But logs.html:8 has `{{if eq .Level "debug"}}selected{{end}}` etc., so the dropdown never highlights the active filter on a `?min=warn` rerender. UX defect: every `?min=` load shows "all" as selected.
- **Fix**: Add `Level: levelLabel(*minLevel)` (or `"all"` if `minLevel == nil`) to the `logsData{…}` literal at handlers.go:95; add a `"all"` case to logs.html's `eq`.
- **Decision**: FIXED via Fix now. Added `Level string` field to `logsData` (types.go); `handleLogs` populates `Level: levelLabel(*minLevel)` (or `""` when nil); logs.html's existing `{{if not .Level}}selected{{end}}` highlights "All" by default and `{{if eq .Level "X"}}selected{{end}}` highlights the active filter. Verified by `go test ./proxy/web/...` — 51 tests pass.

### F11 — Phase-3 evicted "rollback on PUT/DELETE save failure" test cases missing

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/web/handlers_write_test.go:114-184
- **Detail**: Plan §3.7 contract: "~15 tests … PUT updates an existing provider; rollback on save failure … DELETE removes a provider; rollback on save failure." Actual suite has `TestUpdateProvider` (114) and `TestDeleteProvider` (148) covering the happy path + `_NotFound` cases; only `TestCreateProvider_SaveFailure` (78) forces a save failure. PUT-rollback and DELETE-rollback under save failure are not tested. Given F1 (the only DELETE provider path is currently unreachable because of deadlock), the rollback-on-DELETE-save-failure path can't be exercised even if it were written. Once F1 is fixed, the two missing test cases are cheap to add.
- **Fix**: Add `TestUpdateProvider_SaveFailure` (read-only `cfgPath` triggers 500 + `cfg.Providers[name] == old`) and `TestDeleteProvider_SaveFailure` (after F1 fix: read-only path → 500 + provider restored).
- **Decision**: FIXED via Fix now. Added `TestUpdateProvider_SaveFailure` (mutate nim → read-only cfgPath → 500 + behavior/base_url restored to original) and `TestDeleteProvider_SaveFailure` (pre-add a `lonely` provider → read-only cfgPath → 500 + `cfg.HasProvider("lonely")` true). Both tests execute the rollback branch and confirm in-memory state survives the failed save. All 100 tests pass across the three packages. (Note: F1 was withdrawn — no dependency on it.)

### F12 — `Bearer ` prefix check uses byte-slice `==` (non-constant-time)

- **Severity**: 💬 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: internal/eventstream/handlers.go:53
- **Detail**: `token[:7] == "Bearer "` is a slice equality (compiler emits a byte-by-byte compare with early exit), so the prefix length is timing-leakable. The actual token compare at handlers.go:56 correctly uses `subtle.ConstantTimeCompare`. Realistic impact: a local/LAN attacker can learn that the token *doesn't* start with `Bearer ` (it always does in practice), so this is a micro-leak only. Tests in auth_test.go don't cover non-Bearer-shaped headers' timing.
- **Fix**: Strip the literal `"Bearer "` only when present; the prefix check is structural, not secret. No change needed unless the threat model wants constant time on the whole header.
- **Decision**: FIXED via Fix now. Dropped the `token[:7] == "Bearer "` byte-slice compare entirely. The handler now compares the whole `Authorization` header against both acceptable shapes — `"Bearer <token>"` and `"<token>"` — using two `subtle.ConstantTimeCompare`s OR'd. No non-constant-time compare remains on the secret. Existing auth_test.go tests pass unchanged (they send `Bearer <token>` shaped headers, still accepted).

### F13 — Web server bind failure is silent

- **Severity**: 💬 OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: cmd/freedius/main.go:170-174
- **Detail**: The proxy server uses `startProxyServer` + `waitForBind` and crashes the process if `:8082` is taken. The web server (main.go:169-174) starts in a goroutine that only logs errors; if `:8083` is taken the proxy keeps running on `:8082` and the dashboard silently disappears — operator gets a log line, no exit code, no crash. `Server.ReadHeaderTimeout` is set (server.go:40) but no `WriteTimeout`/`IdleTimeout`, while the proxy sets all three (main.go:211-214) — divergent hardening. Not a blocker since the proxy is the load-bearing port, but it can mask dashboard failures in Docker (`docker logs` shows the line but the operator might miss it).
- **Fix**: Mirror `startProxyServer`'s bind-verification pattern for the web server too — call `waitForBind` (or a small `net.Listen` probe) before launching the goroutine; on failure `failf` and exit non-zero.
- **Decision**: FIXED via Fix now. Split `WebServer.ListenAndServe` into `Listen()` (bind, synchronous) + `Serve()` (accept loop, goroutine-friendly). `main.go` now calls `Listen()` directly and `failf`s on bind error before launching `Serve()` in the goroutine — so a port conflict on `:8083` exits the process non-zero, matching the proxy server's `waitForBind` pattern. Refrained from adding `WriteTimeout`/`IdleTimeout`: long-lived SSE streams (`/v1/events`, `/v1/logs`) hold the writer open for the subscription lifetime; setting WriteTimeout would close live SSE streams mid-event. Documented the rationale in `server.go` comments. All 100 tests pass.
