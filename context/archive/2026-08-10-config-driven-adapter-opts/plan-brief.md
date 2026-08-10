# Config-Driven Adapter Options — Plan Brief

> Full plan: `context/changes/config-driven-adapter-opts/plan.md`

## What & Why

The `nim-nous-kilo-defaults` impl-review closed APPROVED but flagged a follow-up:
the generated per-provider adapter wrappers (`GoogleAdapter`, `LmstudioAdapter`,
`OllamaAdapter`, `NIMAdapter`) are **dead code** because `NewDefaultRegistry`
keys the registry by *behavior*, not provider name. As a side effect, the
`no_stream_usage` / `pre_send_hook` flags declared in `providers.yaml` are
**silently ignored** for every openai-behavior provider except `nim`. We make
those options config-driven and per-request, then delete the dead wrappers.

## Starting Point

`config.Provider` has no field for the openai tweaks; `providerDefaults` drops
them on load; and `OpenAICompatibleAdapter.Handle` uses a fixed construction-time
`translateOpts`. The YAML data already exists and is correct — it just never
reaches the request path. `mix` is the only live consumer, via a hardcoded flag
in `mix.go:36`.

## Desired End State

A request to any openai-behavior provider (google/ollama/lmstudio/nim, and any
user-defined provider) now honors `no_stream_usage` and `pre_send_hook` read
from its config at request time. `proxy/adapters_gen.go` contains only the four
behavior adapters — no per-provider wrapper types — and `go generate ./...`
remains the single source-of-truth step, now also propagating the `openai:` block
into embedded defaults.

## Key Decisions Made

| Decision | Choice | Why | Source |
| --- | --- | --- | --- |
| Options source of truth | Config-only (drop code defaults) | Single source = `providers.yaml`; no dual code/config paths | Plan |
| Unknown `pre_send_hook` | Error out | Fail fast on typo; matches existing `configError` style | Plan |
| `nim` registry entry | Collapse to generic openai ctor | Removes `NIMAdapter`; its tweaks now come from `nim` config | Plan |
| `mix` forced flag | Move into `providers.yaml` | Required so `zen`/`go`/`custom`/`mix` don't regress under config-only | Plan |
| Generation mechanism | Keep `go generate ./...` | Codegen stays; only the *output* changes | Plan |

## Scope

**In scope:** add `OpenAI *OpenAIOptions` to `config.Provider`; propagate through
gen + `applyDefaults`; make `Handle` read it per request; add a `preSendHook`
name→fn map; delete per-provider wrapper codegen; re-home `mix`'s flag into YAML;
update/extend tests.

**Out of scope:** `go:embed` of `providers.yaml`; keying the registry by provider
name; changes to anthropic/mix adapter logic beyond the flag move; new providers
or hook functions.

## Architecture / Approach

`providers.yaml` → `go generate ./...` → embedded `providerDefaults`
(including the `openai:` block) + a 4-entry behavior registry (no wrappers).
At request time `OpenAICompatibleAdapter.Handle` reads `provider.OpenAI` to build
`translate.Opts` and resolve the `preSendHook` via a package-level map. `mix`
routes to the same generic openai adapter, now driven by each mix provider's
`OpenAI` config.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Config model | `OpenAI` options survive load + embed; `mix` flag moved to YAML | Struct-comparison tests in `config_test.go` need updating |
| 2. Request path | `Handle` reads `provider.OpenAI` per request; unknown hook errors | `mix` regression if YAML blocks missing |
| 3. Codegen cleanup | Wrappers deleted; `nim` → generic ctor; regen | Forgotten reference to a wrapper type breaks build |
| 4. Tests | NIM/mix tests migrated; new coverage for flags + bad hook | Full 716-test suite must stay green |

**Prerequisites:** existing `providers.yaml` (already declares the flags);
`go generate` toolchain.
**Estimated effort:** ~1 session across 4 tightly-coupled phases (mostly mechanical + test updates).

## Open Risks & Assumptions

- Assumes `mix` upstreams genuinely reject `stream_options` (the prior hardcoded
  flag implies so); re-homing it to YAML preserves that assumption.
- Assumes no production code calls the wrapper constructors directly — verified
  by grep (only `internal/genproviders` references them).
- `sanitizeNIMBody` is the only hook needed; the registry is trivially
  extensible if others appear.

## Success Criteria (Summary)

- `no_stream_usage: true` is honored for google/ollama/lmstudio/nim and any
  user-defined provider at request time.
- `proxy/adapters_gen.go` has no per-provider wrapper types; `go generate` is
  md5-stable.
- `go test ./...`, `mage lint`, `mage govulncheck` all green.
