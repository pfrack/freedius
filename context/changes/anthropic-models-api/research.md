---
date: "2026-07-10T07:58:53+02:00"
researcher: kiro
git_commit: 9469af8
branch: main
repository: freedius
topic: "Add Anthropic /v1/models API endpoint for Claude Code gateway model discovery"
tags: [research, codebase, anthropic-api, models-endpoint, claude-code, gateway]
status: complete
last_updated: "2026-07-10"
last_updated_by: kiro
---

# Research: Add Anthropic /v1/models API Endpoint for Claude Code Gateway Model Discovery

**Date**: 2026-07-10T07:58:53+02:00
**Researcher**: kiro
**Git Commit**: 9469af8
**Branch**: main
**Repository**: freedius

## Research Question

How to add a `/v1/models` endpoint to freedius that emulates the Anthropic Models API, enabling Claude Code's gateway model discovery to populate the `/model` picker with freedius's configured mappings.

## Summary

freedius currently has **no `GET /v1/models` endpoint** on the proxy port (8082). It only has a `ModelsCache` + `FetchModels` mechanism used by the **web dashboard** to display upstream provider model lists. To support Claude Code's gateway model discovery, freedius needs to serve `GET /v1/models` in **Anthropic-native format** on the proxy port, returning the configured mapping names as model IDs. This is a self-contained addition: one new route in `newMux`, one handler function, no changes to existing adapters or the dispatcher.

## Detailed Findings

### 1. Anthropic Models API Response Format (Official Spec)

The Anthropic `/v1/models` list endpoint returns:

```json
{
  "data": [
    {
      "type": "model",
      "id": "claude-sonnet-4-6",
      "display_name": "Claude Sonnet 4.6",
      "created_at": "2025-05-14T00:00:00Z"
    }
  ],
  "has_more": false,
  "first_id": "claude-sonnet-4-6",
  "last_id": "claude-haiku-4-5"
}
```

Key fields per model entry:
- `type`: always `"model"`
- `id`: the model identifier (string)
- `display_name`: human-readable label (optional; Claude Code uses it in the picker)
- `created_at`: ISO 8601 timestamp (optional)

Top-level pagination fields:
- `has_more`: boolean
- `first_id`: ID of first entry in `data`
- `last_id`: ID of last entry in `data`

**Important**: This is NOT the OpenAI format (`object: "model"`, `created: unix_timestamp`, `owned_by`). Claude Code specifically needs the Anthropic-native format.

### 2. Claude Code Gateway Discovery Protocol

From the [official gateway protocol reference](https://docs.anthropic.com/en/docs/claude-code/llm-gateway-protocol#model-discovery):

**Trigger conditions:**
- `ANTHROPIC_BASE_URL` is set and NOT pointing at `api.anthropic.com`
- `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` is set
- No `CLAUDE_CODE_USE_*` provider variables are set
- Nonessential traffic is not disabled

**Request:**
- `GET /v1/models?limit=1000`
- 3-second timeout
- Redirects are treated as failure (credential leak prevention)
- Auth: `x-api-key` header (from apiKeyHelper or resolved API key), OR `Authorization: Bearer <token>` (from `ANTHROPIC_AUTH_TOKEN`)
- Any `ANTHROPIC_CUSTOM_HEADERS` are included

**Response parsing by Claude Code:**
- Reads `id` and optional `display_name` from each entry in the `data` array
- **Ignores entries whose `id` doesn't begin with `claude` or `anthropic`**
- Results cached to `~/.claude/cache/gateway-models.json`
- Entries labeled "From gateway" in the picker

**Minimal response that works:**
```json
{
  "data": [
    { "id": "claude-sonnet-4-6", "display_name": "Claude Sonnet 4.6" },
    { "id": "claude-opus-4-8" }
  ]
}
```

Claude Code only reads `id` and `display_name` — other fields (`type`, `created_at`, `has_more`, `first_id`, `last_id`) are tolerated but not required for discovery to work. However, for full Anthropic API compatibility, all fields should be present.

### 3. Current freedius Architecture (Relevant Parts)

#### Route registration (`cmd/freedius/main.go:452-481`)

```go
func newMux(httpHandler http.Handler) *http.ServeMux {
    mux := http.NewServeMux()
    // health handlers...
    mux.Handle("GET /health", healthHandler())
    mux.Handle("HEAD /health", healthHandler())
    mux.Handle("/", rootHandler(httpHandler))
    return mux
}
```

The mux uses Go 1.22+ pattern routing. Adding `GET /v1/models` is a one-line addition.

#### Existing ModelsCache (`proxy/models.go`)

- `ModelsCache` stores `[]ModelView` per provider (keyed by provider name)
- `FetchModels(ctx, provider)` calls upstream `/v1/models` and parses responses
- `deriveModelsURL(baseURL)` transforms chat/completions/messages URLs to `/models`
- Used exclusively by the **web dashboard** (`proxy/web/handlers.go:114`)

This cache is for upstream provider model lists. The new `/v1/models` endpoint serves freedius's own **mapping names** — it does NOT need to call upstream providers at all.

#### Config access pattern (`config/config.go`)

```go
cfg.RLock()
defer cfg.RUnlock()
// read cfg.Mappings
```

The config uses `sync.RWMutex` for concurrent access. The `/v1/models` handler needs to snapshot mapping names under a read lock.

#### Existing adapters and request flow

The proxy currently accepts **only POST** at the catch-all route (`/`). The dispatcher rejects non-POST with 405. The `/v1/models` endpoint must be a GET route registered **before** the catch-all in the mux, which Go's ServeMux handles correctly with pattern specificity.

### 4. Design Approach

#### Option A: Dedicated handler in `cmd/freedius/main.go` (Recommended)

Add a `GET /v1/models` route to `newMux` with a handler that:
1. Reads mapping names from `cfg` under RLock
2. Builds Anthropic-format model entries from mapping keys
3. Returns the response

**Pros:** Simple, self-contained, no adapter changes, fast (no upstream calls)
**Cons:** Handler in main.go needs access to `*config.Config`

#### Option B: Handler in a new `proxy/models_api.go` file

Same logic but in the proxy package, closer to related code.

**Pros:** Better cohesion with existing proxy code
**Cons:** Slightly more wiring (handler needs config reference)

#### Recommendation: Option B

Create `proxy/models_api.go` with an `AnthropicModelsHandler` that takes `*config.Config`. Register it in `newMux`. This keeps the models-related code together (alongside `proxy/models.go`) and follows the existing pattern where proxy-package code handles protocol concerns.

### 5. What Mapping Names to Expose

freedius mappings are user-defined names like `default`, `auto`, `opus`, `sonnet`, `haiku`. Claude Code's discovery filter only shows IDs starting with `claude` or `anthropic`.

**Options:**
1. **Expose all mapping names** — Claude Code will filter out non-matching ones itself
2. **Prefix mapping names** with `claude-` or `anthropic-` — this would break the dispatcher since the model field in requests must match the mapping key
3. **Expose mapping names as-is and document the filter** — the user is responsible for naming their mappings with `claude` prefix if they want them to appear in the picker

**Recommendation:** Option 1 — expose ALL mapping names. Claude Code filters client-side anyway, and users who want discovery to work will name their mappings `claude-sonnet-4-6` or similar. This is the most honest approach. Additionally, add a `display_name` that includes the actual upstream model info (e.g., `"opus → go/deepseek-v4-pro"`).

### 6. Authentication Considerations

Claude Code sends auth headers on the discovery request:
- `x-api-key: <key>` (from helper or env var)
- OR `Authorization: Bearer <token>` (from `ANTHROPIC_AUTH_TOKEN`)

freedius's proxy port currently has **no authentication** — all requests on :8082 are handled. The `/v1/models` endpoint can simply serve without auth checks, matching the existing open-access model of the proxy port. If `FREEDIUS_UI_TOKEN` auth is ever extended to the proxy port, the models endpoint would inherit it naturally.

### 7. Implementation Plan

1. **New file: `proxy/models_api.go`**
   - `AnthropicModelsHandler` struct (holds `*config.Config`)
   - `ServeHTTP(w, r)` method
   - Builds response from `cfg.MappingsSnapshot()`
   - Returns Anthropic-native JSON format

2. **Modify: `cmd/freedius/main.go`**
   - Pass `cfg` to `newMux` (or pass handler directly)
   - Register `GET /v1/models` in `newMux`

3. **New file: `proxy/models_api_test.go`**
   - Test response format matches Anthropic spec
   - Test with empty mappings
   - Test pagination fields (static: `has_more: false`)
   - Test `?limit=N` query param (optional, for completeness)

### 8. Response Shape for freedius

Given a config with mappings `default`, `opus`, `sonnet`, `haiku`:

```json
{
  "data": [
    {
      "type": "model",
      "id": "default",
      "display_name": "default → nim/step-3.5",
      "created_at": "2026-07-10T00:00:00Z"
    },
    {
      "type": "model",
      "id": "opus",
      "display_name": "opus → go/deepseek-v4-pro",
      "created_at": "2026-07-10T00:00:00Z"
    },
    {
      "type": "model",
      "id": "sonnet",
      "display_name": "sonnet → go/minimax-m3",
      "created_at": "2026-07-10T00:00:00Z"
    },
    {
      "type": "model",
      "id": "haiku",
      "display_name": "haiku → zen/claude-sonnet-4-6",
      "created_at": "2026-07-10T00:00:00Z"
    }
  ],
  "has_more": false,
  "first_id": "default",
  "last_id": "haiku"
}
```

**Key insight**: For Claude Code to show these in the picker, mapping names should start with `claude` or `anthropic`. Users who want full discovery integration should name mappings like `claude-sonnet-4-6`, `claude-opus-4-8`, etc. — which also makes the family-matching work naturally.

### 9. Edge Cases

- **Empty mappings**: Return `{"data": [], "has_more": false, "first_id": null, "last_id": null}`
- **`?limit=N` param**: Claude Code sends `?limit=1000`. With typical freedius configs (5-10 mappings), this is a no-op. Honor it for spec compliance.
- **`?before_id` / `?after_id` params**: Pagination cursors from the Anthropic spec. With small mapping counts, can be safely ignored (always return all).
- **`created_at` value**: Use a fixed timestamp (server start time or epoch) since mappings don't have creation times.
- **Concurrent config changes**: Use `cfg.MappingsSnapshot()` for a consistent point-in-time view.

## Code References

- `cmd/freedius/main.go:452-481` — `newMux` function where `GET /v1/models` route will be added
- `proxy/models.go:1-179` — Existing `ModelsCache`, `FetchModels`, `deriveModelsURL` (web dashboard only)
- `proxy/proxy.go:145-160` — Dispatcher.ServeHTTP rejects non-POST (only applies to catch-all route)
- `config/config.go:107-115` — `MappingsSnapshot()` for safe concurrent map reads
- `config/config.go:55-58` — `Mapping` struct with `ProviderName` and `ModelString`

## Architecture Insights

1. **Separation of concerns**: The new endpoint is orthogonal to the existing request dispatcher. It's a read-only config query, not a proxy operation. It doesn't go through the middleware stack (no request ID, no access log, no event bus) — it's a simple metadata endpoint like `/health`.

2. **Pattern**: Follows the same pattern as the health endpoint — registered explicitly in `newMux` with its own handler, not routed through the dispatcher.

3. **No upstream calls needed**: Unlike the web dashboard's `FetchModels` (which queries upstream providers), this endpoint synthesizes the response entirely from local config. This keeps response time sub-millisecond and avoids the 3-second timeout concern.

4. **Config reactivity**: Since `MappingsSnapshot()` reads live config under RLock, the endpoint automatically reflects any mapping changes made through the web dashboard without restart.

## Historical Context (from prior changes)

- `context/archive/2026-07-05-provider-model-discovery/` — Previous change that added the `ModelsCache` and `FetchModels` for the web dashboard. The upstream model fetching was added for UI purposes, not for serving a models API endpoint.

## Open Questions

1. **Should the endpoint require authentication?** The proxy port is currently open. Adding optional auth (respecting `x-api-key` or `Authorization: Bearer`) would match the Claude Code gateway protocol but adds complexity. Recommend: no auth initially, document that LAN exposure should use firewall rules or bind to 127.0.0.1 (the default).

2. **Should `display_name` include routing info?** e.g., `"opus → go/deepseek-v4-pro"` vs just the mapping name. The routing info helps users understand what they're selecting, but it's internal implementation detail. Recommend: include it — transparency is a freedius design principle.

3. **Should non-`claude`/`anthropic` prefixed mappings include a `claude-` prefix option?** e.g., freedius could expose both `opus` (the real mapping) and `claude-opus` (a synthetic alias that maps to the same thing). This would make discovery work with existing non-prefixed configs. Recommend: investigate in planning phase; could be a config option.

4. **HEAD /v1/models?** Claude Code sends `HEAD /` as a connectivity probe. Should `/v1/models` also respond to HEAD? Anthropic's API does. Recommend: yes, trivial to add.
