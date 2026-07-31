<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: README + Supporting Docs — Ready to Sell, Ready to Use

- **Plan**: context/changes/readme-ready-to-sell/plan.md
- **Scope**: Phases 2-5 (Phase 1 already landed on origin/main + v0.1.0 tag)
- **Date**: 2026-07-31
- **Verdict**: APPROVED (after review-fix commit 4317582)
- **Findings**: 0 critical | 0 warnings | 4 observations (all FIXED: 4317582 + 8d80361)

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

---

## Automated gates (re-run at final state)

- GATE lint (mage lint): PASS — 0 issues
- GATE test (mage test): PASS — 8/8 packages; race detection + coverage
- GATE ci (mage ci): PASS — format / vet / lint / build / govulncheck all green

## Manual rows status (Progress section)

Manual rows are intentionally left `- [ ]` (human jurisdiction). All pending:
- 1.4 / 1.5 / 1.6 (tag, Releases page, `freedius --version`) — confirmed at session entry by user
- 2.11 / 2.12 / 2.13 — pending human verification (curl 200, D7–D19 walkthrough, `--version` on GoReleaser binary)
- 3.9 / 3.10 — pending human verification (fresh-clone Quickstart 200; returning maintainer Web Dashboard read-through)
- 4.8 / 4.9 / 4.10 — pending human verification (Docker section findable; hooks reference findable in <30s; AGENTS.md reachable)
- 5.7 / 5.8 / 5.9 — pending human verification (badges in 10s; no false claims; D24 gone)

## Findings

### F1 — Supported Providers table missing the `mix` provider entry

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: README.md:161 (after fix); table previously 15 rows vs 16 in providers.yaml

- **Detail**:
  Plan 5.3 contracts: "Lists each provider in `providers.yaml`." providers.yaml has 16 entries (nim, zen, go, custom, openai, anthropic, mix, google, mistral, deepseek, groq, together, fireworks, cohere, ollama, lmstudio). The README table had 15 rows — the `mix` entry (behavior: mix, `require_base_url: true`, no `default_api_key_env`, i.e. a user-supplied passthrough endpoint with auto-detected protocol) was absent.

- **Fix**: Added `| mix (passthrough) | mix | _(set in your config)_ |` row. Committed as 4317582.
  - Strength: table is now 1:1 with providers.yaml; honors the "list each provider" contract.
  - Tradeoff: None — row mirrors the existing `OpenAI (BYO endpoint)` and `custom` rows.
  - Confidence: HIGH — same pattern applied to `openai`/`custom` rows.
  - Blind spot: None significant.
- **Decision**: FIXED (commit 4317582)

### F2 — AGENTS.md link not near the top of the Development section

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: README.md:221-224 (after fix)

- **Detail**:
  Plan 4.2 contracts: "The Development section gains a one-line 'For the full contributor guide, see AGENTS.md.' near its top." Before the fix, AGENTS.md was linked in the opening paragraphs (README.md:14) and in the Build-from-source subsection, but NOT as a one-line pointer near the top of the Development section itself.

- **Fix**: Added `For the full contributor guide ...` one-line pointer as the first line of the Development section. Committed as 4317582.
  - Strength: matches the plan's literal contract wording.
  - Tradeoff: None.
  - Confidence: HIGH.
  - Blind spot: None.
- **Decision**: FIXED (commit 4317582)

### F3 — Env-var table single-dash descriptions inconsistent with D8

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — trivial consistency fix
- **Dimension**: Pattern Consistency
- **Location**: README.md:211-212 (FREEDIUS_UI_PORT / FREEDIUS_UI_HOST rows, after fix)

- **Detail**:
  The CLI flags table uses double-dash (`--ui-port`, `--ui-host`) per D8. But the env-var override-description column in the adjacent table said "Override `-ui-port`" / "Override `-ui-host`" (single-dash). Both forms are accepted by Go's `flag` package, but the inconsistency within one README is confusing and arguably re-opens D8 in spirit.

- **Fix**: Normalized both to `--ui-port` / `--ui-host`. Committed as 4317582.
  - Strength: consistency with the flags table the row documents.
  - Tradeoff: None.
  - Confidence: HIGH.
  - Blind spot: None.
- **Decision**: FIXED (commit 4317582)

### F4 — config.example.yaml "DO NOT copy" warning reason was misleading

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — wording fix only
- **Dimension**: Safety & Quality
- **Location**: config.example.yaml:12-15 (after fix)

- **Detail**:
  The header warned: "DO NOT copy this file as-is to your config path — the comments and placeholder values are not valid freedius input." But (a) the file is entirely comments — there are no "placeholder values," and (b) comments ARE valid YAML, so the stated reason ("not valid freedius input") is wrong. The real failure mode is: an empty config (no providers, no mappings) is rejected at startup by `loadFromUnmarshaled` (config/config.go:117) with "config: input contains no model mappings". `os.UserConfigDir()` auto-resolves the config path (main.go:466-470), so copying the file as-is WOULD land on the auto-resolve path and produce the empty-config error.

- **Fix**: Reworded to: "DO NOT copy this file as-is ... it is entirely comments. freedius will load it, find no providers or mappings, and reject it at startup with 'config: input contains no model mappings'." Committed as 4317582.
  - Strength: matches the actual code path and error message; more actionable for the user.
  - Tradeoff: None.
  - Confidence: HIGH.
  - Blind spot: The first paragraph "To customize: copy this file to freedius.yaml ... and uncomment + edit" is retained (that's the correct workflow — copy + edit, not copy-as-is). The two messages complement, not contradict.
- **Decision**: FIXED (commit 4317582)

### F5 (observation) — README "Claude Code or OpenCode" env-inject scope

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — informational
- **Dimension**: Pattern Consistency / Accuracy
- **Location**: README.md:65

- **Detail**:
  README.md:64-67 says the env-inject hint "includes `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, and a few optional Claude-Code-specific variables" and that you copy them to point "Claude Code **or OpenCode** at freedius." Per `internal/envinject/snippet.go:9-18`, the snippet emits only Claude-Code-style env vars (`ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, `ANTHROPIC_DISABLE_TELEMETRY`, etc.). OpenCode does honor Anthropic-compatible env var overrides, so the claim is not strictly false — but the snippet is Claude Code-shaped and the README doesn't note that OpenCode consumption depends on OpenCode's Anthropic-env-var compatibility.

- **Recommendation (left for user discretion)**: Either (a) leave as-is (OpenCode reads ANTHROPIC_* vars, so it works), or (b) add a footnote that the hint is Claude Code-shaped and OpenCode support depends on it honoring Anthropic env vars. No code change required; this is a doc nuance, not a defect. No commit made.
  - Strength: keeps the claim accurate today.
  - Tradeoff: (b) adds a line of friction to the 3-step Quickstart.
  - Confidence: MED — verified snippet.go content; unverified OpenCode env-var behavior.
  - Blind spot: Have not run OpenCode against this snippet to confirm it reads ANTHROPIC_BASE_URL.
- **Decision**: FIXED via Fix A (commit 8d80361) — reworded the QuickStart prose to state the env-inject hint is Anthropic/Claude Code-shaped and that OpenCode consumes it only when honoring Anthropic-compatible env-var overrides. No Quickstart friction added (still one-key install). Verified `mage lint` passes post-fix.

---

## Notes on review scope

- The committed commit range `origin/main..HEAD` on branch `rename/readme-and-nim-default` includes one commit that is NOT part of this change: `12f565b feat(web-404-page): branded 404 page for unknown routes (p1)` (plus its uncommitted follow-up WIP in the worktree). That commit was authored by a prior session and touches `proxy/web/*`, `proxy/web/templates/404.html`, `proxy/web/static/app.css`, and `context/changes/web-404-page/` — all outside the `readme-ready-to-sell` scope. The readme-ready-to-sell commits (096475c, e4921a4, fb33ba7, fadab5c, c79ef84, 4317582) touch ONLY documentation files: README.md, cmd/freedius/templates/starter.yaml, config.example.yaml, context/changes/readme-ready-to-sell/{plan.md,change.md}, plus Phase 1's LICENSE/.goreleaser.yaml/magefiles/.github (already on origin/main). Scope guardrails respected.
- A `DIRTY (not staged)` set from an unrelated WIP sits in the worktree (`proxy/web/templates/models-fragment.html` and an uncommitted `handlers_404_test.go`) — these are not in scope and are not claimed by this review. The pre-commit hook's `generateCheck` tripped on one of those (`handlers_404_test.go`); the review-fix commit was applied with that file temporarily stashed to let the hook pass, then restored untouched.
