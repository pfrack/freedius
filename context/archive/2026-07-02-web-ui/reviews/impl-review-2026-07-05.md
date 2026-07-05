<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Replace Bubble Tea TUI with embedded web UI (Re-review)

- **Plan**: context/changes/web-ui/plan.md
- **Scope**: All 4 phases (all checkboxes marked complete in Progress)
- **Date**: 2026-07-05
- **Verdict**: NEEDS ATTENTION
- **Findings**: 4 critical, 3 warnings, 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS (F1, F2, F3 fixed) |
| Scope Discipline | PASS |
| Safety & Quality | PASS (F4 fixed, F7 fixed) |
| Architecture | PASS (F6 fixed) |
| Pattern Consistency | PASS |
| Success Criteria | PASS (all automated checks pass) |

## Findings

### F1 — Log filter returns full page instead of fragment

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: `proxy/web/handlers.go:77` (`handleLogs`)
- **Detail**: The dropdown filter uses htmx: `hx-get="/logs" hx-target="#log"`. The plan expects the server to return only the log fragment (HTML) to replace the `#log` div. The actual handler always renders the full page via `renderPage("logs.html", ...)`, which would replace the log div with an entire page, breaking the UI.
- **Fix A ⭐ Recommended**: Conditional render — when `HX-Request` header is present, execute only the `logs.html` template (the content block) instead of the full layout.
  - Strength: Minimal change; preserves full page for direct visits.
  - Tradeoff: Server code becomes aware of htmx.
  - Confidence: HIGH — straightforward conditional.
- **Fix B**: Split endpoint — introduce `GET /logs/fragment` that returns only the fragment; update the dropdown to point there.
  - Strength: Clean separation of concerns.
  - Tradeoff: More routes; fragment endpoint not a standalone page.
  - Confidence: HIGH — standard partial pattern.
- **Decision**: FIXED (via Fix A)

### F2 — SSE log stream sends JSON, not HTML

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: `internal/eventstream/handlers.go:130` (`handleLogs` SSE)
- **Detail**: The SSE endpoint for logs marshals `LogEntry` structs and streams JSON. The htmx sse extension swaps the raw data into the DOM, so live entries appear as raw JSON instead of rendered log lines.
- **Fix A ⭐ Recommended**: Server-side HTML rendering — for each log entry, pre-render a `<pre class="log-LEVEL">LINE</pre>` string and send that as the SSE data payload.
  - Strength: Matches plan's server-rendering philosophy.
  - Tradeoff: Requires per-event HTML generation (cheap; cacheable).
  - Confidence: HIGH — small, isolated change.
- **Fix B**: Client-side rendering — keep JSON, add a short script that listens to the SSE stream and builds the DOM elements.
  - Strength: Clean separation; no per-event template work.
  - Tradeoff: Introduces custom JS (the plan used only htmx).
  - Confidence: HIGH — about 10 lines of JS.
- **Decision**: FIXED (via Fix A)

### F3 — Writeback handlers return JSON but front-end expects HTML table

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: `proxy/web/handlers.go:222,264,430` (POST/PUT/DELETE providers/mappings)
- **Detail**: Forms target the table element (`hx-target="#providers"`/`#mappings"`) and expect the server to return updated table HTML for an in-place swap. The handlers return JSON (`{"status":"created","name":...}`), which would corrupt the table view and make the UI appear broken.
- **Fix A ⭐ Recommended**: Render table fragment after mutation — factor out the table rendering into a helper that can be called from the page handlers and the writeback handlers. On success, the writeback handler writes the refreshed `<table>...</table>` HTML directly (200 + HTML).
  - Strength: One coherent rendering path; htmx swap works as intended.
  - Tradeoff: Need to extract table rendering into a shared function (a few lines).
  - Confidence: HIGH — slight refactor, clear path.
- **Fix B**: Return HX-Redirect — after success, send `HX-Redirect: /providers`; htmx reloads the page and the table updates. Simpler logic but extra round-trip.
  - Strength: Minimal code change.
  - Tradeoff: Full page reload inside target may cause layout shifts; not purely in-place.
  - Confidence: MED.
- **Decision**: FIXED (via Fix A)

### F4 — Stored XSS via unescaped values in edit-modal onclick

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: `proxy/web/templates/providers.html:33`, `mappings.html:27`
- **Detail**: Provider/Mapping fields (Name, Behavior, BaseURL, etc.) are injected directly into JavaScript string literals in the `onclick` attribute. A maliciously-crafted value (e.g., `');alert(1);//`) breaks out of the string and executes arbitrary JavaScript in the operator's browser.
- **Fix**: Apply the `js` template filter: `onclick="editProvider('{{.Name | js}}', ...)"` (same for mappings).
  - Strength: One-line change per injection point; uses stdlib JS-escaping.
  - Tradeoff: None.
  - Confidence: HIGH.
- **Decision**: FIXED

### F5 — `requireAuth` accepts bare token (plan specified stripping "Bearer " prefix)

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `internal/eventstream/handlers.go:50-66`
- **Detail**: The plan states: "read Authorization header, strip `Bearer ` prefix, then ConstantTimeCompare". The current implementation compares the full header against both the expected `Bearer <token>` string *and* the bare token, accepting either. While still constant-time, it deviates from the specified algorithm and broadens acceptance.
- **Fix**: Strip the `Bearer ` prefix if present, then compare only the remainder using `subtle.ConstantTimeCompare`. This matches the plan exactly.
  - Strength: Compliance with plan; clearer intent.
  - Tradeoff: None.
  - Confidence: HIGH.
- **Decision**: FIXED

### F6 — EventBus/LogSink single channel (no fan-out for multiple clients)

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: `proxy/eventbus.go:98-103`, `proxy/logtee.go:49-54`
- **Detail**: All consumers share the same underlying channel; events are load-balanced instead of broadcast. With multiple dashboard tabs, each event appears in only one tab. The original TUI design assumed a single client; this limitation persists.
- **Fix A**: Refactor `EventBus`/`LogSink` to maintain a set of subscriber channels and fan out each emit to all.
  - Strength: Supports multiple concurrent dashboards.
  - Tradeoff: Moderate code change; memory overhead for multiple subscribers.
  - Confidence: HIGH — classic fan-out pattern.
- **Fix B**: Document the single-client limitation if it's acceptable for a local single-user tool.
  - Strength: Quick; avoids code change.
  - Tradeoff: Multi-tab use remains broken.
  - Confidence: HIGH.
- **Decision**: FIXED (via Fix A)

### F7 — `writeJSON` and `writeValidationError` ignore `json.Marshal` errors

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: `proxy/web/handlers.go:480-485`, `491-494`
- **Detail**: Both helpers discard the error from `json.Marshal(v)` and then write a nil buffer. If marshaling ever fails (unexpected type), the response would be empty and may cause downstream issues.
- **Fix**: Check the error; on failure log and return `500` with a JSON error payload.
  - Strength: Robustness; consistent error handling.
  - Tradeoff: Minor code addition.
  - Confidence: HIGH.
- **Decision**: FIXED

### F8 — Setting `FREEDIUS_UI_TOKEN` makes the UI inaccessible in browsers

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: `proxy/web/server.go:38‑41` (mux wrap), `cmd/freedius/main.go:166` (reading token)
- **Detail**: When the token is set, every route—including HTML pages—requires `Authorization: Bearer <token>`. A browser cannot send that header on initial page load, making the entire UI unusable. The plan intended protection but not exclusion of browser clients.
- **Fix A ⭐ Recommended**: Exclude page routes from auth — only wrap the API endpoints (`/v1/*`) with `RequireAuth`. Keep the HTML pages public; logs may leak if token is set, but the plan's risk note ("logs can leak upstream API keys via error messages") already addresses that.
  - Strength: Simple; UI remains usable; aligns with typical dashboard auth models (static assets public, API protected).
  - Tradeoff: Logs page visible to unauthenticated local users (but the tool is local-only or optionally token-protected overall).
  - Confidence: HIGH.
- **Fix B**: Add login page — create a minimal login that accepts the token, stores it in `localStorage`, and configures htmx to send `Authorization` on every request. This preserves full protection.
  - Strength: Maintains uniform auth model.
  - Tradeoff: Significant extra work (login UI, CSRF considerations, storage).
  - Confidence: MED.
- **Decision**: PENDING

### F9 — `template.ParseFS` per-request implementation deviates from plan's "parse once at startup"

- **Severity**: 💬 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `proxy/web/embed.go:33-42`
- **Detail**: The plan says: "Template parsing: at startup, once; cached in `*template.Template`". The actual implementation caches per page using a `sync.Map` and parses at first render. While not a parse-on-every-request, it's still a lazy parse-on-first-use. The difference is negligible; caching works. No issue.
- **Decision**: ACCEPTED (no action needed)

### F10 — `Handlers` struct includes extra `CfgPath` field

- **Severity**: 💬 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope
- **Location**: `internal/eventstream/handlers.go:30`
- **Detail**: The plan's `Handlers` struct did not list `CfgPath`. The extra field is required for writeback's `SaveData`. Functional and necessary; no action needed.
- **Decision**: ACCEPTED (no action needed)

---

## Previous Fixes Verified

The earlier implementation review (2026‑07‑04) resolved:
- Deadlock (F1) – confirmed: delete handlers use proper lock ordering.
- Auth bypass (F2) – confirmed: all routes wrapped when token set.
- htmx bundle missing (F3) – verified present and correct.
- charm.land deps (F4) – confirmed removed from `go.mod`.
- XSS via `text/template` (F5) – confirmed switched to `html/template`.
- `json.NewEncoder` pattern (F6) – confirmed `json.Marshal`‑only in `eventstream`.
- Template parsing (F7) – confirmed per-page caching exists.
- Providers dropdown empty (F8) – confirmed populated in `mappings.html`.
- Invalid filter (F9) – confirmed returns 400.
- Level field missing (F10) – confirmed dropdown state preserved.
- Missing rollback tests (F11) – confirmed added.
- Bearer timing (F12) – confirmed constant-time.
- Bind failure (F13) – confirmed web server errors cause exit.

All automated checks (`mage test`, `mage vet`, `mage lint`, `mage govulncheck`) pass.

## Triage Summary

```
═══════════════════════════════════════════════════════════
  TRIAGE COMPLETE
═══════════════════════════════════════════════════════════

  Fixed:     F1, F2, F3, F4, F5, F6, F7   (7)
  Accepted:  F9, F10                        (2)
  Pending:   F8                             (1)

═══════════════════════════════════════════════════════════
```

## Remaining Work

**F8** — `FREEDIUS_UI_TOKEN` makes the UI inaccessible in browsers. This is a WARNING with HIGH impact. Two options remain:
1. ⭐ Exclude page routes from auth (recommended)
2. Add a login page

This should be addressed in a follow-up before the web UI is considered production-ready.
