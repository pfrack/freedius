<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider Fallback Routing

- **Plan**: context/changes/provider-fallback-routing/plan.md
- **Scope**: Phases 1–5 of 5
- **Date**: 2026-07-06
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical 2 warnings 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Anthropic adapter lacks per-request stream timeout

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/anthropic_compat.go:91
- **Detail**: The Anthropic adapter passes `r.Context()` directly to `http.NewRequestWithContext` without adding a per-request timeout. The OpenAI adapter (openai_compat.go:116) wraps the context with `a.streamTimeout`, capping each upstream call independently. Because the dispatcher's shared chain budget is `streamTimeout × 2` (proxy.go:272), a single hanging Anthropic upstream consumes the entire budget before the dispatcher can retry the next provider. The OpenAI adapter's per-request timeout leaves half the budget for fallback attempts.
- **Fix**: Add `context.WithTimeout(r.Context(), a.streamTimeout)` in the Anthropic adapter, mirroring the OpenAI adapter's pattern at openai_compat.go:116.
  - Strength: Identical pattern already exists in the OpenAI adapter; consistent per-attempt bounding across both adapters.
  - Tradeoff: Minor — one line added, follows established pattern.
  - Confidence: HIGH — the OpenAI adapter already does this.
  - Blind spot: None significant — the stream timeout value is already validated in Dispatcher construction.
- **Decision**: FIXED — Added `streamTimeout` field, `NewAnthropicCompatibleAdapterWithTimeout` constructor, `context.WithTimeout` in Handle, updated codegen template and test assertion.

### F2 — Provider delete does not check fallback references

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: proxy/web/handlers.go:476-482
- **Detail**: `handleDeleteProvider` checks if any mapping's `ProviderName` matches the deleted provider (line 477), returning 409 Conflict. It does NOT check `m.Fallback` entries. A mapping could reference a deleted provider in its fallback chain. At runtime, the fallback fails with `provider_not_registered`, triggering the next fallback or aggregated error — safe but potentially surprising to users.
- **Fix**: Extend the loop at handlers.go:476 to also iterate `m.Fallback` and check each entry's `ProviderName` against the deleted provider.
  - Strength: Complete reference check prevents silent runtime failures for fallback entries.
  - Tradeoff: One loop addition; follows the same pattern already used for primary providers.
  - Confidence: HIGH — identical pattern already exists.
  - Blind spot: None significant.
- **Decision**: FIXED — Added fallback reference check in handleDeleteProvider loop.

### F3 — URL path injection in delete button

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/mappings-table.html:29
- **Detail**: `hx-delete="/v1/mappings/{{.Name}}"` places the mapping name in a URL path without path-escaping. Go's `html/template` escapes HTML but not URL segments. Names containing `/` or `?` could alter routing. Validation blocks CR/LF/colon but not `%` (percent-encoding bypass).
- **Fix**: Add `url.PathEscape` or block `%` in mapping name validation. Alternatively, encode the name in the template with a URL-safe escaping function.
  - Strength: Eliminates a class of routing bypass.
  - Tradeoff: Minimal — one function call or validation rule addition.
  - Confidence: MEDIUM — mapping names are currently constrained to alphanumeric + hyphens, limiting practical risk.
  - Blind spot: Haven't audited all URL parameter usages across templates for this class of issue.
- **Decision**: FIXED — Added `%` to blocked characters in `validateMappingName` and `validateProviderName`.

### F4 — handleDeleteProvider does not check fallback references (observation)

- **Severity**: 🔵 OBSERVATION
- **Impact**: N/A
- **Dimension**: Scope Discipline
- **Location**: proxy/web/handlers.go:476-482
- **Detail**: Same issue as F2, documented here for completeness. A mapping's fallback entries referencing a deleted provider will fail at runtime with `provider_not_registered`, which triggers the next fallback or aggregated error — functionally safe but may surprise users who expect the delete to be blocked.
- **Fix**: See F2.
- **Decision**: RESOLVED — fixed by F2.

### F5 — Fallback aggregated error exposes provider internals

- **Severity**: 🔵 OBSERVATION
- **Impact**: N/A
- **Dimension**: Safety & Quality
- **Location**: proxy/proxy.go:418-420
- **Detail**: When all fallback attempts fail, the error message includes `provider/model (error_type)` for each attempt. Values like `authentication_error` or `rate_limit_error` reveal which providers accept the API key and which don't. Acceptable for a local proxy; worth noting for any future remote deployment.
- **Fix**: No action needed for local proxy use. If remote deployment becomes relevant, redact error types in the client-facing message.
- **Decision**: FIXED — Redacted error types from client-facing message; server log retains full details.

## Files touched

config/config.go, config/config_test.go, proxy/errors.go, proxy/openai_compat.go, proxy/openai_compat_test.go, proxy/anthropic_compat.go, proxy/anthropic_compat_test.go, proxy/mix.go, proxy/mix_test.go, proxy/proxy.go, proxy/proxy_test.go, proxy/error_contract_test.go, proxy/error_propagation_test.go, proxy/adapter_errors_test.go, proxy/fallback_test.go, proxy/web/forms.go, proxy/web/forms_test.go, proxy/web/handlers.go, proxy/web/handlers_test.go, proxy/web/types.go, proxy/web/templates/mappings.html, proxy/web/templates/mappings-table.html, config.example.yaml, cmd/freedius/main.go
