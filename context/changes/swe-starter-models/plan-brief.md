# SWE Provider Registry Expansion — Plan Brief

> Full plan: `context/changes/swe-starter-models/plan.md`
> Frame brief: `context/changes/swe-starter-models/frame.md`

## What & Why

freedius should let users route to strong SWE/coding models from Minimax and
Xiaomi (DeepSeek is already registered). The starter template, however, should
keep using only `nim`/`kilo`/`nous` models — providers are a separate concern
from the starter's built-in tiers. The empty-mappings guard in
`ensureDefaultMapping` is confirmed correct and is left unchanged (a
zero-mapping config yields a clean `no_match`, not a 500).

## Starting Point

`providers.yaml` is the single source of truth for the provider registry; it
already had `deepseek` but lacked `minimax` and `xiaomi`. The starter template
(`templates/starter.yaml`, embedded at main.go:48) defines opus/sonnet/haiku/
default tiers using only `nim`/`nous`/`kilo`. Adding a provider is one YAML
entry plus `go generate ./...`, which regenerates `providers_gen.go` and
`adapters_gen.go`.

## Desired End State

`minimax` and `xiaomi` are first-class providers available to any freedius
config, and the starter template is unchanged in behavior (still nim/kilo/
nous only). A config with no mappings still returns a structured `no_match`
error rather than crashing. `mage test` is green.

## Key Decisions Made

| Decision                | Choice                                       | Why (1 sentence)                                                              | Source |
| ----------------------- | -------------------------------------------- | ----------------------------------------------------------------------------- | ------ |
| Starter model sources   | nim / kilo / nous only                      | User requirement: starter models come from nim/kilo; providers are separate | Frame  |
| Provider additions       | minimax + xiaomi (deepseek already present) | Expands SWE model coverage without touching starter behavior                 | Frame  |
| Xiaomi base URL         | `token-plan-sgp.xiaomimimo.com/v1/chat/completions` | User-provided authoritative endpoint                                     | Plan   |
| Empty-mappings guard    | Leave unchanged                              | Returns clean `no_match`, not a 500; injecting a default with no model would break | Frame  |
| Dedicated `coder` tier   | Not added                                    | Out of scope; starter stays as-is                                            | Frame  |

## Scope

**In scope:** register `minimax` + `xiaomi` in `providers.yaml`; regenerate
tables; update `TestProviderDefaults`.

**Out of scope:** injecting minimax/deepseek/xiaomi into the starter tiers;
changing `ensureDefaultMapping`; adding a `coder` mapping; changing fallback
ordering.

## Architecture / Approach

Pure registry/data change. One entry per new provider in `providers.yaml`, then
`go generate ./...` refreshes the generated `providerDefaults` map and adapter
switch. No dispatcher, adapter, or starter logic is modified. The OpenAI
adapter POSTs directly to `provider.DefaultBaseURL` (openai_compat.go:142), so
each `default_base_url` is the full `…/chat/completions` endpoint.

## Phases at a Glance

| Phase | What it delivers                          | Key risk                       |
| ----- | ----------------------------------------- | ------------------------------ |
| 1     | minimax + xiaomi registered + test updated | Wrong Xiaomi/Minimax base URL  |
| 2     | Confirm starter scope + guard behavior    | None (verification only)       |

**Prerequisites:** Xiaomi endpoint + API key; Minimax API key.
**Estimated effort:** ~1 short session (already implemented; plan documents it).

## Open Risks & Assumptions

- Minimax model identifier `MiniMax-M3` and base `api.minimax.com` are best-known
  values; verify against live API before production reliance.
- Xiaomi's exact model string is user-supplied at request time (the provider is
  registered; the mapping's `model_string` is whatever the user configures).

## Success Criteria (Summary)

- `minimax` and `xiaomi` load as registered providers and route openai-style traffic.
- Starter template unchanged; still nim/kilo/nous only.
- `mage test` green; zero-mapping config returns `no_match`, not a 500.
