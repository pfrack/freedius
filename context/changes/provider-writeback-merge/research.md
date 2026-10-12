---
date: 2026-10-12T21:20:18+02:00
researcher: opencode
git_commit: 54abdffed6232418625ae01c83e8efb40b792f76
branch: main
repository: pfrack/freedius
topic: "OpenCode providers (zen/go) update logic — how a dashboard edit mutates a generated provider"
tags: [research, codebase, providers, opencode, zen, go, writeback, config-defaults, codegen, dashboard]
status: complete
last_updated: 2026-10-12
last_updated_by: opencode
---

# Research: OpenCode providers (zen/go) update logic

**Date**: 2026-10-12 21:20 CEST
**Researcher**: opencode
**Git Commit**: `54abdffed6232418625ae01c83e8efb40b792f76`
**Branch**: `main`
**Repository**: pfrack/freedius

## Research Question

How does the provider **update** path behave for the OpenCode providers (`zen`,
`go`) — what mutates a generated provider, what preserves it, and where the
runtime contract is consumed? This maps directly onto the in-flight change
`context/changes/provider-writeback-merge/`, whose objective is exactly this
surface.

## Summary

The OpenCode providers (`zen`, `go`) are **code-generated** entries whose real
contract lives in two places the dashboard form cannot express: the
`openai: { no_stream_usage: true }` adapter-options block and the runtime-only
`RequireBaseURL` / `SupportsCountTokens` flags. The update path is now
**merge → re-apply → validate → save**, implemented in commit `75a11c9`
(`fix(web): preserve generated provider options on dashboard edits`):

1. `decodeProviderForm(r, existing)` overlays only the five form-owned fields
   onto the provider's current value, so `.OpenAI` and `.AnthropicVersion`
   survive (`proxy/web/forms.go:42-53`).
2. `cfg.ReapplyProviderDefaults(name)` re-derives the generated block and both
   runtime flags from `providerDefaults` (`config/defaults.go:51-57`,
   `config/defaults.go:19-40`).
3. `cfg.Validate(cfgPath)` gates the write and returns **400
   `validation_failed`** with rollback (`proxy/web/handlers.go:1173-1178`).
4. `SaveData` persists atomically with a `.bak` backup
   (`config/config.go:534-576`).

Before `75a11c9`, the handler did a whole-value replacement
(`cfg.Providers[name] = p`), which silently dropped `no_stream_usage` on any
`zen`/`go` edit until restart — the mix adapter would then send
`stream_options.include_usage` to OpenCode upstreams that reject it (400s).

**Key invariant**: `providers.yaml` + `go generate ./...` remains the single
source of truth. The UI can enrich a provider but can never *intentionally
clear* a generated option — that stays a `providers.yaml` edit.

## Detailed Findings

### 1. The generated contract for `zen` / `go`

- Source of truth: `providers.yaml:35-47` declares both providers with
  `behavior: mix`, `default_api_key_env: OPENCODE_API_KEY`,
  `require_base_url: true`, and `openai.no_stream_usage: true`. Neither
  declares `default_base_url` (BYO-URL providers).
- Generated output: `config/providers_gen.go:177-185` (`zen`) and
  `config/providers_gen.go:49-57` (`go`). Both carry
  `OpenAI: &OpenAIOptions{NoStreamUsage: true}`, `RequireBaseURL: true`,
  `SupportsCountTokens: false`.
- `zen`/`go` are intentionally **not** auto-injected by `applyDefaults` because
  they have no `DefaultBaseURL` (`config/defaults.go:69-77`; see lessons.md
  "Adding New Providers: Auto-Inject + Env-Var Scope"). A user must declare
  them, then the dashboard can edit them.

### 2. The update handler, step by step

`handleUpdateProvider` (`proxy/web/handlers.go:1137-1203`):

1. Extract `name` from the path; 400 `bad_path` on failure
   (`handlers.go:1139-1143`).
2. `cfg.RLock()` → read `existing` → 404 `not_found` if absent
   (`handlers.go:1145-1151`).
3. `decodeProviderForm(r, existing)` — overlay, not replace
   (`handlers.go:1155`).
4. `validateProviderFields(name, p)` — per-field validation; returns a
   structured `ValidationError` (`forms.go:86-109`).
5. `cfg.Lock()` → snapshot `old` → assign `p` → `ReapplyProviderDefaults(name)`
   (`handlers.go:1165-1171`).
6. `cfg.Validate(cfgPath)`; on failure restore `old`, 400 `validation_failed`
   (`handlers.go:1173-1178`).
7. `cfg.Marshal()`; on failure restore `old`, 500 `marshal_failed`
   (`handlers.go:1180-1186`).
8. `cfg.SaveData(cfgPath, data)`; on failure restore `old`, 500 `save_failed`
   (`handlers.go:1187-1194`).
9. `cfg.Unlock()`; `HX-Request: true` → render the table fragment, else JSON
   `{"status":"updated"}` (`handlers.go:1195-1202`).

`handleCreateProvider` (`proxy/web/handlers.go:1073-1135`) is the same shape
with two differences: it passes the **zero** `config.Provider{}` as the merge
base (`handlers.go:1075`), and it silently **upserts** an existing name
(`old, hadOld` is used only for rollback, `handlers.go:1086-1091`) — the G4
create/update asymmetry explicitly deferred by the plan.

### 3. The merge base — the load-bearing line

`decodeProviderForm` (`proxy/web/forms.go:42-53`) does `p := base` and then
overwrites exactly five fields: `Behavior`, `DefaultBaseURL`,
`DefaultAPIKeyEnv`, `Protocol` (plus the returned `name`). It never touches
`OpenAI` or `AnthropicVersion`. Empty form values **do** overwrite, but
`ReapplyProviderDefaults` immediately backfills anything the generated table
declares — so a blank Base URL on a `zen` provider is fine only if
`providers.yaml` gave it a `default_base_url` (it did not for `zen`/`go`;
`RequireBaseURL` then makes `validate` reject the empty value,
`config/config.go:378-384`).

### 4. Re-applying generated defaults

`mergeProviderDefaults` (`config/defaults.go:19-40`) fills empty
`DefaultBaseURL` / `DefaultAPIKeyEnv`, restores `.OpenAI` from the shared
read-only pointer, and **unconditionally** overwrites `RequireBaseURL` and
`SupportsCountTokens`. It is idempotent.

- `ReapplyProviderDefaults` (`config/defaults.go:51-57`) is the single-provider,
  no-injection write-path entry point. It is a no-op for user-defined providers
  not present in `providerDefaults` (`defaults.go:20-23`), which is why a
  hand-written gateway keeps its settings.
- `applyDefaults` (`config/defaults.go:65-81`) is the load-path entry point and
  the only one that injects absent providers.
- Shared-pointer safety: `.OpenAI` is never deep-copied because
  `OpenAIOptions` is never mutated after load, on either path
  (`config/defaults.go:15-18`, restated for the write path by this change).

### 5. Consumption — where `no_stream_usage` actually matters

- `OpenAICompatibleAdapter.Handle` copies `provider.OpenAI.NoStreamUsage`,
  `.MaxTokensCap`, and resolves `.PreSendHook` (erroring on an unknown hook
  name) into `translate.Opts` (`proxy/openai_compat.go:93-111`; hook registry
  at `proxy/openai_compat.go:26`).
- The mix adapter's OpenAI sub-path reads the same options, which is why
  `zen`/`go` carry the flag (`proxy/mix.go:26-41`, routing at
  `proxy/mix.go:48-80`).
- The flag is honored at translation time: `if req.Stream && !opts.NoStreamUsage`
  gates emission of `stream_options` (`proxy/translate/anthropic_openai.go:64`).
- `SupportsCountTokens` is read per-request by the dispatcher to decide between
  local counting and upstream pass-through (`proxy/proxy.go:283-286`).
- `RequireBaseURL` is only read by `validateProvider`
  (`config/config.go:378-384`).

### 6. Validation and persistence

- `Validate` (`config/config.go:296-303`) is the exported wrapper added by this
  change; `validate` (`config/config.go:305-317`) iterates **all** providers and
  mappings, so a pre-existing invalid entry anywhere now blocks an unrelated
  dashboard edit. Chosen deliberately (surface corruption rather than overwrite
  it), hence 400 rather than 500.
- `Save` (`config/config.go:505-528`) = `validate` → `Marshal` → `SaveData`.
  The provider handlers still call `Marshal` + `SaveData` manually so they can
  distinguish `marshal_failed` from `save_failed` and keep per-error rollback;
  the plan's D3 proposed collapsing to `Save`, and the implemented version kept
  the two-step for the finer error taxonomy.
- `SaveData` (`config/config.go:534-576`) writes atomically: `path → path.bak`,
  temp file, rename over `path`, with backup-restore on failure.
- `Marshal` (`config/config.go:500-503`) omits the `yaml:"-"` runtime flags, so
  `RequireBaseURL` / `SupportsCountTokens` never round-trip and *must* be
  re-derived on every write — this is the reason `ReapplyProviderDefaults` is
  mandatory, not optional.

### 7. Tests that lock the behaviour

- `TestUpdateProvider_ZenKeepsNoStreamUsage` — headline regression: PUT to
  `zen`, assert on-disk YAML still contains `no_stream_usage: true` and the
  reloaded provider keeps `RequireBaseURL`
  (`proxy/web/handlers_write_test.go:453-489`).
- Decoder preservation tests in `proxy/web/forms_test.go:167-239`.
- `TestApplyDefaults_IdempotentAfterWrite` in `config/config_test.go`.
- `TestUpdateProvider_UserDefinedProviderKeepsSettings` — deliberate-break guard
  for the merge base (removing `base` fails it).
- `TestUpdateProvider_EmptyBaseURLRestoredFromDefaults` — deliberate-break guard
  for the re-apply.

### 8. Model discovery (adjacent, not part of update)

`FetchModels` derives `/models` from the provider's base URL
(`proxy/models.go:76-96`) and resolves mix auth via `resolveMixProtocol`
(`proxy/models.go:196-208`). For `zen`/`go` this only works once a
`default_base_url` is set (e.g. `https://opencode.ai/zen/v1/messages` →
`https://opencode.ai/zen/v1/models`, exercised in `proxy/models_test.go:38-44`).
Model discovery is read-only and unaffected by the writeback merge.

## Code References

- `config/config.go:60-81` — `Provider` struct; `RequireBaseURL` /
  `SupportsCountTokens` are `yaml:"-"`.
- `config/config.go:83-92` — `OpenAIOptions` (`no_stream_usage`,
  `pre_send_hook`, `max_tokens_cap`).
- `config/config.go:296-317` — `Validate` / `validate`.
- `config/config.go:336-394` — `validateProvider`; `RequireBaseURL` check at
  `:378-384`.
- `config/config.go:500-576` — `Marshal` / `Save` / `SaveData`.
- `config/defaults.go:19-40` — `mergeProviderDefaults`.
- `config/defaults.go:51-57` — `ReapplyProviderDefaults` (write-path entry).
- `config/defaults.go:65-81` — `applyDefaults` (load-path entry, injects).
- `config/providers_gen.go:49-57` — generated `go`; `:177-185` — generated `zen`.
- `providers.yaml:35-47` — `zen` / `go` source declarations.
- `proxy/web/forms.go:42-53` — `decodeProviderForm` merge decoder.
- `proxy/web/handlers.go:1073-1135` — `handleCreateProvider`.
- `proxy/web/handlers.go:1137-1203` — `handleUpdateProvider`.
- `proxy/openai_compat.go:26,93-111` — hook registry + options consumption.
- `proxy/translate/anthropic_openai.go:64` — `NoStreamUsage` gate.
- `proxy/mix.go:48-103` — mix routing + `normalizeBaseURL`.
- `proxy/proxy.go:283-286` — `SupportsCountTokens` dispatch branch.

## Architecture Insights

- **Two-phase defaults**: the same `providerDefaults` table feeds a load path
  (`applyDefaults`, which may inject) and a write path
  (`ReapplyProviderDefaults`, which never injects). The split exists so a
  dashboard write cannot pollute the config with providers the user never
  declared.
- **Overlay-over-replace** is the core fix: a form is a partial view of the
  resource, so decoding must merge onto the existing value. Any future field
  added to `Provider` that the form does not own is preserved for free by the
  same decoder.
- **`yaml:"-"` implies "must be re-derived"**: runtime-only flags are a trap for
  any write path, since they silently revert to zero unless defaults are
  re-applied. This is the class of bug the change fixed.
- **Validation is whole-config**: `validate` iterates every provider and
  mapping, so write gating is stricter than the single edited entry. Accepted
  tradeoff (see `config/config.go:296-303`).
- **Codegen is the single source of truth**: `providers.yaml` →
  `genproviders` → `providers_gen.go`; `go generate ./...` must produce zero
  diff (plan verification step 3). The UI intentionally cannot clear generated
  options.

## Historical Context (from prior changes)

- `context/changes/provider-writeback-merge/plan.md` — the plan for this exact
  surface: G1–G6 gap list, D1–D4 design, and the locked decision
  "merge + re-apply + validate". Committed `75a11c9`; epilogue `54abdff`.
- `context/archive/opencode-nim-fixes/research.md` — origin of the
  `no_stream_usage` requirement: OpenCode Go/Zen endpoints and NIM SSE
  behaviour; established that `zen`/`go` upstreams reject
  `stream_options.include_usage`.
- `context/archive/config-driven-adapter-opts/` — introduced the `openai:`
  adapter-options block (`no_stream_usage`, `pre_send_hook`, `max_tokens_cap`)
  and its codegen, i.e. the very block the update path had to start preserving.
- `context/archive/2026-06-22-tui-providers-mappings-split/reviews/impl-review.md:108`
  — pre-existing finding: `SaveData` runs while holding the write lock, stalling
  dispatcher `RLock`s (G6, still open, not worsened here).
- `context/foundation/lessons.md` — "Adding New Providers: Auto-Inject +
  Env-Var Scope" explains why `zen`/`go` are not auto-injected (no
  `DefaultBaseURL`); "custom → mix Rewrite in applyDefaults" is stale for the
  current tree (the rewrite now happens in codegen / `providerDefaults`).

## Related Research

- `context/archive/opencode-nim-fixes/research.md` — OpenCode Go auth + NIM SSE
  root cause (the reason `zen`/`go` need `no_stream_usage`).

## Open Questions

- **G4 — create/update asymmetry**: `handleCreateProvider` upserts silently
  while `handleUpdateProvider` 404s. Deferred; separate change.
- **G6 — disk I/O under the write lock**: `SaveData` runs inside `cfg.Lock()`,
  stalling dispatcher readers. Fixing it means restructuring all six writeback
  handlers.
- **Mapping handlers** still call `SaveData` without `validate`; the same fix
  has a larger blast radius (fallback chains). Noted as follow-up in the plan.
- **Permalinks**: `HEAD` (`54abdff`) is ahead of `origin/main` (`0956851`), so
  references above are local `file:line`, not GitHub permalinks. They become
  linkable once the branch is pushed.
