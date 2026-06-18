---
date: 2026-06-18T08:49:27Z
researcher: ai-agent
git_commit: 40b076d61c382de88a9660f261ac31f72f7c9db1
branch: cleanup
repository: freedius
topic: "Provider codegen — go:generate provider boilerplate from providers.yaml"
tags: [research, codebase, config, proxy, code-generation, providers, boilerplate]
status: complete
last_updated: 2026-06-18
last_updated_by: ai-agent
---

# Research: Provider Codegen — go:generate provider boilerplate from providers.yaml

**Date**: 2026-06-18T08:49:27Z
**Researcher**: ai-agent
**Git Commit**: 40b076d
**Branch**: cleanup
**Repository**: freedius

## Research Question

What is the current state of provider metadata in the freedius codebase, and what exact contracts must a code generator preserve to safely replace hand-written boilerplate with a `providers.yaml` → `go generate ./...` pipeline?

## Summary

The codebase has 7 `KnownProviders` (nim, zen, go, custom, openai, anthropic, mix) spread across hand-written maps, a rewrite function with load-bearing ordering, thin wrappers, registry wiring, and env-var checks. All are deterministic boilerplate derivable from 4 metadata dimensions: behavior class (openai/anthropic/mix), defaults (baseURL + APIKeyEnv), rewrites (custom/zen/go → mix), and require-base-url flag.

The plan at `plan.md` is structurally sound but contains several stalenesses from post-S-06 changes:
- `custom→anthropic` is actually `custom→mix` (commit `e771bb3`)
- `proxy/custom.go` was deleted in S-06 — only `proxy/nim.go` is a thin wrapper
- `checkRequiredEnvVars` no longer uses a hardcoded preset list — it iterates models directly
- `zen-go-adapters` folder was archived; reference paths in plan are stale
- `change.md`'s `roadmap_id: S-05` should be S-07 per `roadmap.md`

The research confirms the 3-phase approach is correct, but the codegen contract must capture the exact rewrite ordering (custom→mix BEFORE defaults lookup, zen/go→mix AFTER), the `OriginalProvider` invariant (set once, never overwritten), error-message format contracts (especially the `originalOr(m) adapter (<family>): ...` pattern in phase2_test.go), and the sorted-known-providers string in validator error messages.

## Detailed Findings

### 1. Provider metadata sources (Current State)

All provider metadata lives in four hand-written sources:

| Source | File | Lines | What it encodes |
|---|---|---|---|
| `KnownProviders` | `config/config.go:30-38` | 7 entries | Valid provider names for config validation |
| `knownProviderDefaults` | `config/defaults.go:15-29` | 4 entries | Default base_url + api_key_env per provider |
| `applyEntryDefaults` | `config/defaults.go:50-71` | 22 lines | Rewrite rules + defaults application |
| `base_url` requirement | `config/config.go:154-155` | 1 condition | Providers requiring explicit base_url |
| Registry wiring | `main.go:210-215` | 4 entries | Concrete adapter constructors |
| `checkRequiredEnvVars` | `main.go:277-299` | 23 lines | Env-var existence check via model iteration |

Adding a provider today touches 4-5 of these locations.

### 2. Rewrite ordering in applyEntryDefaults (load-bearing detail)

The exact execution order at `config/defaults.go:50-71`:

1. **Capture OriginalProvider** (line 51-53): if empty, set to current `Provider` — idempotent guard
2. **custom → mix** (line 54-56): top-of-function rewrite, BEFORE defaults lookup
3. **Defaults lookup** (line 57): `knownProviderDefaults[m.Provider]` — uses post-rewrite name
4. **Fill defaults** (line 61-66): only if `BaseURL`/`APIKeyEnv` are empty
5. **zen/go → mix** (line 67-69): bottom-of-function rewrite, AFTER defaults applied

The `custom → mix` rewrite at step 2 MUST stay above the defaults lookup because `knownProviderDefaults` has no entry for `custom` or `mix`. Moving it below line 57 would cause an early return (line 58-60) before the rewrite runs, breaking the `custom` alias.

The `zen/go → mix` rewrite at step 5 stays after the lookup so `zen`/`go` benefit from their `OPENCODE_API_KEY` defaults before being renamed to `mix`.

Per-input trace (initial pass, no pre-set fields):

| Input | OriginalProvider | Provider | BaseURL | APIKeyEnv |
|---|---|---|---|---|
| `nim` | `nim` | `nim` | `https://integrate.api.nvidia.com/v1/chat/completions` | `NVIDIA_NIM_API_KEY` |
| `zen` | `zen` | `mix` | `""` | `OPENCODE_API_KEY` |
| `go` | `go` | `mix` | `""` | `OPENCODE_API_KEY` |
| `custom` | `custom` | `mix` | `""` | `""` (early-return, mix has no defaults) |
| `openai` | `openai` | `openai` | `""` | `""` (early-return) |
| `anthropic` | `anthropic` | `anthropic` | `""` | `ANTHROPIC_API_KEY` |
| `mix` | `mix` | `mix` | `""` | `""` (early-return) |

Tests that enforce this behavior: `config/original_provider_test.go:5-119`, `config/config_test.go:280-326`.

### 3. Adapter architecture

Three core adapters (hand-written, NOT touched by codegen):

| Adapter | Constructor signature | Behavior class |
|---|---|---|
| `OpenAICompatibleAdapter` | `NewOpenAICompatibleAdapter(logger)` / `NewOpenAICompatibleAdapterWithTimeout(logger, timeout)` | OpenAI — translates Anthropic→OpenAI body, streams SSE back |
| `AnthropicCompatibleAdapter` | `NewAnthropicCompatibleAdapter(logger, verboseErrors)` | Anthropic — passthrough via `httputil.ReverseProxy`, sets `x-api-key` + `anthropic-version` |
| `MixAdapter` | `NewMixAdapter(logger, verboseErrors, timeout)` | Routes by `m.Protocol` or URL-suffix (`/v1/messages` → Anthropic, else → OpenAI) |

One thin wrapper (generated by codegen):

| Wrapper | Constructor signature | Delegation |
|---|---|---|
| `NIMAdapter` | `NewNIMAdapter(logger, timeout)` | Wraps `OpenAICompatibleAdapter` with `NoStreamUsage: true` + `preSendHook = sanitizeNIMBody` |

The `Provider` interface at `proxy/provider.go:12-14`:
```go
Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
```

There is **no** `proxy/custom.go` — deleted in commit `e771bb3` (S-06). `custom` rewrites to `mix` at config-load time and uses `MixAdapter` at runtime. Same for `zen` and `go`.

### 4. Registry construction

Current registry wiring at `main.go:210-215` has exactly 4 entries:
```go
"nim": proxy.NewNIMAdapter(logger, streamTimeout),
"openai": proxy.NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout),
"anthropic": proxy.NewAnthropicCompatibleAdapter(logger, verboseErrors),
"mix": proxy.NewMixAdapter(logger, verboseErrors, streamTimeout),
```

`KnownProviders` lists 7 names, but the runtime registry has only 4. The 3 "alias" providers (`custom`, `zen`, `go`) are never looked up because `applyEntryDefaults` rewrites them to `mix` before dispatch.

### 5. checkRequiredEnvVars

The function at `main.go:277-299` iterates `cfg.Models` and `cfg.Mappings` directly — **no hardcoded preset list**. It checks each model's `APIKeyEnv` field (set by `applyEntryDefaults`). Error messages use `originalProviderName(m)` which prefers `OriginalProvider` over `Provider`:

```go
func originalProviderName(m config.Model) string {
    if m.OriginalProvider != "" { return m.OriginalProvider }
    return m.Provider
}
```

Tests at `main_test.go:163-206` enforce that `provider=zen` (not `provider=mix`) appears in env-var error messages, and that `provider=custom` appears when `OriginalProvider` is empty (fallback case).

### 6. Model struct & OriginalProvider lifecycle

The `Model` struct at `config/config.go:19-27` has 7 fields. The `OriginalProvider` field (`yaml:"-"`) is the user-facing name preserved through rewrites. It is:

- **Set** once by `applyEntryDefaults` at line 51-53 (only when empty — idempotent)
- **Read** by `originalOr(m)` in `proxy/proxy.go:246-251`, `originalProviderName(m)` in `main.go:301-306`, and every adapter's error-path format string
- **Never serialized** — `yaml:"-"`

The `Protocol` field was added in S-06 (`e771bb3`) and controls `MixAdapter` routing: explicit `"anthropic"` or `"openai"` wins over URL-sniffing. Validated at `config.go:172-180`.

### 7. Test inventory: what codegen must preserve

#### 7.1 KnownProviders contract

`TestKnownProviders` (`config_test.go:491-501`): asserts exactly 7 entries: `nim, zen, go, custom, openai, anthropic, mix`. The sorted string in `unknown provider` errors is `anthropic, custom, go, mix, nim, openai, zen` (tested at line 106).

#### 7.2 ProviderEnvVar contract

`TestProviderEnvVar` (`config_test.go:503-526`): 8 cases enumerating exact env-var names.

#### 7.3 Rewrite + OriginalProvider contract

`config/original_provider_test.go:5-119`: three tests covering:
- `OriginalProvider` set before rewrite for nim/custom/zen/go/openai
- Idempotency: second call does not overwrite `OriginalProvider`
- End-to-end pipeline: `config.Load → applyDefaults → validate` preserves `OriginalProvider`

#### 7.4 Adapter error-message format (CRITICAL)

`proxy/phase2_test.go:120-219`: the most fragile contract. Adapter error messages must use the format `"%s adapter (%s): %s"` where:
- First `%s` = `originalOr(m)` (e.g., "nim", "custom", "zen", "go")
- Second `%s` = behavior family ("openai-compat" or "anthropic-compat")
- Third `%s` = error detail

Anti-regression check (lines 212-216): must NOT contain bare `"openai adapter:"` or `"anthropic adapter:"` substrings.

#### 7.5 Validation error formats

The `TestLoad` table (`config_test.go:11-474`) asserts 25+ exact error substrings, including `"provider=%s but no base_url"` (post-rewrite name), `"uses unknown provider %q (known: %s)"` (sorted list), `"invalid protocol %q (allowed: anthropic, openai)"`.

#### 7.6 Constructor signatures for NIM and Mix

`proxy/nim_test.go:19-23`: `NewNIMAdapter(logger *slog.Logger, streamTimeout time.Duration) *NIMAdapter`
`proxy/mix_test.go:18-22`: `NewMixAdapter(logger *slog.Logger, verboseErrors bool, streamTimeout time.Duration) *MixAdapter`

These are the only two generated constructors that tests instantiate by type name.

### 8. Build infrastructure & dependencies

| Aspect | Finding |
|---|---|
| Existing `//go:generate` | None — this is the first |
| YAML library | `github.com/goccy/go-yaml v1.19.2` — already in `go.mod`; reuse, no new dep |
| Template/format | `text/template` + `go/format` from stdlib — no new dep |
| `make ci` | `lint → test → build` |
| CI (.github/workflows) | Runs raw `go vet`, `go test`, `go build`, `govulncheck` — does NOT run `make ci` |
| Lint | `golangci-lint` + `staticcheck` via `make lint`; pre-commit hook runs `make lint` |
| Generated-file support | `gci --skip-generated` in Makefile and `.golangci.yaml` — respects `DO NOT EDIT` header |
| Line width | `golines --max-len 120` |
| Go version | `1.26.4` (module), `1.26.1` (CI) |
| `internal/` | Contains `envinject/` package — clean pattern to mimic |

### 9. Format conventions

Contrary to AGENTS.md's `gofumpt` claim, the actual formatter chain is: `gofmt → goimports → golines → gci`. Generated files must be `gofmt`-clean, imports in gci section order, ≤120 cols. The `// Code generated ... DO NOT EDIT.` header causes gci to skip import reordering.

## Code References

- `config/config.go:30-38` — `KnownProviders` map (7 entries, to be generated)
- `config/config.go:154-155` — base_url requirement condition (to become `requireBaseURL` set lookup)
- `config/defaults.go:15-29` — `knownProviderDefaults` map (4 entries with BaseURL/APIKeyEnv)
- `config/defaults.go:50-71` — `applyEntryDefaults` rewrite function (load-bearing ordering)
- `config/defaults.go:33-38` — `ProviderEnvVar` function (stays hand-written, references generated map)
- `proxy/nim.go:15-37` — `NIMAdapter` thin wrapper (38 lines, pure delegation)
- `proxy/mix.go:19-70` — `MixAdapter` (core adapter, hand-written, NOT generated)
- `proxy/provider.go:12-39` — `Provider` interface + `Registry` type (NOT changed by codegen)
- `proxy/proxy.go:246-251` — `originalOr(m)` helper (used by all adapter error messages)
- `main.go:210-215` — registry construction (to become `NewDefaultRegistry`)
- `main.go:277-299` — `checkRequiredEnvVars` (iterates models directly, no hardcoded list)
- `main.go:301-306` — `originalProviderName(m)` helper (prefers `OriginalProvider`)
- `config/original_provider_test.go:5-119` — rewrite + OriginalProvider contract tests
- `config/config_test.go:491-501` — `TestKnownProviders` (asserts 7 entries)
- `config/config_test.go:503-526` — `TestProviderEnvVar` (8 env-var assertions)
- `proxy/phase2_test.go:120-219` — adapter error-message format contract (CRITICAL)
- `proxy/nim_sanitize.go` — `sanitizeNIMBody` pre-send hook (used by generated NIMAdapter)
- `Makefile:47` — `--skip-generated` flag on gci formatter
- `.golangci.yaml:60` — `skip-generated: true` for gci linter

## Architecture Insights

1. **Two-phase rewrite is inherent, not accidental.** The `applyEntryDefaults` function has a structural split: pre-lookup rewrites (`custom→mix`) for providers with no defaults entry, and post-lookup rewrites (`zen→mix`, `go→mix`) for providers that have defaults to apply first. The codegen template must preserve this two-phase structure — it is not an implementation detail but a semantic constraint.

2. **`OriginalProvider` is a relay variable.** It is set once during config loading and read by every downstream component (validation errors, adapter errors, dispatcher headers, env-var checks). The codegen must not break this relay — if `OriginalProvider` stops being set, `provider=mix` would leak into every user-facing error message.

3. **The `KnownProviders` set is a superset of the registry.** `KnownProviders` must accept all 7 names (including `custom`, `zen`, `go` — user-facing aliases), while the runtime registry needs only 4 adapters (`nim`, `openai`, `anthropic`, `mix`). The codegen must distinguish between "config-time validation surface" and "runtime dispatch surface."

4. **`proxy/phase2_test.go` is the hardest contract to satisfy.** Its error-message format assertion (`"%s adapter (%s): %s"` with anti-regression checks for bare adapter names) ties the generated code to exact string formats. Any change to error message templates must be reflected in these tests.

5. **CI does not run `make lint`** — only the pre-commit hook does. The plan's Phase 3 golden-file check (`go generate && git diff --exit-code`) would need to be added to both the Makefile's `ci` target AND `.github/workflows/ci.yml` for CI enforcement.

## Historical Context

The plan-brief and plan.md were written before S-06 (`custom-to-mix-protocol`, commit `e771bb3`, 2026-06-18) was merged. Three stalenesses result:

1. **`custom→anthropic` is stale** — S-06 changed it to `custom→mix`. The current code at `config/defaults.go:54-56` reads `if m.Provider == "custom" { m.Provider = "mix" }`.

2. **`proxy/custom.go` is deleted** — S-06 deleted it. The plan's "Current State Analysis" item 5 lists both `nim.go` and `custom.go` as thin wrappers; only `nim.go` exists.

3. **`context/changes/zen-go-adapters/` is archived** — the plan's References section (line 256) and plan-brief's References (line 64) point to `context/changes/zen-go-adapters/plan.md`, which does not exist. The correct path is `context/archive/zen-go-adapters/plan.md`.

Additional notes:
- `change.md` declares `roadmap_id: S-05` but `roadmap.md` assigns provider-codegen to **S-07** — stale frontmatter
- The `custom` rewrite placement constraint (top of function, before defaults lookup) is documented in `context/archive/custom-to-mix-protocol/plan.md:68` and is load-bearing — the codegen template must replicate it
- `proxy/mix.go` was modified in commit `e771bb3` to add `Protocol` field support — the `MixAdapter`'s routing precedence (Protocol → URL suffix) is the current behavior the codegen's registry wiring must preserve

From `context/foundation/lessons.md`, four rules apply:
- "Embrace Extra Tests" — the generator needs its own `internal/genproviders/main_test.go`
- "Adapter Return Contract" — generated adapters must return `nil` after `WriteHeader`
- "Custom Provider: x-api-key + anthropic-version required" — universal Anthropic auth
- "`custom` → `mix` Rewrite in `applyDefaults`" — tests must use post-rewrite name

## Related Research

- `context/archive/zen-go-adapters/research.md` — introduced `MixAdapter` and the rewrite pattern codegen will generalize (85 KB, prior art for URL-suffix routing and `NoStreamUsage` on OpenAI sub-adapter)
- `context/archive/custom-to-mix-protocol/plan.md` — documents the `custom→mix` rewrite placement constraint at line 68; the load-bearing ordering the codegen must preserve
- `context/archive/provider-and-mapping/research.md` — established `KnownProviders` + `applyEntryDefaults` as the design pattern
- `context/foundation/roadmap.md` — S-07 entry at roadmap line ~140-170, with prerequisites S-05 + S-06

## Open Questions

1. **`ProviderEnvVar` naming convention.** The plan says `ProviderEnvVar` stays hand-written but references the generated `knownProviderDefaults` map. What var name should the generator emit — `knownProviderDefaults` (shadow old) or `knownProviderDefaultsGen` (requires updating the hand-written function)?

2. **`knownProviderDefaults` as map vs. separate data structure.** Currently typed as `map[string]modelDefaults`. The `modelDefaults` type stays hand-written (`config/defaults.go:10-13`). If the generator moves defaults to a different package structure, the type must stay visible.

3. **PresetProviders() — what does it return?** The plan says generated `PresetProviders()` returns provider names with a `default_api_key_env`. Current tests do not assert this function exists. Is it purely a future-proofing addition, or must it be wired into `checkRequiredEnvVars`? If wired, the test `TestCheckRequiredEnvVars_ProviderNotReferenced` (main_test.go:76-92) would break because it relies on the per-model iteration to skip unreferenced providers.

4. **`generate-check` in CI vs. Makefile.** CI runs raw `go` commands, not `make ci`. The plan's Phase 3 golden-file check in the Makefile won't be enforced by CI unless `.github/workflows/ci.yml` is also updated.

5. **`change.md` roadmap_id fix.** Should resolve to S-07 before plan execution.
