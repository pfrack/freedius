<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Modernize Default Config: NIM Tiers + Nous + Kilo

- **Plan**: context/changes/nim-nous-kilo-defaults/plan.md
- **Scope**: Phases 1–2 of 3 (Phase 3 pending)
- **Date**: 2026-08-09
- **Verdict**: REJECTED
- **Findings**: 2 critical, 4 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | FAIL |
| Scope Discipline | WARNING |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — Nous base URL does not resolve (NXDOMAIN)

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence / Safety & Quality
- **Location**: providers.yaml:120, config/providers_gen.go:99
- **Detail**: `https://api.nousresearch.com/v1/chat/completions` — `api.nousresearch.com` has no DNS record (`getent hosts` returns nothing; curl fails with connect error 000). The real Nous Research inference API, per portal.nousresearch.com/api-docs, is `https://inference-api.nousresearch.com/v1`; a POST to `https://inference-api.nousresearch.com/v1/chat/completions` returns a proper `401` for a bad key. The plan asserted this URL as "confirmed by user" — the plan was wrong, and the implementation propagated it. Every tier's `nous` fallback is currently a guaranteed transport failure.
- **Fix**: Change `default_base_url` to `https://inference-api.nousresearch.com/v1/chat/completions` in providers.yaml and re-run `go generate ./...`.
  - Strength: Verified live against the Nous catalog (`/v1/models` returns 354 entries) and auth path (401 with an invalid key).
  - Tradeoff: None — one-line change plus regeneration.
  - Confidence: HIGH — DNS + live HTTP evidence on both hostnames.
  - Blind spot: None significant.
- **Decision**: FIXED — providers.yaml points at `https://inference-api.nousresearch.com/v1/chat/completions`; `go generate ./...` re-run, providers_gen.go:100 updated.

### F2 — Nous fallback model `hy3:free` does not exist

- **Severity**: ❌ CRITICAL
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: cmd/freedius/templates/starter.yaml (5 occurrences: opus, sonnet, haiku, default, auto)
- **Detail**: Live probe against the Nous inference API returns `404 Model 'hy3:free' not found`. The catalog exposes it as `tencent/hy3:free` (also `tencent/hy3`, `tencent/hy3-preview`); with that id the same request returns `401` (auth), proving the id resolves. The plan pinned the bare `hy3:free` string from user input without catalog verification — exactly the risk the plan's own Critical Implementation Details warned about.
- **Fix**: Replace all five `model_string: hy3:free` with `tencent/hy3:free`.
  - Strength: Confirmed against `GET https://inference-api.nousresearch.com/v1/models`.
  - Tradeoff: None.
  - Confidence: HIGH — 404 vs 401 discriminates id validity cleanly.
  - Blind spot: None significant.
- **Decision**: FIXED — all five starter.yaml entries now `tencent/hy3:free` (lines 39, 51, 63, 81, 92).

### F3 — NIM fallback model `nvidia/llama-3.1-8b-instruct` is not in the NIM catalog

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: cmd/freedius/templates/starter.yaml (haiku, default, auto fallbacks)
- **Detail**: `GET https://integrate.api.nvidia.com/v1/models` (100 ids) contains `meta/llama-3.1-8b-instruct`, not `nvidia/llama-3.1-8b-instruct`. The other four NIM ids used by the starter (`nemotron-3-ultra-550b-a55b`, `nemotron-3-super-120b-a12b`, `llama-3.3-nemotron-super-49b-v1`, `nemotron-3-nano-30b-a3b`) all verify as present. This one entry silently degrades three tiers' first fallback hop.
- **Fix**: Rename to `meta/llama-3.1-8b-instruct` in the three fallback entries.
  - Strength: Matches the live catalog exactly; keeps the intended "cheapest NIM hop" semantics.
  - Tradeoff: None.
  - Confidence: HIGH — catalog membership check.
  - Blind spot: None significant.
- **Decision**: FIXED — three starter.yaml fallbacks now `meta/llama-3.1-8b-instruct` (lines 61, 79, 90).

### F4 — Unplanned relaxation of the colon ban in model_string validation

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Scope Discipline
- **Location**: config/config.go:311, config/config.go:351, proxy/web/forms.go:216
- **Detail**: The diff loosens two security validators from `\r\n:` to `\r\n` (plus mirrored test changes). This is not in the plan's "Changes Required" and touches an input sanitizer whose stated purpose is preventing header injection into `X-Freedius-Matched-Model` (proxy/proxy.go:279). It was forced by the colon in `hy3:free`. Technically the relaxation is sound — RFC 7230 field-values permit `:`, only CR/LF break framing — and provider/mapping *names* (used in URL paths) still reject `:` and `%`. But it is an undocumented widening of a security check, discovered only because it broke `TestStarterTemplate_ValidConfig` in a shared tree.
- **Fix A ⭐ Recommended**: Keep the relaxation and document it as a plan addendum (new "Changes Required" entry under Phase 2, plus a line in Critical Implementation Details).
  - Strength: The change is correct and required — every realistic vendor free-tier id (`tencent/hy3:free`, `llama3:8b`) contains a colon; reverting would block the whole feature. Code comments already explain the CR/LF-only rationale.
  - Tradeoff: The plan becomes a moving target; the security-validator change ships without having been reviewed at plan time.
  - Confidence: HIGH — colon is legal in HTTP field-values; names still ban it.
  - Blind spot: Have not audited whether `ModelString` reaches any URL path or log-format context where `:` is structurally meaningful (checked proxy.go, count_tokens_local.go, openai_compat.go — all header/body/log-field uses only).
- **Fix B**: Revert the validator change and keep colons banned, encoding fallback ids differently.
  - Strength: Preserves the original scope boundary and the stricter validator.
  - Tradeoff: Makes the Nous/Kilo free-tier ids unrepresentable — kills the feature.
  - Confidence: LOW — no viable alternative encoding exists for vendor ids.
  - Blind spot: None.
- **Decision**: FIXED via Fix A — plan.md gains a Critical Implementation Details bullet on the colon ban plus a Phase 2 "Changes Required #3" addendum and a matching automated success criterion.

### F5 — `no_stream_usage: true` on nous/kilo diverges from sibling openai providers

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Pattern Consistency
- **Location**: providers.yaml:124, providers.yaml:132
- **Detail**: Both new providers set `no_stream_usage: true`, which suppresses `stream_options.include_usage` (proxy/translate/anthropic_openai.go:57) and therefore drops token accounting on streamed responses. In providers.yaml this flag is reserved for upstreams that reject the option — NIM (with `sanitizeNIMBody`), Google, and the two local servers. The comparable hosted OpenAI-shaped providers (deepseek, groq, together, fireworks, mistral, cohere) do not set it. Kilo's gateway documents usage in the final SSE chunk, and Nous advertises plain OpenAI compatibility, so the flag is likely unnecessary here. The plan suggested this shape, so the implementation followed it — the plan is the source of the mismatch.
- **Fix**: Drop `openai: {no_stream_usage: true}` from the `nous` and `kilo` entries, regenerate, and re-add only if a live streamed request rejects `stream_options`.
  - Strength: Restores usage accounting and matches the majority pattern for hosted openai-class providers.
  - Tradeoff: If either upstream rejects `stream_options.include_usage`, streaming breaks until the flag is restored — needs one live streamed probe per provider to confirm.
  - Confidence: MEDIUM — Kilo's API reference documents a `usage` field in the final chunk; Nous is untested with a real key.
  - Blind spot: No authenticated streaming request was made to either provider during this review.
- **Decision**: FIXED — `openai: {no_stream_usage: true}` removed from both entries; `go generate ./...` re-run. Side-effect discovered: this also deleted the generated `NousAdapter`/`KiloAdapter` wrappers (−48 lines in proxy/adapters_gen.go), which were **dead code** — `NewDefaultRegistry` (adapters_gen.go:122) keys the registry by *behavior* (`nim`/`openai`/`anthropic`/`mix`) and `proxy.go:324` looks up `p.Behavior`, so a per-provider-name adapter is never reachable. The flag was therefore a no-op for nous/kilo either way. The same dead-wrapper issue pre-exists for `GoogleAdapter`, `OllamaAdapter`, `LmstudioAdapter` — out of scope here, worth a follow-up. Gates re-verified: build clean, 716 tests pass, lint 0 issues.

### F6 — README provider table and example config not updated

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: README.md:160-183 (Supported Providers table), README.md:110-120 (example config)
- **Detail**: Neither `nous` nor `kilo` appears in the Supported Providers table, and the README's example/quickstart config still shows the old NIM-only shape with retired `deepseek-ai/deepseek-v4-*` ids. Plan Phase 3 manual criterion 3.3 ("README Quickstart still accurate") is therefore not yet satisfiable. Phase 3 is legitimately still pending, so this is a heads-up rather than drift — but the stale `deepseek-v4-*` ids in the example predate this change and are now inconsistent with the shipped starter.
- **Fix**: In Phase 3, add `Nous Research | openai | NOUS_API_KEY` and `Kilo | openai | KILO_API_KEY` rows and refresh the example-config model ids to match the new starter.
- **Decision**: FIXED — README now documents NIM-primary + per-tier fallback (optional Nous/Kilo keys), the example config uses live NIM ids, the fallback-chain example shows the real nim→nous→kilo shape, and the provider table gains both rows. No `deepseek-v4-*` ids remain. Satisfies Phase 3 criterion 3.3 ahead of schedule.

### F7 — Phase 2 marked complete in Progress but left uncommitted

- **Severity**: 📋 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: context/changes/nim-nous-kilo-defaults/plan.md:363-365
- **Detail**: Progress rows 2.1–2.3 are `[x]` with no commit ref, while rows 1.1–1.3 carry `e715c54`. All Phase 2 work (starter.yaml, config.go, forms.go, the new ordering test) is still in the working tree alongside unrelated changes from `logs-ui-live-tail`. The gates themselves do verify green in this tree: `go build ./...` clean, `go test ./...` 716 passed, `mage lint` 0 issues, `mage govulncheck` no vulnerabilities, and `go generate ./...` is a no-op — so Phase 3's automated criteria (3.1, 3.2) already hold. The risk is bookkeeping, not correctness: a shared dirty tree makes per-phase attribution impossible.
- **Fix**: Commit Phase 2 separately and backfill the commit SHA onto rows 2.1–2.3.
- **Decision**: PENDING

### F8 — Stray file with an embedded newline in the repo root

- **Severity**: 📋 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: repo root — untracked file literally named `fail\nsafety is kept: the snapshot only happens once a real .bak is found.\n\nResolves impl-review F3 (warning).`
- **Detail**: A 147-byte junk file created by a mis-quoted `git commit -m` during the earlier `claude-settings-injection` work. Not produced by this change, but it sits untracked in the working tree and will trip up glob-based tooling. `.gitignore` does not cover it.
- **Fix**: Delete it (`rm -- "$(printf 'fail\\nsafety...')"` or via a `find -inum` delete).
- **Decision**: PENDING
