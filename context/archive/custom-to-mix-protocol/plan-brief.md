# Custom → Mix + Protocol Field — Plan Brief

> Full plan: `context/changes/custom-to-mix-protocol/plan.md`

## What & Why

The `custom` provider is hardcoded to always use Anthropic protocol, but users may have OpenAI-compatible custom endpoints. By rewriting `custom → mix` internally (same pattern as `zen`/`go`) and adding an optional `protocol` field, users get auto-detection for standard URLs and explicit control for ambiguous ones — without changing their config syntax.

## Starting Point

`CustomAdapter` is an 11-line wrapper delegating to `AnthropicCompatibleAdapter`. The `MixAdapter` already auto-detects protocol from URL paths (`/v1/messages` → Anthropic, else → OpenAI). The `zen`/`go` → `mix` rewrite pattern in `applyEntryDefaults` is proven and testable.

## Desired End State

Users write `provider: custom` and it works with both Anthropic and OpenAI endpoints. Standard URLs auto-detect; ambiguous URLs use `protocol: openai` or `protocol: anthropic`. The `CustomAdapter` struct is gone — one less adapter to maintain.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
| --- | --- | --- |
| Default when protocol omitted | URL sniffing fallback | Zero breaking change for existing configs; proven mix behavior. |
| Protocol field scope | Available to all providers, only mix reads it | Simple, forward-compatible; zen/go rewrite to mix so it works everywhere. |
| Field name | `protocol` | Short, clear, matches what it describes. |
| Keep `custom` in KnownProviders | Yes, as alias → mix | Users keep writing `provider: custom` — meaningful UX name. |

## Scope

**In scope:**
- Add `Protocol` field to `config.Model` with validation
- Change `custom` rewrite from `→ anthropic` to `→ mix`
- Update `MixAdapter.Handle` to check protocol before URL sniffing
- Delete `proxy/custom.go` and `proxy/custom_test.go`
- Update all affected tests

**Out of scope:**
- Removing `custom` from `KnownProviders`
- Making `protocol` mandatory
- Changing `zen`/`go`/`nim` behavior

## Architecture / Approach

Config rewrite in `applyEntryDefaults`: `custom → mix` (one-line change). `MixAdapter.Handle` gains a 4-line protocol check before existing URL sniffing. `CustomAdapter` deleted. All existing `custom` configs continue working because `mix` already handles `/v1/messages` URLs correctly.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Config | Protocol field + validation + custom→mix rewrite | Tests fail until Phase 4 updates them |
| 2. MixAdapter | Protocol-aware routing | None — additive logic |
| 3. Delete CustomAdapter | Remove dead code + deregister | Dangling references if missed |
| 4. Update Tests | All assertions updated, custom_test.go deleted | None |

**Prerequisites:** S-03 (zen-go-adapters) landed — `MixAdapter` is the proven multi-format router.
**Estimated effort:** ~1 session, single phase could even be done atomically.

## Open Risks & Assumptions

- Users with ambiguous custom URLs (not ending in `/v1/messages` or `/v1/chat/completions`) who relied on implicit Anthropic routing will silently get OpenAI routing — acceptable because such URLs likely indicate OpenAI-format endpoints anyway.

## Success Criteria (Summary)

- `provider: custom` configs with `/v1/messages` URLs work identically to before
- `provider: custom` configs with OpenAI-format URLs now work (previously broken)
- `protocol: anthropic` overrides URL sniffing for ambiguous URLs
- `go test ./...` passes with zero failures
