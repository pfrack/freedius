---
date: 2026-07-05T17:57:29+02:00
researcher: pfrack
git_commit: 5e460b0bcc578eafba5ae973292dfb3a4cf816ae
branch: main
repository: freedius
topic: "UI button to fetch/list a provider's models (OpenAI/Anthropic style), cache them, and refresh — to ease mapping configuration"
tags: [research, codebase, web-ui, providers, mappings, models-discovery, caching, htmx]
status: complete
last_updated: 2026-07-05
last_updated_by: pfrack
last_updated_note: "Added follow-up research: consolidate model fetch UI onto the mapping modal only"
---

# Research: Provider model discovery UI — fetch, cache, and refresh model lists

**Date**: 2026-07-05T17:57:29+02:00
**Researcher**: pfrack
**Git Commit**: 5e460b0bcc578eafba5ae973292dfb3a4cf816ae
**Branch**: main
**Repository**: freedius

## Research Question

Add a UI with a button to show the available models for a provider (OpenAI-style
or Anthropic-style API), so the user can more easily configure mappings; also
cache the fetched models and eventually refresh them.

## Summary

This is a **greenfield feature** — a whole-repo grep for `/models`, `/v1/models`,
`ListModels`, `list models` returns **zero** hits. No model-discovery, catalog, or
`/models` upstream call exists anywhere today. The one prior mention is a
consciously-deferred idea in `context/archive/zen-go-adapters/research.md:660`
(Decision 5: "S-03 does NOT auto-fetch the model list … The user maintains their
config manually"), earmarked for a future `freedius init`. This change realizes
that deferred intent, inside the web dashboard.

Everything needed already has a house pattern to copy:

- **Upstream auth** — the OpenAI adapter reads the key from env and sets
  `Authorization: Bearer` (`proxy/openai_compat.go:77,134-138`); the Anthropic
  adapter sets `x-api-key` + `anthropic-version` (`proxy/anthropic_compat.go:78-92`).
  Behavior (`openai`/`anthropic`/`mix`) selects which shape.
- **URL derivation** — the mix adapter's `normalizeBaseURL` suffix-swap
  (`proxy/mix.go:84-102`) is exactly the logic to turn a stored
  `…/chat/completions` or `…/v1/messages` base URL into a `…/models` URL.
- **Concurrency-safe cache** — `config.Config`, `EventBus`, and `LogSink` are all
  built as an `sync.RWMutex` + map + `Snapshot()` copy-out (`config/config.go:28-154`,
  `proxy/eventbus.go:33-45`, `proxy/logtee.go:26-39`). A `ModelsCache` is a drop-in
  of that idiom.
- **Web wiring** — routes register in `SetupMux` (`proxy/web/handlers.go:20-73`)
  and are auto auth-gated by the single boundary at `proxy/web/server.go:38-41`;
  htmx button→fragment-swap is already used for the log filter
  (`proxy/web/templates/logs.html:6-12`).

**Recommended shape:** an in-memory, mutex-guarded `ModelsCache` in `proxy/`,
injected via `eventstream.Handlers`, fed by a `GET /v1/providers/{name}/models`
(read cache) + `POST /v1/providers/{name}/models/refresh` (force re-fetch) handler
pair that returns a **self-contained htmx fragment**. Surface it two ways: a
per-provider "Fetch models" button on the Providers page, and a `<datalist>`
autocomplete behind the mapping form's `model_string` input. Defer on-disk
persistence to a follow-up (the list is always re-fetchable, so disk is a cold-start
nicety, not a requirement). Do **not** introduce TTL/auto-expiry — the codebase has
none and its refresh model is explicit user action.

**One landmine (must address first):** `renderProvidersTable`/`renderMappingsTable`
load `providers-table.html` / `mappings-table.html`
(`proxy/web/handlers.go:274,318`) — **these partials do not exist** in
`proxy/web/templates/`. Every write handler's `HX-Request` branch currently 500s at
template-load time; only the JSON path is tested. A new fetch feature must either
create self-contained fragment templates or fix these partials first (see
Architecture Insights).

## Detailed Findings

### 1. Web UI architecture — where the button/endpoint/list plug in

**Routing** — `SetupMux`, `proxy/web/handlers.go:20-73`, uses Go 1.22
`http.ServeMux` method+pattern syntax. Existing surface:

| Pattern | Handler | Ref |
|---|---|---|
| `GET /providers` | `handleProviders` | handlers.go:43 |
| `GET /mappings` | `handleMappings` | handlers.go:46 |
| `POST /v1/providers` | `handleCreateProvider` | handlers.go:51 |
| `PUT /v1/providers/` | `handleUpdateProvider` | handlers.go:54 |
| `DELETE /v1/providers/` | `handleDeleteProvider` | handlers.go:57 |
| `POST /v1/mappings` … `PUT/DELETE /v1/mappings/` | create/update/delete mapping | handlers.go:62-68 |

- The codebase strips trailing-slash path names manually via `pathName(r, "/v1/providers/")` (`proxy/web/forms.go:184`) and does **not** yet use `{name}` wildcards — but Go 1.22 `ServeMux` supports them (`r.PathValue("name")`), and more-specific patterns win precedence, so `GET /v1/providers/{name}/models` coexists cleanly with `PUT /v1/providers/`.
- **Auth is a single boundary around the whole mux** (`proxy/web/server.go:38-41`, `handler = h.RequireAuth(mux)` when `FREEDIUS_UI_TOKEN` set) — any new route is auth-gated for free.

**Handler pattern** — read-side canonical is `handleProviders`
(`proxy/web/handlers.go:136-160`): build rows from `cfg.*Snapshot()`, then
content-negotiate on `r.Header.Get("HX-Request") == "true"` to return a fragment vs
a full page. A `handleFetchModels` follows this **read** shape (it queries the cache
/ upstream, it doesn't mutate config), not the CRUD write shape at
`proxy/web/handlers.go:334-382`.

**htmx button → list swap** — the exact idiom is the log-level filter
(`proxy/web/templates/logs.html:6-12`: `hx-get="/logs" hx-target="#log"
hx-trigger="change"`) and the CRUD forms
(`proxy/web/templates/providers.html:53-58`: `hx-post … hx-target="#providers"
hx-swap="outerHTML"`). A per-row fetch button:

```html
<button class="btn-sm" hx-get="/v1/providers/{{.Name}}/models"
        hx-target="#models-{{.Name}}" hx-swap="innerHTML">Fetch models</button>
<td id="models-{{.Name}}"></td>
```

Server returns an HTML fragment; htmx swaps it. No JS.

**Templates** — `proxy/web/embed.go`: `//go:embed templates static`; `loadPageTemplate`
does `template.ParseFS(assets, "templates/layout.html", "templates/"+pageFile)` and
caches one `*template.Template` per page in a `sync.Map` (`embed.go:26-61`). Pages
override `{{define "content"}}` blocks (`layout.html:17`). A models fragment should be
a **self-contained** template (its own file, or a `{{define "models-fragment"}}`
executed directly) so it dodges the missing-partials bug.

**View models** — `proxy/web/types.go`: `providerRow{Name, Behavior, BaseURL,
APIKeyEnv, Protocol, MappingCount}` (`types.go:29`), `mappingsData{pageData;
Mappings []mappingRow; Providers []providerRow}` (`types.go:52`). Add a flat
`modelsData{Provider string; Models []ModelView; FetchedAt string; Error string}`
following this one-struct-per-fragment convention.

**Styling** — `proxy/web/static/app.css`: reuse `.btn-sm` (`app.css:199`) for the
row button, bare `table`/`<ul>` styles (`app.css:69-87`) for the list, `.form-error`
(`app.css:153`) for the error state. No new CSS required.

**Tests** — `newWriteMux(t)` (`proxy/web/handlers_write_test.go:23`) writes a real
YAML to `t.TempDir()`, `config.Load`s it (seed `testConfigYAML` has a real `nim`
provider), and returns `(mux, cfg, cfgPath)`. New endpoint tests point a provider's
base URL at an `httptest.Server` returning canned `/models` JSON, then assert the
fragment body.

### 2. Provider config + upstream request mechanics

**Config schema** — `config/config.go:37-54`:

```go
type Provider struct {
    Behavior         string // openai | anthropic | mix
    DefaultBaseURL   string
    DefaultAPIKeyEnv string
    AnthropicVersion string
    Protocol         string // mix only: openai|anthropic
    RequireBaseURL   bool   // runtime-only
    SupportsCountTokens bool // runtime-only
}
```

There is **no per-provider `base_url`/`api_key_env`** field — effective values live
in `DefaultBaseURL`/`DefaultAPIKeyEnv`, merged by `applyDefaults`
(`config/defaults.go:16-44`) from the codegen'd `providerDefaults`
(`config/providers_gen.go`, generated from `providers.yaml` by
`internal/genproviders/main.go`). `Mapping` is just `{ProviderName, ModelString}`
(`config/config.go:58-61`).

**Resolution** — read a provider with `cfg.ProvidersSnapshot()` or
`RLock`+`Providers[name]` (`config/config.go:134-142`). The API key is read from env
**inside the adapter**: `os.Getenv(provider.DefaultAPIKeyEnv)`
(`proxy/openai_compat.go:77`, `proxy/anthropic_compat.go:55`).

**Upstream auth headers (copy per behavior):**

- OpenAI — `proxy/openai_compat.go:134-138`: `Authorization: Bearer <key>` (only if key non-empty).
- Anthropic — `proxy/anthropic_compat.go:84-92`: `x-api-key: <key>` + `anthropic-version: <ver>` (default `"2023-06-01"`, `:78-81`); strips any `Authorization`.
- mix — `proxy/mix.go:49-79`: no auth of its own; rewrites base URL then delegates to the openai or anthropic sub-adapter.

**`behavior` values** — validated to `openai | anthropic | mix` at
`config/config.go:196-207`; `Protocol` (mix only) is `openai|anthropic`
(`:242-249`). Branch the models fetch on behavior: `anthropic` → Anthropic shape;
`openai` → OpenAI shape; `mix` → use `Protocol` if set, else infer from base-URL
path suffix (mirror `proxy/mix.go:56-78`: `/v1/messages` suffix → anthropic, else
openai).

**Deriving the `/models` URL** — stored base URLs are full endpoint paths, so the
path suffix must be swapped. Reuse/extract `MixAdapter.normalizeBaseURL`
(`proxy/mix.go:84-102`) — a pure `url.Parse` + suffix-swap helper with no adapter
state:

| Provider (example base URL) | Derived models URL |
|---|---|
| groq `https://api.groq.com/openai/v1/chat/completions` | `https://api.groq.com/openai/v1/models` |
| deepseek `https://api.deepseek.com/chat/completions` (no `/v1`!) | `https://api.deepseek.com/models` |
| anthropic `https://api.anthropic.com/v1/messages` | `https://api.anthropic.com/v1/models` |
| zen (mix) `https://opencode.ai/zen/v1/messages` | `https://opencode.ai/zen/v1/models` |
| go (mix) `https://opencode.ai/zen/go/v1/chat/completions` | `https://opencode.ai/zen/go/v1/models` |
| ollama `http://localhost:11434/v1/chat/completions` | `http://localhost:11434/v1/models` |

Rule: parse URL → strip a trailing `/chat/completions` or `/messages` suffix →
append `/models`. `normalizeBaseURL` handles only one "other" suffix at a time, so a
dedicated `deriveModelsURL(baseURL)` handling both suffixes is cleanest. **Watch the
deepseek edge case** (no `/v1` segment).

**HTTP client** — there is **no shared client**. `OpenAICompatibleAdapter` builds
its own `*http.Client` in its constructor (`proxy/openai_compat.go:40-51`: 30s
dial/keepalive, `Proxy: http.ProxyFromEnvironment`); the field is unexported. A
models fetch should construct a small dedicated client with a **short** timeout
(a models GET is cheap — unlike the 5-min streaming timeout), optionally copying the
transport config.

### 3. External `/v1/models` API shapes (for parsing)

**OpenAI-style** `GET /v1/models`, `Authorization: Bearer <key>`:

```json
{ "object": "list",
  "data": [ { "id": "gpt-4o", "object": "model", "created": 1715367049, "owned_by": "openai" } ] }
```

**Anthropic-style** `GET /v1/models`, `x-api-key: <key>` + `anthropic-version: 2023-06-01`
(confirmed against current docs, 2026-07-05):

```json
{ "data": [ { "id": "claude-opus-4-6", "type": "model",
              "display_name": "Claude Opus 4.6", "created_at": "2026-02-04T00:00:00Z",
              "capabilities": { … }, "max_input_tokens": 0, "max_tokens": 0 } ],
  "first_id": "…", "has_more": true, "last_id": "…" }
```

Both expose `data[].id`, so a **minimal common parser** — `{ "data": [ { "id":
string, "display_name": string /*optional, anthropic*/ } ] }` — covers all 16
providers. Anthropic's `display_name` is a nice-to-show label; OpenAI has none
(fall back to `id`). Anthropic paginates (`has_more`/`limit` up to 1000, default
20); for freedius's use a single page (raise `limit`, ignore pagination) is enough —
note it in the plan if any provider truncates.

### 4. Caching, state, and refresh

**No cache exists** — this is net-new. **No TTL/expiry/stale machinery exists
anywhere** (grep of `time.Now/Since/TTL/stale` finds only latency and uptime).
The codebase's refresh model is **explicit user action** (CRUD edits re-render an
htmx fragment), not time-based invalidation.

**House concurrency pattern** (three precedents): `config.Config`
(`config/config.go:28-33`), `EventBus` (`proxy/eventbus.go:33-45`), `LogSink`
(`proxy/logtee.go:26-39`) — all `sync.RWMutex` + map, reads under `RLock`, writes
under `Lock`, with a `Snapshot()` copy-out so renderers iterate lock-free. A
`ModelsCache` is a direct copy:

```go
type ModelView struct { ID, DisplayName string }
type modelsEntry struct { Models []ModelView; FetchedAt time.Time; Err string }
type ModelsCache struct {
    mu      sync.RWMutex
    entries map[string]modelsEntry // keyed by provider name
}
// Get(name) (modelsEntry, bool)  — RLock
// Set(name, entry)               — Lock
```

`FetchedAt` is stored **only to display** "last refreshed 3m ago", never to
auto-expire.

**Config lifecycle / persistence precedent** — config is mutated in memory under the
write lock **and** written back to YAML on every edit, with rollback on save failure
(`proxy/web/handlers.go:345-372`, repeated 6×). Atomic write helper `Config.SaveData`
(backup → temp → rename → restore-on-failure) already exists (`config/config.go:332-375`),
and the config dir convention is `os.UserConfigDir()/freedius/`
(`cmd/freedius/main.go:430-434`). So **if** on-disk cache is wanted later, the
infrastructure is all there (`~/.config/freedius/models-cache.yaml`, write-through on
refresh, treat missing/corrupt as "empty, needs fetch"). But the model list is always
re-fetchable, so disk persistence is a cold-start optimization — **defer it**.

**Wiring** — `cmd/freedius/main.go` `run()` constructs `cfg`, registry, dispatcher,
bus, then aggregates into `h := &eventstream.Handlers{Bus, LogSink, Cfg, Registry,
… CfgPath}` (`main.go:158-168`) which the web mux closes over. Add a `ModelsCache`
field to `eventstream.Handlers` (`internal/eventstream/handlers.go:24-34`),
construct it in `main.go` (~`:148`), and every web handler reaches it via
`h.ModelsCache` exactly like `h.Cfg`/`h.CfgPath` today.

**Event bus** — `EventBus`'s `RequestEvent` (`proxy/eventbus.go:13-27`) is
proxy-request-specific; a live "models fetched" SSE push would need to overload it or
add a second bus. **Overkill** — the htmx request/response fragment already updates
the UI. Skip the bus for v1.

### 5. Mappings UX — how a fetched list feeds in

The mapping form has **3 fields** — `name`, `provider_name`, `model_string`
(`proxy/web/templates/mappings.html:56-63`). `provider_name` is already a safe
`<select>` populated from `.Providers` (`mappings.html:59-61`); `model_string` is a
free-text `<input required>` (`:63`) — **the one error-prone field**, and the target
for assistance. Validation forbids CR/LF/colon in model_string
(`proxy/web/forms.go:173-181`) and requires provider_name to exist
(`:100-102`).

**Recommended: a `<datalist>` autocomplete on the model input**, populated per
selected provider. It keeps `model_string` a text input — preserving the ability to
type a model the API doesn't list (the config layer explicitly allows arbitrary model
strings, and offline/air-gapped Docker is a first-class use case,
`context/archive/2026-07-02-web-ui/research.md:196-201`) — while offering discovered
models as suggestions. htmx-native: on provider-select `change`,
`hx-get="/v1/providers/{name}/models"` swaps the `<datalist><option>`s. This matches
the deferred `zen-go-adapters` Decision-5 philosophy ("manual config, but assisted").

**Rejected: a hard `<select>` of fetched models** — it would forbid unlisted/custom
model strings the config layer permits and break when `/models` is unavailable.

The **per-provider "Fetch models" button** belongs in the Providers table Actions
cell (`proxy/web/templates/providers.html:30-46`, alongside Edit/Delete) — each row
already carries `Name`/`BaseURL`/`APIKeyEnv`/`Behavior`. It's the natural
fetch/refresh trigger and populates the cache the mapping-form datalist then reads.

## Code References

- `config/config.go:37-61` — `Provider` and `Mapping` structs (no per-provider base_url/api_key).
- `config/config.go:28-33,121-154` — `Config` RWMutex + `Snapshot()` copy-out (the cache idiom to copy).
- `config/config.go:332-375` — `SaveData` atomic backup→temp→rename (reusable if on-disk cache added).
- `config/defaults.go:16-44` — `applyDefaults` fills `DefaultBaseURL`/`DefaultAPIKeyEnv`.
- `config/providers_gen.go` — codegen'd `providerDefaults` (base URLs are full endpoint paths).
- `providers.yaml` — 16 providers, source of truth; `behavior` + `default_base_url`.
- `proxy/openai_compat.go:40-51,77,116-138` — own `*http.Client`, env-key read, `Authorization: Bearer`, per-request `context.WithTimeout`.
- `proxy/anthropic_compat.go:55,78-92` — env-key read, `x-api-key` + `anthropic-version`, strips `Authorization`.
- `proxy/mix.go:49-79,84-102` — behavior→sub-adapter dispatch + `normalizeBaseURL` suffix-swap (reuse for `/models` URL derivation).
- `proxy/eventbus.go:33-45`, `proxy/logtee.go:26-39` — RWMutex+map+Snapshot precedents for `ModelsCache`.
- `proxy/web/handlers.go:20-73` — `SetupMux` route registration (add `GET`/`POST /v1/providers/{name}/models[/refresh]`).
- `proxy/web/handlers.go:136-160` — `handleProviders` read+HX-negotiation shape to model `handleFetchModels` on.
- `proxy/web/handlers.go:274,318` — **missing-partials landmine** (`providers-table.html`/`mappings-table.html` don't exist).
- `proxy/web/handlers.go:345-372` — mutate→marshal→save→rollback write-back pattern.
- `proxy/web/server.go:38-41` — whole-mux auth boundary (`RequireAuth`).
- `proxy/web/embed.go:26-61` — `loadPageTemplate` / `ParseFS` / `renderPage`.
- `proxy/web/types.go:29,45,52` — `providerRow`/`mappingRow`/`mappingsData` view models.
- `proxy/web/templates/providers.html:30-46` — Actions cell (home for the "Fetch models" button).
- `proxy/web/templates/mappings.html:56-63` — 3-field form; `model_string` free-text input (datalist target).
- `proxy/web/templates/logs.html:6-12` — htmx button/select → target-swap idiom to copy.
- `proxy/web/static/app.css:153,199` — `.form-error`, `.btn-sm` reuse.
- `proxy/web/handlers_write_test.go:17-23` — `newWriteMux(t)` test harness for new endpoint tests.
- `internal/eventstream/handlers.go:24-44,77` — `Handlers` struct (add `ModelsCache` field) + `RequireAuth`.
- `cmd/freedius/main.go:146-169,430-434` — startup wiring / injection point; `UserConfigDir` convention.

## Architecture Insights

- **Behavior is the single switch** for both auth header and response parser
  (`openai` → Bearer + `{data:[{id}]}`; `anthropic` → `x-api-key`+version +
  `{data:[{id,display_name}]}`; `mix` → resolve via `Protocol`/URL-suffix first).
  One small fetcher with a per-behavior branch covers all 16 providers because both
  wire formats share `data[].id`.
- **URL derivation is the only genuinely fiddly bit.** Stored base URLs are endpoint
  paths, not API roots. A pure `deriveModelsURL(baseURL)` (parse → strip
  `/chat/completions`|`/messages` → append `/models`) mirrors the existing
  `normalizeBaseURL`/`supportsCountTokens` suffix logic; unit-test it against the
  table above including deepseek's `/v1`-less path.
- **Cache = config's twin, minus persistence and TTL.** Copy the
  RWMutex+map+`Snapshot()` shape verbatim; store `FetchedAt` for display only. The
  codebase has no expiry concept — don't add one. Refresh is a button, not a timer.
- **Missing table partials are a pre-existing latent bug** on the htmx CRUD path.
  Keep the new fetch fragment **self-contained** (its own template file executed
  directly, not routed through `renderProvidersTable`) so the feature can't inherit
  the 500. Optionally fix the partials as a separate hygiene change.
- **Graceful degradation is mandatory** (offline/air-gapped is a design goal): a
  missing base URL (`require_base_url` providers before config), unset API-key env,
  or upstream error must render a friendly inline `.form-error` fragment — never a
  crash, and never block hand-typing a model string. Local providers
  (ollama/lmstudio) have no `DefaultAPIKeyEnv`; skip auth when the env name is empty.
- **Two surfaces, one backend.** The Providers-page button and the mapping-form
  datalist call the same `GET /v1/providers/{name}/models` (reads cache) and
  `POST …/refresh` (force fetch). Building the endpoint + cache once serves both.

## Historical Context (from prior changes)

- `context/archive/zen-go-adapters/research.md:84,109,660` — **Decision 5, "Out of
  scope: hot-reload of model lists"**: the `/v1/models` endpoints for Zen/Go were
  documented but auto-fetch was explicitly deferred to a future `freedius init`. This
  change picks up that deferred intent (in the web UI instead of `init`).
- `context/archive/2026-07-02-web-ui/research.md:38-40,196-201` — web dashboard
  stack decision: Go stdlib `html/template` + **vendored** htmx, zero runtime deps,
  single static binary, chosen for air-gapped Docker. Any model-fetch UI must
  degrade gracefully offline.
- `context/archive/2026-07-02-web-ui/plan.md:730-736` — mapping form was
  specified as a fixed 3-field form (name, provider dropdown, model text); models
  were always manual free-text. The provider dropdown being empty was a shipped bug
  later fixed (`reviews/impl-review.md:123-129`, confirmed
  `reviews/impl-review-2026-07-05.md:177`).
- `context/foundation/lessons.md` — relevant hard-won rules: **Adapter Return
  Contract** (return `nil` once any response is written), **Custom Provider x-api-key
  + anthropic-version required** (Anthropic needs `x-api-key`, not Bearer), **Adding
  New Providers** (`applyDefaults` only fills declared providers). The models fetcher
  inherits the same auth truths.

## Related Research

- `context/changes/provider-model-discovery/change.md` — this change's identity file.
- `context/archive/zen-go-adapters/research.md` — the deferred model-list-fetch decision.
- `context/archive/2026-07-02-web-ui/research.md` + `plan.md` — the web dashboard this feature extends.
- `context/foundation/roadmap.md` — no existing slice for model discovery; this is net-new (candidate backlog entry).

## Open Questions

1. **Pagination** — Anthropic `/v1/models` paginates (`has_more`, `limit` ≤ 1000,
   default 20). Single-page-with-high-limit is likely sufficient; confirm no target
   provider truncates a meaningful list. OpenAI-compatible providers vary (some
   return everything, some paginate) — decide whether to follow `has_more`/cursors or
   cap at one page and note the cap in the UI.
2. **Which providers actually expose `/models`?** All 16 nominally do (OpenAI-compat
   convention + Anthropic), but NIM, Cohere's `/compatibility` path, and Opencode
   Zen/Go should be spot-checked with `curl` during planning — the derived URL may
   differ for non-standard mounts.
3. **Endpoint verb split** — `GET` reads cache (empty until first fetch) vs `POST
   …/refresh` forces upstream. Alternative: a single `GET` that fetches-on-miss and
   `POST`/`?refresh=1` forces. Decide during planning; the datalist wants a cheap
   read, the button wants an explicit refresh.
4. **On-disk cache** — include in this change (write-through via `SaveData` to
   `~/.config/freedius/`) or defer to a follow-up? Recommendation: **defer** — the
   list is always re-fetchable; ship in-memory first.
5. **Route wildcard vs prefix-strip** — adopt Go 1.22 `{name}` wildcards
   (`r.PathValue`) for the new routes, or stay consistent with the codebase's
   `pathName` prefix-stripping convention? Minor; pick one for consistency.
6. **`freedius init` synergy** — the deferred Decision-5 idea was for `init` to write
   the model list into config. Should this change also expose the cached list to a
   future `init`, or stay UI-only? Out of scope for now; flag the seam.

## Follow-up Research 2026-07-05T19:08

### Research Question

Phases 1-3 of the original plan are implemented and committed (`e4d6f68`, `ffa1b13`,
epilogue `09819d0`), plus uncommitted local tweaks on top. Manual verification (plan.md
steps 2.4/2.5/3.3-3.6) surfaced UX feedback: the user wants the "Fetch models" button
**removed from the Providers page entirely** and consolidated **only** on the mapping
modal, where the `model_string` field should work in one of two explicit modes: free
typing (as today), or click "Fetch models" → an explicit, visible, clickable list
appears → picking an entry fills the input. Clarified via AskUserQuestion:

- **List UI**: a **custom visible list** (not the native `<datalist>` dropdown) —
  clicking "Fetch models" reveals a list of clickable model names inside the modal;
  clicking one fills `model_string` and the list hides.
- **GET endpoint**: **drop** `GET /v1/providers/{name}/models` (cache-only read) —
  nothing in the new design reads the cache without being willing to trigger a fetch;
  the mapping modal's only path becomes the explicit `POST …/refresh`.

### Current Implementation State (as of this research)

Three commits landed the original plan; four files then received **uncommitted**
local edits (`git diff --stat`: `handlers.go`, `app.css`, `mappings.html`,
`models-fragment.html`, `types.go`) that already started bending the fragment toward
dual-mode rendering — this in-progress work is directly relevant to the new direction
and mostly reusable.

**Providers page** (`proxy/web/templates/providers.html:8,31,34-41`) — an 8th `Models`
column with `<td id="models-{{.Name}}"></td>`, and a `btn-sm` "Fetch models" button
(`hx-post=".../models/refresh" hx-target="#models-{{.Name}}" hx-swap="innerHTML"`)
in the Actions cell, ahead of Edit/Delete. **This entire column + button must be
removed** per the new direction.

**Mappings modal** (`proxy/web/templates/mappings.html:56-96`) — current (already
locally modified) behavior:
- Provider `<select>` has an inline `onchange` that calls
  `htmx.ajax('POST', '/v1/providers/'+name+'/models/refresh?target=datalist', {target:'#model-suggestions', swap:'innerHTML'})` — i.e. **auto-fetches on every provider selection change**, no explicit user action.
- `editMapping(name, provider, model)` (used by the Edit button, `mappings.html:85-96`)
  does the same auto-refresh call when the modal opens for editing.
- The `model_string` input is `<input name="model_string" list="model-suggestions" required>`
  with an empty `<datalist id="model-suggestions"></datalist>` sibling, populated by the
  above calls.
- **All of this auto-trigger wiring must be removed/replaced** with an explicit "Fetch
  models" button + a custom clickable list, per the clarified direction. The provider
  `<select>` itself (`mappings.html:59-66`) stays — the button will read `this.form.elements.provider_name.value` or similar at click time, following the exact pattern already used in `editProvider`/`editMapping` (`providers.html:96-110`, `mappings.html:85-96`) where plain inline `onclick`/`onchange` JS reads sibling form fields — no framework, matches house style.

**Fragment template** (`proxy/web/templates/models-fragment.html`) — locally modified
to branch on a `DatalistMode bool` (new field, not yet committed): `true` → renders
bare `<option>` elements for the native datalist; `false` → renders a `<ul class="model-list">` of `<li><strong>{{.ID}}</strong> — {{.DisplayName}}</li>` plus a fetched-time/error footer (this is the Providers-page row-list rendering). **Neither branch as
written is directly reusable as a click-to-select list** — the `<li>` items in the
`false` branch have no `onclick`/identifying attribute to wire a click handler to, and
the `true` branch emits `<option>` (invalid outside a `<datalist>`/`<select>`). The new
custom-list mode needs a **third rendering shape**: clickable list items carrying the
model ID in a way JS can read on click (e.g. `<li onclick="...">`, or `data-model-id`
+ event delegation) that also closes/hides the list and writes into the form's
`model_string` field. This suggests replacing the boolean `DatalistMode` with a
tri-state (or simply repurposing the existing `false`/list branch, adding
`onclick="selectModel(this)"` to each `<li>`, and deleting the `DatalistMode`
true-branch and its call site).

**Handlers** (`proxy/web/handlers.go:649-745`):
- `handleFetchModels` (GET, `:649-675`) — cache-only read, **always sets
  `DatalistMode: true`**. Registered at `handlers.go:74-76`. Per the clarified
  direction this handler + its route registration should be **deleted entirely**
  (nothing in the new design does a cache-only read).
- `handleRefreshModels` (POST, `:677-`) — the actual upstream-fetch endpoint;
  reads `datalistMode := r.URL.Query().Get("target") == "datalist"` (a **local,
  uncommitted addition**) to decide which fragment branch to render. This is the one
  handler the new mapping-modal button will call; the `?target=datalist` query-param
  branch should be replaced by whatever signal selects the new "custom clickable
  list" rendering (could stay a query param, e.g. `?mode=picker`, or — since the
  Providers-page caller is going away — become the **only** rendering mode, letting
  `DatalistMode`/the query param be deleted outright since there is no longer a second
  consumer).
- `renderModelsFragment` (`:731-`) loads `models-fragment.html` via
  `loadFragmentTemplate` (`proxy/web/embed.go:31-`) and executes it standalone
  (bypasses `layout.html` and the missing-partials bug) — this plumbing is unaffected
  by the UI change and should be kept as-is.

**Styling** (`proxy/web/static/app.css:203-225`, uncommitted) — `.model-list`
(scrollable `<ul>`, max-height 130px, top-border between items) and `.text-muted`
were added for the Providers-page row list. These are directly reusable for the new
modal picker list (same visual shape: a small scrollable list under the input), no new
CSS classes strictly required, though the modal context may want the list positioned
as an absolutely-positioned dropdown-like panel rather than an inline table cell — a
design decision for planning, not research.

**Tests** (`proxy/web/handlers_models_test.go`, all 6 tests) — `newModelsWriteMux`
harness + `TestFetchModels_ColdCache`, `TestFetchModels_NamedNonexistent`,
`TestRefreshModels_WithUpstream`, `TestRefreshModels_AfterRefreshGetCached`,
`TestRefreshModels_UpstreamError`, `TestFetchModels_CachedAfterFailedRefresh`. Three
of these (`ColdCache`, `NamedNonexistent`, and the `GET` half of
`AfterRefreshGetCached`/`CachedAfterFailedRefresh`) directly exercise the `GET`
endpoint being removed — **these will need deletion or rewrite** to instead assert
against `POST …/refresh` twice (refresh → refresh again, checking the second response
reflects the cache), since there's no longer a separate read path to assert against.
`TestRefreshModels_WithUpstream` and `TestRefreshModels_UpstreamError` remain valid
as-is (they already test `POST …/refresh` behavior + graceful error rendering).

### Scope Implications for the Existing Plan

This is a **UI-surface-only reversal**, confined to Phase 3 (and the `?target=datalist`
sliver of Phase 2 that was added locally, not in the original committed plan). Phase 1
(core domain: `ModelsCache`, `deriveModelsURL`, `fetchModels`) is **entirely
unaffected** — no changes to `proxy/models.go` or its tests. Phase 2's committed
core (routes, `renderModelsFragment`, `loadFragmentTemplate`, `modelsData`) is mostly
kept; only the `GET` route/handler and the `DatalistMode`/`target` query-param branch
introduced by uncommitted local edits are being undone/reworked.

**Net file impact for the next plan revision:**
- `proxy/web/templates/providers.html` — remove `Models` column + "Fetch models"
  button (revert to the pre-Phase-3 7-column table).
- `proxy/web/templates/mappings.html` — remove auto-refresh `onchange`/`editMapping`
  calls; add an explicit "Fetch models" button + a hidden-by-default clickable list
  container near `model_string`; add a small `selectModel(li)`-style handler (or
  event-delegation) that fills the input and hides the list.
- `proxy/web/templates/models-fragment.html` — collapse to a single clickable-list
  rendering shape (drop the `DatalistMode` `<option>` branch); add per-item click
  affordance.
- `proxy/web/handlers.go` — delete `handleFetchModels` + its `GET` route; simplify
  `handleRefreshModels` to drop the `target=datalist` query-param branching (single
  rendering mode now).
- `proxy/web/types.go` — drop `DatalistMode` from `modelsData` (or repurpose,
  depending on final template design).
- `proxy/web/static/app.css` — likely reusable as-is; may want a couple of
  positioning rules if the list becomes an overlay rather than inline.
- `proxy/web/handlers_models_test.go` — delete/rewrite the 3 GET-touching tests.

### Open Questions (follow-up)

1. **List visibility toggle** — does the clickable list appear inline below the input
   (pushing form layout down) or as an absolutely-positioned overlay? No existing
   precedent in this codebase for an overlay dropdown; inline (matching the
   Providers-page `.model-list` style) is the lower-risk, zero-new-CSS choice.
2. **Repeat clicks** — should a second "Fetch models" click re-fetch upstream (like
   today's refresh-button semantics) or just re-show the already-fetched list? Given
   there's no cache-read path anymore, every click is necessarily a `POST …/refresh`
   — the only question is whether re-clicking after the list is already showing
   re-fetches or just toggles visibility.
3. **Empty/error states in the new list** — the current fragment's "No models fetched
   yet" / `.form-error` messaging (`models-fragment.html`) should carry over verbatim
   into the single new rendering shape.
