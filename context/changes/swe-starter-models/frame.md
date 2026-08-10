# Frame Brief: Default mapping copy + SWE provider coverage

> Framing step before /10x-plan. Captures what is *actually* at issue, separated
> from what was initially assumed.

## Reported Observation

1. User is unsure which mapping becomes the catch-all `default` (and which mapping
   gets *copied* into `default`) when one is not explicitly defined.
2. User wants the out-of-box config to cover stronger SWE / coding models across
   more providers (named: DeepSeek, Minimax, "xiaomi sgp").
3. User suspects `ensureDefaultMapping`'s early `return` on `len(c.Mappings) == 0`
   (config.go:189) is a defect — "when mapping length is 0 shouldn't we then have
   some default mapper. Is in fact very strange state."

## Initial Framing (preserved)

- **User's stated cause or approach**: the empty-mappings guard is a bug that can
  leave the system with no usable default mapper; the fix is to also add DeepSeek /
  Minimax / Xiaomi providers and richer SWE model mappings.
- **User's proposed direction**: extend the starter/default mappers with SWE models
  from Minimax, DeepSeek, Xiaomi; add those providers to the registry.
- **Pre-dispatch narrowing**: user clarified "starter mapping with better models on
  nim, kilo etc" — i.e. enrich the *starter template*, and additionally register
  deepseek/minimax/xiaomi as providers.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Dispatcher catch-all behavior** — how `default` is selected / what happens when
   absent. (Answers Q1.)
2. **`ensureDefaultMapping` empty guard** — whether returning early on empty mappings
   is correct. (Answers Q3.)
3. **Starter template model coverage** — the real driver: out-of-box SWE/coding
   models are limited to NIM/nous/kilo. (Answers Q2.)
4. **Global provider registry scope** — whether Minimax/Xiaomi belong in
   `providers.yaml` (generated table) or only in the starter. (Scoping of Q2.)

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Q1: `default` copies the alphabetically-first mapping when absent | config.go:186-202 — if no explicit `default`, copies `ProviderName`/`ModelString`/`AddedAt` of `keys[0]` after `sort.Strings`. Starter defines `default` explicitly, so nothing copied there. | CONFIRMED |
| Q3: empty-mappings guard is a defect | config.go:189 returns early; BuildMatchers (config.go:232) omits catch-all when no `default`; resolveMapping returns `mapped=false` (proxy.go:147); dispatcher returns clean `no_match` JSON (proxy.go:231-239), NOT a 500. Injecting a `default` with no model to copy would 500 on every request. | NOT A DEFECT — guard is correct |
| Q2: starter lacks strong SWE models across providers | starter.yaml only references nim/nous/kilo; deepseek provider exists in registry but unused in starter; minimax/xiaomi absent entirely. | CONFIRMED — real enhancement |
| Q2-scoping: minimax/xiaomi belong in registry | providers.yaml is the generated single source of truth; adding them mirrors existing entries (deepseek, groq, …). | CONFIRMED — low risk |

## Narrowing Signals

- Q3 resolved by reading the actual no-match path: empty mappings → clean error, no
  crash. The "strange state" is a *degenerate config*, not a code defect.
- Q1 resolved: copy source is alphabetical-first key, not a fixed "primary" tier.
- Q2 is the genuine work item; scope = starter enrichment + register 2 new providers.

## Cross-System Convention

- Adding a provider = one `providers.yaml` entry + `go generate ./...` (regenerates
  `providers_gen.go` and `adapters_gen.go`). This matches every other provider.
- Starter tiers must stay NIM-first with `nous` before `kilo`, `kilo` last
  (enforced by `TestStarterTemplate_FallbackChainOrdering`, main_test.go:516).
- A dedicated `coder` tier is the clean way to expose SWE models without changing the
  `default` catch-all's fast/small behavior.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: the out-of-box config cannot route to
> strong SWE/coding models from DeepSeek/Minimax/Xiaomi. The empty-mappings guard is
> NOT a defect and should not be "fixed."

The guard question (Q3) is a non-issue — leave `ensureDefaultMapping` as-is. The
valuable change is: register `minimax` (and `xiaomi` as base-URL-required) in
`providers.yaml`, and enrich `templates/starter.yaml` with a dedicated `coder` tier
plus DeepSeek/Minimax fallbacks, keeping the NIM→nous→kilo contract intact.

## Confidence

- **HIGH** for Q1 and Q3 (verified in code).
- **MEDIUM** for Q2 model-name accuracy — Minimax `MiniMax-M3` and DeepSeek
  `deepseek-reasoner`/`deepseek-chat` are used; Xiaomi's base URL + model name were
  NOT verifiable and are registered as `require_base_url: true` pending user input.

## What Changes for /10x-plan

Plan should cover: (1) registry add of `minimax` + `xiaomi` in providers.yaml and
regenerate; (2) starter.yaml `coder` tier + DeepSeek/Minimax fallbacks; (3) leave the
empty-mappings guard unchanged. Note: one providers.yaml edit (minimax/xiaomi) was
already made provisionally and matches this framing.

## References

- config.go:182-203 (ensureDefaultMapping), config.go:212-239 (BuildMatchers)
- proxy.go:132-148 (resolveMapping), proxy.go:230-239 (no_match path)
- cmd/freedius/templates/starter.yaml (embedded starter)
- cmd/freedius/main_test.go:516 (fallback ordering contract)
- providers.yaml (source of truth for provider registry)
