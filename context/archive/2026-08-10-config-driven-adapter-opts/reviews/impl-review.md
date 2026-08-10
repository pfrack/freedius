<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Config-Driven Adapter Options

- **Plan**: context/changes/config-driven-adapter-opts/plan.md
- **Scope**: All 4 phases
- **Date**: 2026-08-10
- **Verdict**: APPROVED
- **Findings**: 0 critical, 2 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Transport error handling diverges from sibling adapter

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/openai_compat.go:161–163
- **Detail**: OpenAI adapter returns a plain `fmt.Errorf` on transport errors, which the dispatcher classifies as a generic 529 `overloaded_error` (fallback-eligible). The sibling `anthropic_compat.go:132-138` calls `writeTransportError` (which uses `isPermanentTransportError` to pick 502 vs 529) and returns `nil` — making the response non-fallback-eligible. This means OpenAI-path transport failures trigger fallback retries while Anthropic-path failures do not. This divergence pre-dates this change; the refactor preserved it.
- **Fix**: Either align both adapters to the same contract (both fallback-eligible or both not), or document the divergence with a code comment explaining why. Decision belongs to the broader adapter contract discussion — out of scope for this change.
- **Decision**: FIXED — aligned the Anthropic adapter to be fallback-eligible. Changed `anthropic_compat.go` so transport errors return an `*upstreamError` (502 for permanent, 529 for transient) instead of writing the response directly and returning nil. The Anthropic error format is preserved because the dispatcher writes via `writeAnthropicError` when the fallback chain exhausts. Refactored `writeTransportError` → `logTransportError` (logging only). Full suite: 721 tests pass.

### F2 — Missing API key silently accepted for openai-behavior providers

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/openai_compat.go:79–92
- **Detail**: When `DefaultAPIKeyEnv` is empty, `apiKey` stays empty and no `Authorization` header is set. The sibling `anthropic_compat.go:77-87` returns a `configError` if the env var is empty. This is intentional for keyless local providers (ollama/lmstudio), but a user who forgets to set `default_api_key_env` on a cloud provider sends an unauthenticated request that fails opaquely upstream. This behavior pre-dates this change.
- **Fix**: Add a comment at line 80 clarifying this is intentional for keyless providers. A validation warning for cloud-looking base URLs could be a follow-up.
- **Decision**: SKIPPED — a request-time warning was added then removed. For keyless providers (ollama/lmstudio) the warning fires on every request (false alarm); for cloud providers needing a key, the 401 → fallback → authentication_error path already surfaces the problem to the user. No clean signal exists that isn't already covered by existing fallback error reporting.

### F3 — GenerateProxy hardcodes the 4 registry entries

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: internal/genproviders/main.go:139, 147–152
- **Detail**: `GenerateProxy` ignores the `Spec` parameter and hardcodes the 4 registry entries. Adding a new behavior class requires editing generator source. Acceptable for now since the registry is stable.
- **Fix**: Document this constraint in the generator's package doc. No action needed now.
- **Decision**: FIXED — removed the unused `Spec` parameter from `GenerateProxy()` entirely (updated 5 test call sites). The 4 behavior keys are now documented as stable and wired unconditionally in the function comment.

### F4 — `-out` flag defaults to cwd, not package directory

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: internal/genproviders/main.go:231–238
- **Detail**: When `-out` is empty, the output path defaults to `adapters_gen.go` or `providers_gen.go` in the current working directory. The `//go:generate` directives run from the package dir, so this works — but a future contributor running from the repo root would write to the wrong location.
- **Fix**: Add a comment noting the `-out` default assumes invocation from the package directory.
- **Decision**: FIXED — added a comment at main.go:232 documenting the cwd assumption and when to pass -out explicitly.

### F5 — `p.OpenAI` pointer sharing from providerDefaults

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: config/defaults.go:36–38
- **Detail**: `p.OpenAI = defaults.OpenAI` assigns the same pointer from `providerDefaults` to every user provider lacking OpenAI options. If any code path mutated `p.OpenAI.NoStreamUsage`, it would affect all sharers. Currently the field is only read, never mutated, so this is safe.
- **Fix**: If `OpenAIOptions` is ever made mutable per-request, this would need to be a deep copy. No action needed now.
- **Decision**: FIXED — added a comment at defaults.go:37 noting the shared-pointer safety invariant and the deep-copy condition.

### F6 — Unknown `Protocol` value falls through to URL sniffing

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/mix.go:55–78
- **Detail**: When `Protocol` is set to a value other than `"anthropic"` or `"openai"`, the switch falls through to URL-path sniffing. `validateProvider` only validates `Protocol` for `behavior: mix`, so a `behavior: openai` provider with an invalid protocol silently ignores it.
- **Fix**: Acceptable — the switch default is a reasonable fallback. No action needed.
- **Decision**: FIXED — added an explicit `default:` case to the protocol switch at mix.go:55 with a comment documenting the fallthrough to URL-path sniffing.
