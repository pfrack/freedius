---
id: provider-writeback-merge
date: 2026-10-12
status: planned
created: 2026-10-12
---

# Provider Writeback Merge — Implementation Plan

## Overview

The dashboard's provider CRUD handlers perform a **whole-value replacement** of
`config.Provider`, built from a form that only knows five fields. The generated
`openai:` adapter-options block and both runtime-only flags are structurally
unreachable from that form, so **any dashboard edit to a known provider silently
destroys its upstream contract** until the process restarts.

Concretely, editing `zen` or `go` in the UI today:

1. **Drops `openai.no_stream_usage`** — the mix adapter starts sending
   `stream_options` to opencode upstreams that reject it (400s at request time).
2. **Drops `openai.pre_send_hook`** (`sanitizeNIMBody` for `nim`) — unsanitized
   bodies reach upstream.
3. **Drops `openai.max_tokens_cap`** (`poolside`) — Claude Code's
   `max_tokens: 64000` goes out unclamped and upstream rejects it.
4. **Zeroes any field absent from the form** — a blank Base URL input erases the
   codegen'd `default_base_url` instead of falling back to it.
5. **Leaves `RequireBaseURL` / `SupportsCountTokens` stale** — both are
   `yaml:"-"`, so they never round-trip and are only re-derived by
   `applyDefaults()`, which the write path never calls. `SupportsCountTokens`
   flipping false makes the dispatcher count tokens locally for a provider that
   could pass through upstream.
6. **Skips validation entirely** — handlers call `SaveData`, not `Save`, so
   `cfg.validate()` never gates a dashboard write.

Decision locked with the user: **merge + re-apply + validate**. Form fields
overlay the existing `Provider` value (preserving `.OpenAI`), `applyDefaults()`
re-derives the generated block and both runtime flags, and `validate()` gates
the write. `providers.yaml` + `go generate ./...` remains the single source of
truth. Accepted tradeoff: the UI cannot *intentionally clear* a generated
option — that stays a `providers.yaml` edit.

## Current State Analysis

**Confirmed by inspection (all line numbers current at commit `0956851`):**

- `decodeProviderForm` (`proxy/web/forms.go:33-45`) calls `r.ParseForm()` then
  builds a fresh `config.Provider{}` from exactly five form values: `name`,
  `behavior`, `default_base_url`, `default_api_key_env`, `protocol`.
  `OpenAI` and `AnthropicVersion` are never read.
- `applyDefaults()` (`config/defaults.go:16-50`) is the only code that merges
  `providerDefaults` into `c.Providers`. It is called solely from
  `loadFromUnmarshaled` (`config/config.go:163`) — reached only by `Load()` and
  `LoadFromBytes()`. **No write path calls it.**
- `handleUpdateProvider` (`proxy/web/handlers.go:1146`) does
  `cfg.Providers[name] = p` — whole-value replacement.
- `handleCreateProvider` (`handlers.go:1087`) does the same; it keeps
  `old, hadOld` purely for rollback, so POST to an existing name is a silent
  upsert (see G4).
- All six writeback handlers call `cfg.SaveData(cfgPath, data)`
  (`handlers.go:1101, 1156, 1219, 1266, 1322, 1372`) — never `cfg.Save()`, so
  `validate()` (`config/config.go:296-308`) is bypassed on every write.
- Consumption side reads these flags per-request: `openai_compat.go:95-103`
  reads `provider.OpenAI.NoStreamUsage`, `.MaxTokensCap`, `.PreSendHook`, and
  **errors on an unknown hook name**. `mix.go:48-80` routes on `.Protocol` /
  URL suffix. A nil `OpenAI` therefore means "no tweaks", not "use defaults".
- Blast radius: only **known** providers (those in `providerDefaults`) lose
  anything. User-defined providers have no generated block to lose.
- The web dashboard is the **only** writeback surface — `proxy/tui/` no longer
  exists, and the six `SaveData` call sites above are the complete set.

## Design

### D1 — Merge form fields onto the existing Provider

Introduce a single decoder that takes the current value and overlays only the
fields the form owns. This fixes G1, G2 and G3 in one place, for both create and
update, with no template or JS change.

`proxy/web/forms.go` — new signature:

```go
// decodeProviderForm decodes a provider form from the request body, overlaying
// the submitted fields onto base. Only the five user-editable fields are
// read; every other field of base (notably the generated OpenAI options block
// and AnthropicVersion) is preserved verbatim. base is the zero Provider when
// creating.
func decodeProviderForm(r *http.Request, base config.Provider) (string, config.Provider, error)
```

Callers: `handleCreateProvider` passes `config.Provider{}`;
`handleUpdateProvider` passes the `old` value it already reads for rollback.

Empty-string semantics: an empty form value overwrites (it is a real
submission), but `applyDefaults` in D2 immediately backfills anything the
generated table declares. Net effect — a blank Base URL input on `nim` keeps
working because `default_base_url` is restored from `providerDefaults`.

### D2 — Re-apply generated defaults after mutation

`applyDefaults()` (`config/defaults.go:16`) currently fills empty
`DefaultBaseURL` / `DefaultAPIKeyEnv`, copies the shared `.OpenAI` pointer, and
sets both runtime flags. It is idempotent and safe to call again.

Call it once, inside the write lock, immediately after the map mutation and
before `Marshal()`. Both handlers already hold `cfg.Lock()` at that point, and
`applyDefaults` mutates only the map — no lock re-entry.

Because `.OpenAI` is restored from the shared read-only pointer, the existing
"shared pointer is safe because OpenAIOptions is read-only after load"
invariant (`config/defaults.go:37-41`) must be restated to also cover the write
path. Extend that comment; do **not** deep-copy (nothing mutates it).

### D3 — Gate the write with validate()

Replace `SaveData(cfgPath, data)` with `Save(cfgPath)` in the two provider
write handlers. `Save` (`config/config.go:508-519`) already does
`validate()` → `Marshal()` → `SaveData()`, so this collapses the
manual `Marshal()` + `marshal_failed` branch into one call and gains validation
for free. Rollback-on-failure structure is unchanged.

`validate()` iterates *all* providers and mappings, so a pre-existing invalid
entry anywhere in the file would now block an unrelated dashboard edit. That is
correct-but-stricter behaviour: it surfaces config corruption instead of
overwriting it. Keep it — but return 400 (`validation_failed`) rather than 500,
since the fault is the config content, not the server.

The four mapping handlers keep `SaveData` in this change (out of scope); note
them as follow-up.

### D4 — Tests

`config/config_test.go` and `proxy/web/` additions, table-driven per
AGENTS.md:

- `TestApplyDefaults_IdempotentAfterWrite` — call `applyDefaults` twice; second
  call is a no-op on an already-defaulted config.
- `TestDecodeProviderForm_PreservesOpenAIOptions` — base carries
  `OpenAI{NoStreamUsage:true, PreSendHook:"sanitizeNIMBody", MaxTokensCap:32768}`;
  POST body has only the five fields; assert all three survive.
- `TestDecodeProviderForm_PreservesAnthropicVersion` — same for
  `AnthropicVersion` (currently lost too, though not in the original gap list).
- `TestHandleUpdateProvider_ZenKeepsNoStreamUsage` — end-to-end through
  `httptest`: PUT to a `zen` provider, assert the written YAML still contains
  `no_stream_usage: true`.
- `TestHandleUpdateProvider_EmptyBaseURLRestoredFromDefaults` — blank
  `default_base_url` on `nim`; assert the persisted YAML keeps the NVIDIA URL.
- `TestHandleUpdateProvider_InvalidConfigReturns400` — a config that fails
  `validate()` yields 400 `validation_failed`, and the on-disk file is
  untouched.
- `TestHandleUpdateProvider_HTMXStillRendersTable` — regression guard on the
  `HX-Request: true` branch: response is an HTML fragment, not JSON.

## What We're NOT Doing

- **Exposing the `openai:` block in the UI** (rejected alternative). Would need
  new form fields, `providers.html` inputs, `editProvider()` JS plumbing, and a
  hook-name registry exported from `proxy` into `web` for validation. Deferred;
  revisit only if users need to tune hooks without editing YAML.
- **G4 — create/update asymmetry.** `handleCreateProvider` silently upserts
  while `handleUpdateProvider` 404s. Real inconsistency, separate change.
- **G6 — disk I/O under the write lock.** `SaveData` runs while holding
  `cfg.Lock()`, stalling dispatcher `RLock`s. Pre-existing and already flagged
  in `context/archive/2026-06-22-tui-providers-mappings-split/reviews/impl-review.md:108`.
  Not worsened by this change; fixing it means restructuring all six handlers.
- **Mapping handler validation** — they still call `SaveData`. Same fix, larger
  blast radius (fallback chains), separate change.
- **`config.Provider` schema changes** — no new fields, no YAML tag changes.
- **`providers.yaml` / codegen changes** — none required; the single source of
  truth is untouched.

## Files Touched

| File | Change |
|---|---|
| `proxy/web/forms.go` | `decodeProviderForm` takes a base `config.Provider` and overlays |
| `proxy/web/handlers.go` | Pass existing value to the decoder; call `applyDefaults()` under lock; `SaveData` → `Save` in the two provider handlers |
| `config/defaults.go` | Extend the shared-`.OpenAI`-pointer comment to cover the write path |
| `config/config_test.go` | Idempotency test |
| `proxy/web/forms_test.go` | Exists today; add decoder preservation tests |
| `proxy/web/handlers_write_test.go` | End-to-end writeback tests |

**No existing test calls `decodeProviderForm` directly** (verified by grep), so
the signature change in D1 breaks no call sites — only the two handlers in
`handlers.go` need updating.


## Verification

1. `mage build` — compiles.
2. `mage test` — all packages green with `-race`; new tests above pass.
3. `go generate ./...` then `git diff --exit-code config/ proxy/` — confirms no
   generated file changed (proves the single-source-of-truth invariant held).
4. Manual: `mage run` with a `zen` provider, edit it in the dashboard, confirm
   the persisted YAML retains `openai: no_stream_usage: true` and the running
   proxy still routes correctly without a restart.

  `HX-Request: true` branch.

## Progress

### Phase 1 — Merge semantics (D1)

#### Automated

- [x] 1.1 `decodeProviderForm` overlays submitted fields onto a base `config.Provider`, preserving `OpenAI` / `AnthropicVersion`
- [x] 1.2 `handleCreateProvider` passes the zero `Provider`; `handleUpdateProvider` passes the existing value

#### Manual

- [ ] 1.3 Confirm the dashboard Edit dialog still round-trips a known provider correctly in a browser

### Phase 2 — Re-apply generated defaults (D2)

#### Automated

- [x] 2.1 Extract `mergeProviderDefaults(name, p)` shared by `applyDefaults` and the new scoped re-apply
- [x] 2.2 Add `Config.ReapplyProviderDefaults(name)` — single-provider, **no injection** of absent providers
- [x] 2.3 Call it under the write lock after mutation in both provider write handlers
- [x] 2.4 Export `Config.Validate(path)` wrapping the unexported `validate`

#### Manual

- [ ] 2.5 Confirm a user-defined (non-`providerDefaults`) provider keeps its hand-written settings across a dashboard edit

### Phase 3 — Validation gate (D3)

#### Automated

- [x] 3.1 Gate both provider write handlers on `Validate` → 400 `validation_failed` with rollback
- [x] 3.2 Save failures still return 500 and still roll back

#### Manual

- [ ] 3.3 Confirm a deliberately broken config is rejected with a readable message rather than silently overwritten

### Phase 4 — Tests (D4)

#### Automated

- [x] 4.1 `TestApplyDefaults_IdempotentAfterWrite` in `config/config_test.go`
- [x] 4.2 Decoder preservation tests in `proxy/web/forms_test.go`
- [x] 4.3 End-to-end writeback tests in `proxy/web/handlers_write_test.go`
- [x] 4.4 `go generate ./...` produces no diff (single-source-of-truth invariant)

#### Manual

- [ ] 4.5 `mage run` with a `zen` provider: edit in dashboard, confirm persisted YAML retains `openai: no_stream_usage: true` and routing still works without restart

Committed in 75a11c9 (`fix(web): preserve generated provider options on dashboard
edits`). Automated gates: build PASS, full suite PASS (8 packages, -race PASS),
`go generate ./...` zero diff PASS, gofmt clean, go vet clean, staticcheck
clean. Deliberate-break verification: removing the merge base fails
TestUpdateProvider_UserDefinedProviderKeepsSettings; removing the re-apply fails
TestUpdateProvider_EmptyBaseURLRestoredFromDefaults.
