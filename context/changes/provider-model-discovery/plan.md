# Provider Model Discovery UI — Implementation Plan

## Overview

Add a web-dashboard feature to fetch, cache, and refresh provider model lists from upstream `/v1/models` endpoints. Two UI surfaces share one backend: a "Fetch models" button on the Providers page, and a `<datalist>` autocomplete on the mapping form's `model_string` input. The cache is in-memory only (no TTL, no on-disk persistence), refreshed via explicit user action. The fetcher branches on `behavior` (openai/anthropic/mix) for auth headers and URL derivation.

## Current State Analysis

No model-discovery code exists anywhere — the `/v1/models` endpoint is never called, no cache exists, and the mapping form's `model_string` field is a free-text input with no assistance.

The web dashboard (`proxy/web/`) uses Go stdlib `html/template` + vendored htmx (zero runtime deps). The codebase follows a consistent pattern for concurrency-safe state: `sync.RWMutex` + map + `Snapshot()` copy-out (precedented in `config.Config`, `EventBus`, `LogSink`). The `eventstream.Handlers` struct wires dependencies into web handlers. Auth is a single boundary around the whole mux.

One pre-existing bug: `renderProvidersTable`/`renderMappingsTable` (`handlers.go:274,318`) try to load `providers-table.html`/`mappings-table.html` partials that don't exist, causing every write handler's htmx path to 500. The new feature avoids this by using a self-contained fragment template — not routed through those broken partial loaders.

### Key Discoveries:

- Upstream auth patterns: OpenAI-compat sets `Authorization: Bearer` (`proxy/openai_compat.go:77,134-138`), Anthropic sets `x-api-key` + `anthropic-version` (`proxy/anthropic_compat.go:78-92`)
- URL derivation: stored base URLs are endpoint paths (e.g. `…/chat/completions`), must be suffix-swapped to `/models`. The existing `normalizeBaseURL` (`proxy/mix.go:84-102`) is a near-match but handles only one "other" suffix at a time
- Behavior dispatch: `behavior` field is the single switch for both auth headers and response parser shape
- Both OpenAI-style and Anthropic-style `/v1/models` responses share `data[].id`, so a minimal common parser covers all providers
- Local providers (ollama/lmstudio) have no `DefaultAPIKeyEnv` — the fetcher must skip auth when the env name is empty

## Desired End State

A developer opens the Providers page, clicks "Fetch models" on a provider row, and sees a list of available models appear inline (with a "refreshed X ago" timestamp). On the Mappings page, when adding or editing a mapping, the `model_string` input offers a `<datalist>` of fetched models for the selected provider — the dev can accept a suggestion or type a custom model ID freely. Offline or misconfigured providers show a friendly error inline instead of crashing or blocking the UI.

### Verification:

- Click "Fetch models" on any provider → model list renders inline; a second click refreshes it
- On the mapping form, select a provider → the model input's datalist populates with cached models
- When the API key is unset or upstream is unreachable → a `.form-error` message appears inline; the mapping form's datalist remains functional if models were previously cached
- Direct browser GET to `/v1/providers/{name}/models` returns the cached model list as an HTML fragment

## What We're NOT Doing

- **No on-disk cache persistence** in this change (deferred: cold-start optimization, always re-fetchable)
- **No TTL, auto-expiry, or background refresh** — the codebase has no expiry machinery, and refresh is explicit via button
- **No SSE push** for "models fetched" events — the htmx fragment response is sufficient
- **No `freedius init` integration** — the cache is placed in `proxy/` for future import, but no CLI surface in this change
- **No pagination follow-up** — single page with limit=1000; if a provider has >1000 models a UI note warns of truncation
- **No hard `<select>` replacing the model input** — it stays a free-text input with datalist suggestions; the config layer allows arbitrary model strings and offline/air-gapped use is a design goal

## Implementation Approach

**Three phases, bottom-up**: core domain logic (cache + URL derivation + upstream fetcher) → web endpoints + fragment templates → UI surface integration. Each phase is independently testable. The cache lives in `proxy/` (not `proxy/web/`) so it's a domain object importable by future packages (e.g. `init`).

The fetcher constructs a short-timeout HTTP client per request (models GET is cheap, unlike the 5-min streaming timeout used by adapters). Auth branch mirrors the mix adapter's dispatch: `openai` → Bearer, `anthropic` → `x-api-key`+version, `mix` → resolve via `Protocol` field or URL path sniffing.

## Critical Implementation Details

### URL derivation — deepseek edge case

Stored base URLs are endpoint paths, not API roots. The `deriveModelsURL` helper must handle providers whose base URL lacks a `/v1` segment (deepseek: `https://api.deepseek.com/chat/completions`). Rule: parse URL → strip trailing `/chat/completions` or `/messages` suffix → append `/models`. If the path has neither suffix, append `/models` to the path as-is. Test all variants from the research table (groq, deepseek, anthropic, zen, go, ollama).

### Fragment template — dodge the missing-partials bug

The existing `loadPageTemplate` wraps every template in `layout.html` and `renderProvidersTable` calls `loadPageTemplate("providers-table.html")` which doesn't exist. The new fragment template must bypass both: add a `loadFragmentTemplate` to `embed.go` that parses a single template file standalone (no layout), and execute it directly in the handler. Do not call `renderPage` or any of the broken table renderers.

### Graceful degradation for local/offline providers

Providers with empty `DefaultAPIKeyEnv` (ollama, lmstudio) must not fail — skip setting auth headers entirely. Providers with empty `DefaultBaseURL` (require_base_url before config) must return a clear error message. Upstream connection errors must not crash the handler or block the mapping form's datalist from returning cached data on GET.

---

## Phase 1: Core Domain — Cache, URL Derivation, and Upstream Fetcher

### Overview

Create the `ModelsCache` (in-memory, RWMutex-guarded), the `deriveModelsURL` pure function, and the `fetchModels` HTTP helper in `proxy/models.go`. These are pure logic with no web dependency — testable in isolation.

### Changes Required:

#### 1.1 ModelsCache struct and types

**File**: `proxy/models.go` (new)

**Intent**: Add a concurrency-safe, in-memory cache for fetched model lists, following the codebase's RWMutex+map+Snapshot() pattern from `Config`, `EventBus`, and `LogSink`.

**Contract**:
- `ModelView` exported struct: `ID string`, `DisplayName string`
- `modelsEntry` unexported: `Models []ModelView`, `FetchedAt time.Time`, `Err string`
- `ModelsCache` exported struct with unexported `mu sync.RWMutex`, `entries map[string]modelsEntry`
- `NewModelsCache() *ModelsCache` constructor
- `Get(name string) ([]ModelView, time.Time, error)` — RLock read, returns (nil, zero, nil) on miss
- `Set(name string, models []ModelView, err error)` — Lock write, records FetchedAt=time.Now()
- No Snapshot() method needed (no lock-free iteration across entries required yet)

#### 1.2 URL derivation helper

**File**: `proxy/models.go` (new — same file)

**Intent**: Derive the upstream `/v1/models` URL from a provider's stored base URL (which may end in `/chat/completions`, `/v1/messages`, or neither).

**Contract**: `func deriveModelsURL(baseURL string) (string, error)` — parse → strip trailing `/chat/completions` or `/messages` suffix → append `/models`. Returns error on unparseable URL. Handles the deepseek edge case (no `/v1` segment — `…/chat/completions` → `…/models`).

#### 1.3 Upstream model fetcher

**File**: `proxy/models.go` (new — same file)

**Intent**: Fetch model list from an upstream provider's `/v1/models` endpoint with per-behavior auth headers, parse the minimal common JSON shape (`data[].id` with optional `display_name`), and return `[]ModelView`.

**Contract**:
- `func fetchModels(ctx context.Context, provider config.Provider) ([]ModelView, error)`
- Constructs a dedicated `*http.Client` with a short timeout (e.g. 10s)
- Derives the models URL via `deriveModelsURL(provider.DefaultBaseURL)`
- Branches on `provider.Behavior` for auth headers (openai → `Authorization: Bearer`, anthropic → `x-api-key` + `anthropic-version`, mix → resolve via `Protocol` or URL path sniffing) — mirroring `proxy/mix.go:56-78`
- Reads API key from `os.Getenv(provider.DefaultAPIKeyEnv)`; skips auth header when env name is empty (local providers)
- Parses `{"data": [{"id": "…", "display_name": "…"}]}` shape — `display_name` optional (Anthropic only), falls back to `id`
- Returns empty slice + nil error when API key env is set but empty (graceful no-op, not an error)

#### 1.4 Unit tests

**File**: `proxy/models_test.go` (new)

**Intent**: Test the cache, URL derivation, and fetch logic in isolation.

**Contract**:
- `deriveModelsURL` table-driven tests covering: groq (`/openai/v1/chat/completions` → `/openai/v1/models`), deepseek (no `/v1`), anthropic (`/v1/messages` → `/v1/models`), zen mix, go mix, ollama, URL with no recognized suffix, invalid URL
- `ModelsCache` tests: Set/Get round-trip, miss returns zero values, concurrent reads/writes with `-race`
- `fetchModels` integration test: spin up `httptest.Server` returning canned `/models` JSON for openai shape and anthropic shape, call fetchModels against it, verify parsed ModelView slice

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/ -run "TestDeriveModelsURL|TestModelsCache|TestFetchModels" -race -count=1` passes
- No data races under concurrent cache access

#### Manual Verification:

- N/A (pure logic, no UI surface)

---

## Phase 2: Web Endpoints + Fragment Templates

### Overview

Wire the `ModelsCache` into `eventstream.Handlers`, register two new routes (`GET /v1/providers/{name}/models` and `POST /v1/providers/{name}/models/refresh`), create a self-contained fragment template, and add handler logic. The GET endpoint reads the cache (silently returning cached data even after a fetch error). The POST endpoint forces an upstream fetch, updates the cache, and returns the updated fragment.

### Changes Required:

#### 2.1 Add ModelsCache to Handlers struct

**File**: `internal/eventstream/handlers.go`

**Intent**: Inject `ModelsCache` alongside existing dependencies (`Cfg`, `Bus`, `LogSink`, etc.) so web handlers can reach it.

**Contract**: Add `ModelsCache *proxy.ModelsCache` field to the `Handlers` struct.

#### 2.2 Construct ModelsCache in main.go

**File**: `cmd/freedius/main.go`

**Intent**: Create the `ModelsCache` instance at startup and pass it into `eventstream.Handlers`.

**Contract**: Insert `mc := proxy.NewModelsCache()` before the `Handlers` literal (around line 158), add `ModelsCache: mc` to the struct initialization.

#### 2.3 Register new routes in SetupMux

**File**: `proxy/web/handlers.go`

**Intent**: Add two routes using Go 1.22 `{name}` wildcards alongside existing routes.

**Contract**: Add to `SetupMux`:
- `mux.HandleFunc("GET /v1/providers/{name}/models", …)` → calls `handleFetchModels`
- `mux.HandleFunc("POST /v1/providers/{name}/models/refresh", …)` → calls `handleRefreshModels`

#### 2.4 Fragment template loader

**File**: `proxy/web/embed.go`

**Intent**: Add a template loader for self-contained fragment files that don't wrap in `layout.html`. This dodges the missing-partials bug and keeps the models fragment independent.

**Contract**: `func loadFragmentTemplate(name string) (*template.Template, error)` — parses `templates/` + name from the embedded FS, cached in a `sync.Map` (or reuse `pageTemplates` pattern). Must not include `layout.html`.

#### 2.5 Self-contained models fragment template

**File**: `proxy/web/templates/models-fragment.html` (new)

**Intent**: Define the HTML fragment rendered when a model list is fetched — a `<ul>` or table of model IDs with a "refreshed X ago" timestamp, plus an error state.

**Contract**:
- Success state: renders model list with ID + optional display_name, plus "Fetched X ago" relative time
- Error state: renders a `<div class="form-error">` with the error message
- Both states: wrapped in a `<div id="models-{providerName}">` so the htmx target swap replaces the whole region
- Template data: `modelsData{Provider string; Models []ModelView; FetchedAt string; Error string}`

#### 2.6 View model type

**File**: `proxy/web/types.go`

**Intent**: Add a view model struct for the models fragment, following the existing `providerRow`/`mappingRow` convention.

**Contract**: Add `modelsData` struct (no `pageData` embedding — this is a fragment, not a page):
```go
type modelsData struct {
    Provider  string
    Models    []proxy.ModelView
    FetchedAt string
    Error     string
}
```

#### 2.7 Fetch models handler (GET)

**File**: `proxy/web/handlers.go`

**Intent**: Handle `GET /v1/providers/{name}/models` — read the cache and return an HTML fragment. Never calls upstream; returns cached data even if stale (silent fallback for the datalist). Returns an empty fragment on cache miss.

**Contract**: `func handleFetchModels(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger)` — extracts provider name from `r.PathValue("name")`, validates provider exists in config, reads cache via `h.ModelsCache.Get(name)`, renders `models-fragment.html` via `loadFragmentTemplate`. On miss: renders fragment with empty model list (not an error).

#### 2.8 Refresh models handler (POST)

**File**: `proxy/web/handlers.go`

**Intent**: Handle `POST /v1/providers/{name}/models/refresh` — fetch upstream, update cache, return fragment. On success: models appear inline. On failure: error fragment with `.form-error` styling, but cache retains previous data (GET silently returns it).

**Contract**: `func handleRefreshModels(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger)` — validates provider exists and has a `BaseURL`, resolves `config.Provider` from config, calls `proxy.FetchModels(ctx, provider)`, calls `h.ModelsCache.Set(name, models, err)`, renders `models-fragment.html`. On upstream error: sets `modelsData.Error` to a user-friendly message, renders error fragment. Must not crash on missing API key env — returns "API key not set" error.

#### 2.9 Endpoint tests

**File**: `proxy/web/handlers_models_test.go` (new) or appended to existing test file

**Intent**: Test the two endpoints with an `httptest.Server` as the upstream.

**Contract**: Table-driven tests using `newWriteMux` harness extended with `ModelsCache`:
- `GET /v1/providers/nim/models` on cold cache → empty fragment (200 OK, body contains no model entries or "No models fetched yet")
- `POST /v1/providers/nim/models/refresh` against upstream returning JSON → 200, fragment contains model IDs
- `GET` after refresh → fragment contains cached models
- `POST` refresh when upstream returns error → 200, fragment contains error message but no crash
- `GET` after failed refresh → still returns previously cached models (silent fallback)
- `GET /v1/providers/nonexistent/models` → 404

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/web/ -run "TestFetchModels|TestRefreshModels" -race -count=1` passes
- `go test ./proxy/... ./proxy/web/... -race -count=1` — no regressions
- `mage lint` passes

#### Manual Verification:

- `mage run` → open Providers page → browser dev tools show no template-load errors
- Direct `curl http://localhost:8083/v1/providers/{name}/models` returns HTML fragment (not 500)

---

## Phase 3: UI Surfaces — Providers Button + Mappings Datalist

### Overview

Add the "Fetch models" button to the Providers table Actions cell and the `<datalist>` autocomplete to the mapping form's `model_string` input. Both surfaces use the same endpoints from Phase 2, wired via htmx attributes — no custom JavaScript.

### Changes Required:

#### 3.1 "Fetch models" button in providers table

**File**: `proxy/web/templates/providers.html`

**Intent**: Add a "Fetch models" button to each provider row's Actions cell, plus a target `<div>` for the fragment swap.

**Contract**:
- Add an 8th column `<th>Models</th>` to the table header
- In each row, add `<td id="models-{{.Name}}"></td>` after the Actions cell (or inside it)
- In the Actions cell, add a `<button class="btn-sm" hx-post="/v1/providers/{{.Name}}/models/refresh" hx-target="#models-{{.Name}}" hx-swap="innerHTML">Fetch models</button>` before the Edit button
- The button uses `hx-post` to trigger the refresh endpoint; the fragment swaps into the row's models column

#### 3.2 Datalist autocomplete on mapping form

**File**: `proxy/web/templates/mappings.html`

**Intent**: Add a `<datalist>` to the `model_string` input, populated by htmx GET when the provider select changes.

**Contract**:
- Add an empty `<datalist id="model-suggestions"></datalist>` element before or after the model input
- Add `list="model-suggestions"` attribute to the model `<input name="model_string">`
- Add `hx-get="/v1/providers/{selected}/models" hx-target="#model-suggestions" hx-trigger="change"` to the provider `<select>` — but since the URL depends on the selected value, use the htmx `hx-include` pattern or a small inline script that sets `hx-get` dynamically
- **Implementation note**: htmx doesn't natively support dynamic URL from select value in pure attributes. Two options: (a) use `hx-get="/v1/providers/models"` with `hx-include` and server-side inspection of the referer, or (b) add a 3-line inline script on `change` that sets `this.nextElementSibling.setAttribute('hx-get', '/v1/providers/' + this.value + '/models')` then triggers htmx. The plan leaves this choice to the implementer, preferring the minimal approach that works with the vendored htmx version.

#### 3.3 Update `providerRow` if needed

**File**: `proxy/web/types.go`

**Intent**: No changes required — the providers template already has access to `.Name` via the template range. The models column is an independent htmx target, not part of the providerRow struct.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `go vet ./...` passes

#### Manual Verification:

- Open Providers page → each row shows a "Fetch models" button → click it → model list appears inline in that row
- Click "Fetch models" again → list refreshes with updated "Fetched X ago" timestamp
- Open Mappings page → click "Add Mapping" → select a provider that has fetched models → type in the model input → see model suggestions from the datalist
- Select a provider with no fetched models → datalist is empty, user can still type freely
- Test with a provider whose API key is unset → click Fetch → error message appears inline
- Test while offline → click Fetch → error message appears, existing cached models remain in datalist

---

## Testing Strategy

### Unit Tests:

- `deriveModelsURL`: all URL variants from research table (groq, deepseek, anthropic, zen, go, ollama, no-suffix, invalid URL)
- `ModelsCache`: Set/Get round-trip, miss returns zero, concurrent reads (race detector)
- `fetchModels`: httptest server returning OpenAI-style JSON, Anthropic-style JSON, error responses, empty data array
- Handler endpoints: cold cache GET, POST refresh with upstream, GET after refresh, POST refresh error, nonexistent provider

### Integration Tests:

- Full htmx flow: POST refresh → GET cache read → data consistency between the two
- Graceful degradation: missing API key, unreachable upstream, empty base URL

### Manual Testing Steps:

1. `mage run` → open dashboard in browser with `FREEDIUS_UI_TOKEN` set
2. Navigate to Providers page → verify "Fetch models" button appears on each row
3. Click "Fetch models" on a provider with a valid API key → verify models appear
4. Navigate to Mappings → open Add Mapping dialog → select that provider → verify datalist populates
5. Type a custom model ID not in the list → verify the form accepts it
6. Click "Fetch models" on a provider with no API key → verify error message appears
7. Turn off network / use invalid base URL → verify error handling doesn't crash

## Performance Considerations

- The fetcher uses a dedicated `*http.Client` with a 10s timeout — the models endpoint responds in <1s for all known providers
- Cache is in-memory with RWMutex — reads are lock-free under RLock, writes are rare (user-initiated button clicks)
- No background goroutines, no timers, no polling — zero CPU overhead when idle
- Fragment template is parsed once and cached, like page templates

## References

- Research: `context/changes/provider-model-discovery/research.md`
- Change identity: `context/changes/provider-model-discovery/change.md`
- Deferred Decision 5: `context/archive/zen-go-adapters/research.md:660`
- Web UI stack: `context/archive/2026-07-02-web-ui/research.md:38-40`
- Lessons: `context/foundation/lessons.md` (Adapter Return Contract, x-api-key requirement)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Core Domain

#### Automated

- [x] 1.1 `go test ./proxy/ -run "TestDeriveModelsURL|TestModelsCache|TestFetchModels" -race -count=1` passes — 28 tests pass

### Phase 2: Web Endpoints + Fragment Templates

#### Automated

- [x] 2.1 `go test ./proxy/web/ -run "TestFetchModels|TestRefreshModels" -race -count=1` passes — e4d6f68
- [x] 2.2 `go test ./proxy/... ./proxy/web/... -race -count=1` — no regressions — e4d6f68
- [x] 2.3 `mage lint` passes — e4d6f68

#### Manual

- [ ] 2.4 `mage run` loads the dashboard without template errors
- [ ] 2.5 Direct `curl` to `/v1/providers/{name}/models` returns HTML fragment (not 500)

### Phase 3: UI Surfaces

#### Automated

- [x] 3.1 `mage lint` passes
- [x] 3.2 `go vet ./...` passes

#### Manual

- [ ] 3.3 "Fetch models" button appears on each provider row and works
- [ ] 3.4 Mapping form datalist populates on provider selection
- [ ] 3.5 Error handling: missing API key, unreachable upstream, offline — all show errors inline
- [ ] 3.6 Custom model ID typing still works alongside datalist
