# Provider-and-Mapping (S-02) — Plan Brief

> Full plan: `context/changes/provider-and-mapping/plan.md`
> Research: `context/changes/provider-and-mapping/research.md`

## What & Why

Replace freedius's exact-match model routing with family-aware mapping: the user writes five lines (`opus` → go, `sonnet` → go, `haiku` → nim, `auto` → nim, `default` → nim) and freedius routes every Claude Code model name correctly. Add a two-tier provider model where preset providers (nim/zen/go) ship with in-binary defaults and agnostic compat providers (openai/anthropic) let users point at any compatible endpoint. Folded into one PR with the S-01 foundation (Provider interface, Registry, NIM+custom adapters) that the new compat adapters build on.

## Starting Point

The codebase is at the F-01 baseline: `config/config.go:13-20` has a `Config{Models}` with `Model{Provider, Model}` only — no `BaseURL`, no `APIKeyEnv`, no `Mappings` field. `proxy/proxy.go:86-90` returns 501 with a `X-Freedius-Matched-*` header. `main.go:82` calls `proxy.NewDispatcher(cfg, logger)` (2-arg). The git log's `4d88ef1 First call routed (#2)` is misleading — the S-01 plan at `context/changes/first-call-routed/plan.md` describes the Provider interface, Registry, NIM+custom adapters, and `(*Config).UsesProvider` method, but none of it is on disk. S-02 therefore folds the S-01 foundation into Phase 1.

## Desired End State

A user with a five-line `mappings:` config gets every Claude Code request routed to the right provider: `claude-opus-4-1` → `go`/deepseek-v4-pro, `claude-sonnet-4-6` → `go`/deepseek-v4-flash, `claude-haiku-3-5` → `nim`/step-3.5, `auto` → `nim`/step-3.5, anything else → `nim`/step-3.5. `provider: custom` is accepted and rewritten to `provider: anthropic` (the S-01 alias path). `provider: nim` works with no `base_url` or `api_key_env` in the config (in-binary defaults fill them in). `provider: openai` and `provider: anthropic` are new compat providers that the user points at any compatible endpoint. Missing env vars fail at startup with a clear actionable message. The S-01 power-user path (`models:` exact match) continues to work and always wins over family match.

## Key Decisions Made

| Decision                       | Choice            | Why (1 sentence)  | Source           |
| ------------------------------ | ----------------- | ----------------- | ---------------- |
| S-01 dependency                | Fold S-01 work into S-02 plan | S-02's compat adapters depend on the S-01 Provider interface; shipping S-01 separately would require a half-shipped S-02 | Plan |
| Phase structure                | One slice, three phases (foundation / mappings block / family patterns) | Each phase has a testable, complete user-facing behavior; S-01 work fits naturally into Phase 1 | Plan |
| `custom` → `anthropic` alias   | Resolved at config load (`applyDefaults`) | Single source of truth — dispatcher and registry have no knowledge of `custom` as a name | Plan |
| `(*Config).UsesProvider` owner | S-02 adds it (single source of truth) | Eager env-var check loop needs it; one method, one test, no S-01/S-02 land-order coupling | Plan |
| `mappings:` Go struct shape    | Reuse `config.Model` | Zero new types, one validation code path, single source for the (provider, model, base_url, api_key_env) tuple | Plan |
| `mappings:` validation rules   | Same as `models:` | One mental model, one code path, matches F-01 review F7 single-source principle | Plan |
| Both blocks rule               | At least one of `models:` / `mappings:` required | Symmetric, allows pure-mappings configs, allows pure-models configs (S-01 path), allows mixed | Plan |
| Eager env-var check owner      | S-02 owns it | Misconfigurations surface at startup, not on first request; matches S-01 design intent | Plan |
| Compat adapter factoring       | `AnthropicCompatibleAdapter` + `OpenAICompatibleAdapter` as canonical; `CustomAdapter` and `NIMAdapter` as thin wrappers | DRY at the adapter level, makes S-03's `ZenAdapter`/`GoAdapter` multi-format routers easy to write | Research |
| Family pattern priority        | Fixed by code order in `knownFamilies` slice, not YAML order | Predictable routing beats flexible-but-confusing routing | Research |
| `default:` family semantics    | Opt-in by inclusion in user's `mappings:` block | User controls the catch-all; no `default:` → unmatched models 404 | Research |
| Lookup chain                   | `models` exact match → `mappings` literal key → `mappings` family match → 404 | S-01 power-user path wins; `mappings` literal key is the explicit override; family match is the convenience | Research |
| In-binary defaults shape       | `config/defaults.go` map + `(*Config).applyDefaults()` method | Single source of truth; runs after strict YAML parse, before validation; one merge-at-load approach (Option B) | Research |

## Scope

**In scope:**
- New `mappings:` config block (family-name → Model mapping)
- Extended `KnownProviders` (`{nim, zen, go, custom, openai, anthropic}`)
- In-binary defaults for preset providers (nim, zen, go)
- `(*Config).applyDefaults()` method (alias rewrite + defaults merge)
- `(*Config).UsesProvider(name)` method
- "At least one of `models:` / `mappings:`" rule
- Eager startup env-var check loop in `main.go`
- Hardcoded family pattern table (`opus`/`sonnet`/`haiku`/`auto`/`default`)
- Family-pattern lookup in the dispatcher with code-order priority
- Two new compat adapter files (`proxy/openai_compat.go`, `proxy/anthropic_compat.go`)
- S-01 foundation work (folded in): `Provider` interface, `Registry`, NIM+custom adapters, translation package, error helpers
- `config.example.yaml` updated to demonstrate the new schema
- `test-manual.sh` updated for the new schema

**Out of scope:**
- Zen/Go multi-format adapter support — `provider: zen` and `provider: go` return 500 "provider not registered" in S-02 (S-03's work)
- OpenAI Responses / Google Generative AI formats — only Anthropic Messages and OpenAI Chat Completions
- Hot-reload of model lists, `freedius init` command, auto-injection of Claude Code env vars — S-04
- Family name customization (no user-defined families; the 5 names are hardcoded)
- `base_url` defaults for compat providers (openai/anthropic require user-supplied URLs)
- Pattern-based `base_url` per provider, top-level `providers:` block, lazy env-var re-read
- Total upstream-call timeout (wall-clock bounded only by `r.Context()`)
- Request-body logging, metrics endpoint, pprof, Windows support

## Architecture / Approach

Three small packages and one entry point, extended from the F-01 baseline. Config is loaded once at startup into an immutable struct (with in-binary defaults merged in by `applyDefaults`); the dispatch handler closes over it. The `Provider` interface is the single seam: it hides the asymmetry between Anthropic passthrough (`httputil.ReverseProxy`) and OpenAI translation (`http.Client` + stateful SSE translator) from the dispatcher. Pure translation functions live in `proxy/translate/` with no I/O. Family patterns are hardcoded in `proxy/families.go`. The dispatcher's lookup chain is `models` exact match → `mappings` literal key → `mappings` family match (in `knownFamilies` order) → 404.

```
                           ┌─────────────────┐
   Claude Code ──POST──▶   │  net/http       │
   (claude-opus-4-1)       │  ServeMux       │
                           │  (catch-all /)  │
                           └────────┬────────┘
                                    │
                                    ▼
                           ┌─────────────────┐
                           │  Dispatcher     │
                           │  (proxy/)       │
                           │  - parse body   │
                           │  - lookup chain │
                           │    (models →    │
                           │     mappings)   │
                           └────────┬────────┘
                                    │ delegates to
                                    ▼
                           ┌─────────────────┐         ┌─────────────────┐
                           │  Provider       │  uses   │  Registry       │
                           │  (interface)    │◀────────│  (proxy/)       │
                           └────────┬────────┘         │  nim, custom,   │
                                    │                  │  openai,        │
                                    │ implements       │  anthropic      │
                                    ▼                  └─────────────────┘
                           ┌─────────────────┐
                           │  AnthropicCompat │
                           │  OpenAICompat   │
                           │  NIM/Custom     │
                           │  (wrappers)     │
                           └────────┬────────┘
                                    │ calls
                                    ▼
                           ┌─────────────────┐
                           │  translate pkg  │
                           │  (proxy/translate)
                           │  pure bytes-in  │
                           │  bytes-out      │
                           └─────────────────┘
                                    ▲
                           ┌────────┴────────┐
                           │  freedius.yaml  │
                           │  models: +      │
                           │  mappings:      │
                           └─────────────────┘
```

## Phases at a Glance

| Phase     | What it delivers       | Key risk                  |
| --------- | ---------------------- | ------------------------- |
| 1. Schema + provider foundation (S-01 work + S-02 schema/defaults/alias/compat) | Extended config schema (`Model.BaseURL`/`APIKeyEnv`, `Config.Mappings`, 6-name `KnownProviders`); in-binary defaults + `applyDefaults`; `Provider` interface + `Registry`; `AnthropicCompatibleAdapter` + `OpenAICompatibleAdapter`; S-01 NIM/custom adapters as wrappers; dispatcher consults Registry; eager env-var check; `config.example.yaml` updated | Phase 1 is large (~60% of slice effort); S-01 work folded in; risk of merge conflicts with S-03 prep |
| 2. `mappings:` block routing (literal key lookup) | Dispatcher consults `mappings:` after `models:` exact match; per-entry validation for `mappings:`; "at least one of models/mappings" rule; `models:` always wins | Easy to get the precedence wrong (must verify `models:` exact match wins over `mappings:`) |
| 3. Family patterns + tests | `proxy/families.go` with the 5-pattern table; dispatcher runs family pattern match after literal-key lookup; priority is code-order, not YAML-order; `default:` opt-in; `test-manual.sh` updated; `context/foundation/lessons.md` created | Family pattern regex needs case-insensitive matching; priority resolution is non-obvious; tests must be independent of YAML order |

**Prerequisites:** F-01 (done) and S-01 (folded into Phase 1).
**Estimated effort:** ~6-10 hours of focused work across 3 phases. Phase 1 is the bulk (absorbing S-01 work). Phases 2-3 are smaller follow-ups.

## Open Risks & Assumptions

- **Family pattern priority resolves ambiguity the user may not expect**: a model name like `claude-haiku-sonnet-2024` matches both `haiku` and `sonnet`; `sonnet` wins because it's higher in the `knownFamilies` slice. If a user has a `haiku:` mapping and a `sonnet:` mapping with different targets, they may be surprised that `sonnet` wins for ambiguous model names. The fix is documentation, not code.
- **`default:` opt-in vs. implicit catch-all**: the research decided opt-in (no `default:` → unmatched models 404). This is a design choice that may not be obvious to a user who writes `mappings: opus: { ... }` and expects `claude-unknown-2026` to fall through. The error message ("status: no_match") is the only signal.
- **The `custom` alias rewrite in `applyDefaults` changes error messages**: validation messages that reference the provider name will say `anthropic` even when the user wrote `custom`. The implementer must write tests against the post-rewrite name, not the pre-rewrite name. This is a small but easy-to-miss detail.
- **S-01 work folded into Phase 1 makes Phase 1 large**: the implementer should expect Phase 1 to take 4-6 hours of focused work. The review (F-01 review F7 single-source principle) applies — the `knownProviderDefaults` map, the `KnownProviders` map, and the `knownFamilies` slice are three separate single-sources, and each must be maintained in its own file.
- **In-binary defaults for `nim` and `zen` are time-sensitive**: the `nim` default URL is `https://integrate.api.nvidia.com/v1/chat/completions` (a real, public NIM endpoint); `zen` has no default URL (multi-format gateway; user must specify). If NIM renames their endpoint, the in-binary default goes stale. The fix is a config override, not a code change.
- **SSE footguns are non-obvious** (inherited from S-01 research): `json.NewEncoder.Encode` adds a trailing `\n` that corrupts SSE event framing; `bufio.Scanner` has a 64KB line cap. The implementer MUST use `json.Marshal` and `bufio.Reader.ReadBytes('\n')`. These go in `context/foundation/lessons.md` after Phase 3.

## Success Criteria (Summary)

- `make ci` is green on every push; `govulncheck ./...` reports no new vulnerabilities.
- A five-line `mappings:` config routes every Claude Code model name to the right provider, with tool use, streaming, and multi-turn all working through a real `claude-code` session.
- `provider: custom` in a config is accepted and routes through the `anthropic` adapter (no behavior change for S-01 users).
- `provider: nim` works with no `base_url` or `api_key_env` in the config; `provider: openai`/`anthropic` require user-supplied `base_url` and `api_key_env`.
- Missing env vars fail at startup with a clear actionable message; per-model `api_key_env` overrides are caught.
- The S-01 power-user path (`models:` exact match) continues to work and always wins over family match.
- Coverage: `config` ≥ 90%, `proxy` ≥ 85%, `proxy/translate` ≥ 90%.
