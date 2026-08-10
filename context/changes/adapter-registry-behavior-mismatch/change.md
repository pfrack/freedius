---
change_id: adapter-registry-behavior-mismatch
title: Fix dispatcher resolving adapters by behavior instead of provider name
status: new
created: 2026-08-10
updated: 2026-08-10
archived_at: null
---

## Notes

Live P1 regression found during the `nim-nous-kilo-defaults` implementation
review.

`proxy/proxy.go:324` resolves adapters with `d.Registry.Lookup(p.Behavior)`,
but the registry built by `NewDefaultRegistry` (`proxy/adapters_gen.go:122`) is
keyed by **provider name** (`"nim"`, `"openai"`, `"anthropic"`, `"mix"`).
Because `nim`'s behavior is `"openai"`, every NIM request resolves to the plain
`OpenAICompatibleAdapter` and `sanitizeNIMBody` never runs.

Impact: NIM is the primary provider for all five tiers in the default starter.
`sanitizeNIMBody` strips boolean subschemas and aliases the reserved `type`
tool parameter (`proxy/nim_sanitize.go`) — without it, tool-calling against NIM
is expected to fail for non-trivial JSON schemas.

Introduced by `1a1bf3a` (Tui dashboard, #13, 2026-06-20), which changed
`Lookup(m.Provider)` → `Lookup(provider.Behavior)` without re-keying the
registry map. The original code at `b75db78` looked up by provider name and was
correct.

Same class of breakage affects the other generated per-provider wrappers —
`GoogleAdapter`, `OllamaAdapter`, `LmstudioAdapter` — whose `no_stream_usage`
option is likewise never applied.

Why no test caught it: `proxy/nim_test.go` constructs `NewNIMAdapter(...)`
directly, exercising the adapter in isolation and never the registry path the
dispatcher actually takes.

Verified 2026-08-10 with a scratch test:
`Lookup("openai") -> *proxy.OpenAICompatibleAdapter`, not `*NIMAdapter`.
