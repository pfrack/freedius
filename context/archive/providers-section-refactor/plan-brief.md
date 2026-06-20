# Provider/Mapping Structural Refactor — Plan Brief

> Full plan: `context/changes/providers-section-refactor/plan.md`
> Research: `context/changes/providers-section-refactor/research.md`

## What & Why

Split the conflated `config.Model` struct (7 fields serving both provider identity and model routing) into two types: `Provider` (behavior, base_url, api_key) and `Mapping` (name → provider + freetext model). Eliminate the alias rewriting system (`custom`/`zen`/`go` → `mix`) by making aliases first-class provider entries. Every mapping currently redundantly carries `base_url` and `api_key_env` even when sharing the same provider.

## Starting Point

`config.Model` (`config/config.go:22-30`) is used as the value type for both `Config.Models` and `Config.Mappings` maps. A two-phase `applyEntryDefaults` function (`config/providers_gen.go:71-95`) rewrites aliases and fills defaults. The TUI has a single 7-field form for both models and mappings. The generator (`internal/genproviders/main.go`) emits provider metadata as separate maps (`KnownProviders`, `knownProviderDefaults`, `requireBaseURL`, `PresetProviders`).

## Desired End State

A `freedius.yaml` with `providers:` (behavior, base_url, api_key) and `mappings:` (provier_name + model_string). No alias rewriting. The TUI has separate forms: `p` for provider (6 fields), `a` for mapping (3 fields). Config loading validates that mapping `provider_name` references a defined provider. Adapters receive resolved `Provider` + `Mapping` structs instead of the conflated `Model`.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Adapter Handle signature | Two params: Provider + Mapping | Clear separation of concerns; adapter knows both provider config and target model. | Plan |
| Config YAML keys | `providers:` + `mappings:` | Self-documenting; "providers hold config, mappings hold routing." | Plan |
| Backward compatibility | Hard break, no migration | Zero migration code to maintain; clear error messages from YAML strict-unmarshal. | Plan |
| Alias rewriting | Eliminated entirely | zen/go/custom become standalone provider entries with behavior=mix; no runtime rewriting. | Research |
| Per-mapping overrides | None — pure mapping | Matches stated goal; complex cases handled by separate provider entries. | Plan |
| AnthropicVersion | Provider-level only | Property of the upstream API endpoint, not the model. | Plan |
| Mix routing | Sniff base_url path suffix | `/v1/messages` → anthropic, else → openai. Same as current fallback. | Plan |
| Count tokens | Provider.SupportsCountTokens flag | Set at defaults-merge time; no duplicate routing logic in adapters. | Plan |
| Family matching | Kept as-is | Operates on mapping names only; independent of the refactor. | Plan |
| Phase ordering | Data model → adapters → TUI → generators | Types must exist before adapters can use them; TUI needs settled adapter signature. | Plan |
| TUI forms | Separate provider (p) and mapping (a) forms | Two different things get different forms and key bindings. | Plan |
| Test strategy | Update all tests in-phase | Tests stay green per-phase; no deferred test rewrite. | Plan |

## Scope

**In scope:**
- New `Provider` and `Mapping` structs in `config/config.go`
- Split validation (`validateProvider` + `validateMapping`)
- Simplified `Marshal` (no clone/restore dance)
- Updated `checkRequiredEnvVars` in `main.go`
- Changed `Provider.Handle` interface signature in `proxy/provider.go`
- Updated all 4 adapters (nim, openai, anthropic, mix)
- Updated dispatcher (no `Models` map)
- Split TUI forms (provider 6-field, mapping 3-field)
- Updated genproviders generator and providers.yaml
- Updated starter template and example config
- All config, proxy, proxy/tui, and main tests updated

**Out of scope:**
- Auto-migration of old config format
- Supporting `models:` YAML key
- Per-mapping base_url or api_key_env overrides
- Changing family-pattern matching
- Proxy/translate package changes
- Event bus or middleware changes

## Architecture / Approach

**Data model first**: Phase 1 defines `Provider` and `Mapping` types, updates `Config`, `Load`, `Validate`, `Save`, `Marshal`, and `checkRequiredEnvVars`. All config tests updated in this phase.

**Adapter update**: Phase 2 changes the `Provider` interface to accept `(w, r, provider, mapping, body)`. Each adapter reads its fields from the appropriate struct: base URL/api key from `provider`, model string from `mapping`. Dispatcher removes `Models` lookup, resolves provider from `Providers[mapping.ProviderName]`.

**TUI split**: Phase 3 creates separate forms with separate key bindings (`p` for add provider, `a` for add mapping). Config tab renders providers section first, then mappings section, with a single cursor spanning both.

**Generator refresh**: Phase 4 updates genproviders to emit a `providerDefaults` map instead of separate metadata maps and the `applyEntryDefaults` function. `providers.yaml` loses `rewrite_to`.

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  providers:  │     │  mappings:   │     │  Dispatcher  │
│  nim:        │     │  haiku:      │     │  resolves    │
│   behavior:  │     │   provider:  │────→│  Provider +  │
│   base_url:  │     │   model:   ...│    │  Mapping     │
│  go:         │     │  opus:       │     │  then calls  │
│   behavior:  │     │   provider:  │     │  adapter     │
│   base_url:  │     │   model:   ...│    │  .Handle()   │
└──────────────┘     └──────────────┘     └──────┬───────┘
                                                 │
                                    ┌────────────┴───────────┐
                                    │  Provider Interface    │
                                    │  Handle(w,r,provider,  │
                                    │  mapping, body)        │
                                    └────────────────────────┘
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. New Config Data Model | Provider/Mapping types, validation, Save, starter template | Getting field semantics right — Behavior vs Protocol naming, YAML tag choices |
| 2. Adapter Signature Change | Updated Handler signatures, dispatcher, capabilities | All adapters and mock providers must be updated simultaneously; ~10 test files |
| 3. TUI Split | Separate provider/mapping forms, views, picker | Cursor spanning two lists; form validation branching; picker data sources |
| 4. Generator Update & Cleanup | Genproviders emits providerDefaults, providers.yaml cleaned, full suite green | Regenerated code must match new types; genproviders test assertions need updating |

**Prerequisites:** Research document at `context/changes/providers-section-refactor/research.md` (complete)
**Estimated effort:** ~4 implementation sessions across 4 phases

## Open Risks & Assumptions

- **Mappings referencing deleted providers**: Deleting a provider does not cascade-delete its mappings. Orphaned mappings will fail at request time with "provider not found". This is accepted — no referential integrity enforcement.
- **Behavior naming**: Using `"openai"`, `"anthropic"`, `"mix"` as behavior values. These must stay in sync with adapter registry keys.
- **`SupportsCountTokens` derivation**: For mix providers, the flag is set based on whether `DefaultBaseURL` path ends with `/v1/messages` at config-load time. If a user changes the base_url later (via TUI), the flag won't auto-update. Mitigation: `applyDefaults` re-runs on every `Save` call.
- **Hard break acceptance**: Users with existing configs must manually rewrite. This is deliberate but worth documenting in release notes.

## Success Criteria (Summary)

- `freedius init` writes new-format config with `providers:` and `mappings:` sections
- `go test ./...` passes full suite
- TUI has separate `p` (add provider) and `a` (add mapping) forms
- Requests route correctly with new config format
- Old configs produce clear parse errors
