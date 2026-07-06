# Plan Brief: Provider Fallback Routing

## What & Why

freedius currently maps each model name to exactly one provider+model target.
If that target fails — bad config, transport error, or an upstream HTTP
error — the request just fails; there's no automatic retry against another
provider. This plan adds an ordered, per-mapping list of fallback targets so
a request transparently retries the next configured provider/model on
failure, before giving up with one aggregated error.

## Starting Point

- `config.Mapping{ProviderName, ModelString}` is strictly single-target
  (`config/config.go:58-61`).
- `Dispatcher.ServeHTTP` calls `adapter.Handle` exactly once
  (`proxy/proxy.go:268-310`); the `!ww.wroteHeader` check there is the only
  place that both detects failure and proves no bytes reached the client.
- `OpenAICompatibleAdapter.Handle` writes upstream 4xx/5xx errors directly to
  the client and returns `nil` (`proxy/openai_compat.go:146-149`) — the
  dispatcher currently has no way to know an upstream error occurred.
- `AnthropicCompatibleAdapter.Handle` delegates entirely to
  `httputil.ReverseProxy`, which always writes before returning.
- The web mapping dialog has one Provider-select + one Model-input; no
  repeatable-row pattern exists in the templates.
- A prior archived research doc explicitly validated "no silent fallback" as
  correct, tested behavior — this plan is a deliberate, considered reversal.

## Desired End State

A mapping can declare an ordered `Fallback []Mapping` list (inline
provider+model pairs, unbounded length, cycle/duplicate detection only).
On failure, freedius retries down the chain within a shared timeout budget;
success is logged server-side only; full exhaustion returns one aggregated
Anthropic-shaped error naming every attempt. The web UI supports building
and editing chains via a repeatable, unbounded row group in the mapping
dialog.

## Key Decisions Made

| Decision | Choice | Source |
|---|---|---|
| Fallback trigger codes | All error statuses incl. 401/403 | Research clarification |
| Streaming scope | Pre-response only — no mid-stream fallback | Research clarification |
| Exhaustion behavior | Aggregated summary of all attempts | Research clarification |
| Success observability | Server-side log only (no headers/event bus) | Research clarification |
| Config shape | Inline provider+model pairs per mapping | Research clarification |
| Chain depth cap | None — cycle/duplicate detection only | Plan Round 2 + user follow-up |
| Web UI scope | Full support, repeatable row group, unbounded length | Plan Round 2 + user follow-up |
| Adapter contract | Changes for all mappings, not conditional | Plan Round 2 |
| Timeout budget | Shared across chain: per-attempt × configurable multiplier | Plan Round 3 |

## Scope

**In scope:**
- `config.Mapping.Fallback` schema + validation (cycle/duplicate detection)
- Adapter return-contract change (all 3 adapters: openai-compat, anthropic-compat, mix)
- Dispatcher retry loop with shared timeout budget and aggregated exhaustion error
- Web UI: repeatable fallback row group in the mapping dialog + table display
- Server-side logging of fallback attempts and outcomes
- Full test suite updates (existing adapter contract tests + new integration tests)

**Out of scope:**
- Mid-stream fallback (after response headers are sent)
- Response headers or event-bus signaling of fallback activity
- Any fixed max-attempts cap beyond the configured list length
- Provider health tracking / circuit breakers / cross-request state
- TUI dashboard changes (no TUI exists in this codebase today)

## Architecture / Approach

Bottom-up build order: schema (Phase 1) → adapter contract change that makes
upstream-status errors visible to the dispatcher instead of directly written
(Phase 2, highest risk — `AnthropicCompatibleAdapter`'s `ReverseProxy`-based
implementation needs rework to inspect the response before committing it) →
dispatcher retry loop reusing the existing `wroteHeaderResponseWriter`
invariant, with one shared `context.WithTimeout` wrapping the whole chain
(Phase 3) → web UI repeatable row group (Phase 4) → full end-to-end test
coverage and a worked example in `config.example.yaml` (Phase 5). Each phase
is independently testable; Phase 2 must land cleanly (all existing adapter
tests updated, not dropped) before Phase 3 can build on it.

## Phases at a Glance

| Phase | Title | Prerequisites | Estimated effort |
|---|---|---|---|
| 1 | Config Schema & Validation | None | Small |
| 2 | Adapter Return Contract Change | Phase 1 | Medium–Large (highest risk: Anthropic/ReverseProxy retrofit) |
| 3 | Dispatcher Fallback Loop | Phase 1, 2 | Medium |
| 4 | Web UI Support | Phase 1 (schema), independent of 2/3 for basic CRUD | Medium |
| 5 | End-to-End Testing & Docs | Phase 1–4 | Small–Medium |

## Open Risks & Assumptions

- **Risk**: `AnthropicCompatibleAdapter`'s reliance on `httputil.ReverseProxy`
  makes Phase 2 architecturally the hardest change — retrofitting it to
  inspect upstream status before committing bytes may require moving off
  `ReverseProxy`'s single-shot forwarding for the error path. Flagged in the
  plan's Phase 2 §3 as needing careful implementation-time judgment.
- **Assumption**: "cycle detection" for inline (non-referential) fallback
  pairs reduces to exact-duplicate-pair rejection across the chain — there's
  no notion of a mapping "pointing back" to itself since fallback entries
  aren't references to other named mappings.
- **Assumption**: a per-provider default multiplier (e.g. `2`) is a
  reasonable default for the shared timeout budget; not user-validated, since
  this is an implementation-level default, not a user-facing decision point.
- **Risk**: aggregating a heterogeneous set of failure types (e.g. one 429,
  one 401) into a single client-facing error status/type necessarily picks
  one attempt's classification (last attempt, per the plan) — this may not
  always be the most informative choice for the caller.

## Success Criteria Summary

`go test ./...`, `go vet ./...`, and `go build ./...` all pass with every
existing adapter-contract test updated (not dropped) to match the new
"typed pre-header error" contract. Manually: a mapping with a broken primary
and working fallback returns a transparent success with a server-log
fallback trail; a mapping where every target fails returns one aggregated
error naming every attempt; the web UI can build, edit, and remove
multi-entry fallback chains with no hard row-count limit; a plain
single-target mapping with no `fallback:` key behaves identically to today.
