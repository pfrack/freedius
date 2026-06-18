# Frame Brief: TUI — Error Detail Expansion + Provider-Level Defaults

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The user described two gaps in the TUI:
1. Cannot expand a log entry to see full error details — the one-line request log truncates error messages and provides no detail view.
2. Cannot set provider-level defaults (base_url, api_key_env) — must repeat the same values on every model/mapping entry that uses the same provider.

## Initial Framing (preserved)

- **User's stated cause or approach**: "Expand any log I want" / "change config, edit providers and mapping" — both presented as missing features to build. Referenced URL replacement as already handled by providers.
- **User's proposed direction**: Build log expansion and config editing of providers/mappings.
- **Pre-dispatch narrowing**:
  - Log expansion scope: initially "Full request/response bodies" → narrowed to "I want to see error, normal request is not so needed for me"
  - Config editing scope: "Edit only providers separately and mapping separately" → narrowed to "Yes, provider-level defaults"

## Dimension Map

### Dimension Group A: Log/Error Detail Expansion

The observation could originate at any of these dimensions:

1. **Error data already captured, UI truncates it** — `RequestEvent.ErrorMessage` and `ErrorType` are already populated by `EventBusMiddleware` (`proxy/proxy.go:477-478`) from `X-Freedius-Error-*` response headers set by `writeErrorJSON` (`proxy/proxy.go:290-291`) and `writeAnthropicError` (`proxy/errors.go:34-35`). The data is in the ring buffer, but `renderRequestsTab` truncates it to 80 chars (`views.go:74`). No expand/detail view exists to show the full value.

2. **Privacy NFR blocks body capture** — `proxy/proxy.go:1-5` forbids logging request/response payloads. This was the blocker for "full request/response bodies" but is irrelevant to error detail expansion since error data is already captured at the metadata level (via headers, not body capture).

3. **TUI lacks a detail/expand panel** — The Dashboard model has no "selected request" state, no detail view rendering, and no expand/collapse key binding. The current update loop (`model.go:119-171`) handles only tab switching, form editing, and ring-buffer append — no request selection. ← user's current framing: this gap needs filling

### Dimension Group B: Provider-Level Defaults

1. **Provider defaults are generated code, not user YAML** — Provider-level defaults (`base_url`, `api_key_env`) exist only in `config/providers_gen.go:21-35` (generated from `providers.yaml`). `Config` struct has no `Providers` field (`config/config.go:16-19`). Users must duplicate `base_url` and `api_key_env` on every model/mapping entry. ← user's framing

2. **Config struct is extensible** — Adding a `Providers map[string]ProviderDefaults` field to `Config` is backward-compatible (`omitempty`). The `applyEntryDefaults` merge logic at `config/providers_gen.go:71-95` already handles the inheritance pattern — it just needs to consult user defaults before generated defaults.

3. **Per-entry editing already exists** — e/a/d on the Config tab (`model.go:393-437`, `model.go:452-535`) handles full CRUD for individual model/mapping entries. This does NOT need to change. Provider-level editing is additive.

4. **Providers tab is read-only** — `renderProvidersTab` (`views.go:90-115`) displays a summary table with no edit capability. Making it editable or adding a new editing surface is needed.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Error messages are not captured at all | `proxy/proxy.go:477-478` — `EventBusMiddleware` reads `X-Freedius-Error-Type` and `X-Freedius-Error-Message` headers. `proxy/errors.go:34-35` — `writeAnthropicError` sets both headers. `proxy/proxy.go:290-291` — `writeErrorJSON` sets both headers. Error data IS captured. | **NONE** |
| Error messages are captured but truncated in TUI | `proxy/tui/views.go:74` — `truncate(e.ErrorMessage, 80)`. No expand action exists on the Requests tab. | **STRONG** |
| Full request/response bodies needed for debugging | User narrowed: "I want to see error, normal request is not so needed." Error detail (already captured) is the real need. | **NONE (user refuted)** |
| Provider defaults are user-editable | `config/config.go:16-19` — `Config` struct has no `Providers` field. `config/providers_gen.go:21-35` — `knownProviderDefaults` is compiled-in Go map, not user YAML. No `providers:` section exists in `freedius.yaml`. | **NONE** |
| Provider-level defaults are architecturally feasible | `config/config.go` — `Config` struct is extendable with an `omitempty` field. `config/providers_gen.go:71-95` — `applyEntryDefaults` merge pattern already exists, just needs to consult user defaults before generated defaults. New `ProvidDefaults` struct would hold `BaseURL` and `APIKeyEnv`. | **STRONG** |
|  |  |  |

## Narrowing Signals

Decisive observations from Step 4 that narrowed the hypothesis space:

- **"I want to see error, normal request is not so needed"** — rules out full body capture, confirms error-only expansion. Error data is already captured; the gap is pure TUI display.
- **"Yes, provider-level defaults"** — confirms the real gap is `base_url`/`api_key_env` deduplication at the provider level, not splitting the entry list or reworking the existing e/a/d form.

## Cross-System Convention

- **Error display**: `tui-config-setup` Phase 1 added error message capture and display. The manual verification checkbox 1.6 ("Send unknown-model request in TUI — see error message instead of just red status") is still unchecked in the plan progress section, suggesting the feature exists but may not have been user-verified end-to-end.
- **Error code collapse**: `error-code-collapse/frame.md` identified that multiple error conditions collapse to 529 "overloaded_error," reducing the usefulness of `ErrorType` in `RequestEvent`. If error expansion shows a detail view, users would see the full error message text even when the status code is ambiguous — making expansion more valuable.
- **Config editing**: `tui-config-setup` implemented per-entry CRUD on the Config tab. Provider-level defaults build on the same `Config.Save()` / validation pipeline, making this a natural extension.
- **Lessons**: `lessons.md:15-19` (custom→mix rewrite) applies — any provider-level defaults editor must handle alias providers (zen, go, custom) correctly, using `OriginalProvider` for round-trip.

## Reframed (or Confirmed) Problem Statement

> **The actual problems to plan around are**: (1) Error messages are fully captured in `RequestEvent` but the TUI has no detail/expand view — users see only a truncated 80-char snippet with no way to read the full error. (2) Provider-level defaults (`base_url`, `api_key_env`) cannot be set by the user — they must repeat the same values on every model/mapping entry that shares a provider.

The initial framing of "expand any log to see full request/response bodies" and "edit config providers/mappings" was partially wrong. The real scope is much narrower:
- Log expansion → error-only detail view. No body capture needed. No privacy concerns. Pure TUI display change.
- Config editing → provider-level defaults. Per-entry editing already exists (e/a/d). The gap is a `Providers` map in `Config` and a corresponding editing surface.

If addressed:
- Users can press a key (e.g., Enter) on a red/orange log entry to open a detail panel showing full error message, error type, status code, all metadata.
- Users can set `base_url` and `api_key_env` once per provider in the TUI, and all matching entries inherit those values — overriding only when a per-entry value is explicitly set.

## Confidence

- **HIGH** — strong narrowing signals from user directly refuting the initial framing. Error data is confirmed captured (evidence at `proxy/proxy.go:477-478`, `proxy/tui/views.go:74`). Provider defaults are confirmed absent from user YAML (evidence at `config/config.go:16-19`, `config/providers_gen.go:21-35`). Both problems are well-bounded and architecturally straightforward.

## What Changes for /10x-plan

The plan should address two independent features: (1) a detail/expand panel for error entries in the Requests tab (select with j/k, expand with Enter, showing full `ErrorMessage`, `ErrorType`, and all `RequestEvent` fields), and (2) a `Providers` section in `Config` for per-provider base_url/api_key_env defaults, merged into `applyEntryDefaults`, with an editable view in the TUI (either reworking the Providers tab or adding a new editing surface). These touch different code layers (TUI display vs config struct) but share the TUI tab navigation surface.

## References

- Source files:
  - `proxy/tui/views.go:74` — error message truncated to 80 chars
  - `proxy/tui/views.go:50-84` — `renderRequestsTab` one-line layout
  - `proxy/tui/model.go:67-89` — `Dashboard` struct (no request selection)
  - `proxy/proxy.go:477-478` — `EventBusMiddleware` error header reads
  - `proxy/proxy.go:290-291` — `writeErrorJSON` sets error headers
  - `proxy/errors.go:34-35` — `writeAnthropicError` sets error headers
  - `config/config.go:16-19` — `Config` struct (no `Providers` field)
  - `config/providers_gen.go:21-35` — `knownProviderDefaults` (generated, not user YAML)
  - `config/providers_gen.go:71-95` — `applyEntryDefaults` merge pattern
- Related changes:
  - `context/changes/tui-config-setup/` — per-entry CRUD, error message capture (Phase 1)
  - `context/changes/error-code-collapse/` — error code differentiation
  - `context/changes/tui-dashboard/` — original TUI dashboard
- Lessons: `context/foundation/lessons.md:15-19` — alias rewrite (zen/go/custom → mix)
- Investigation tasks: ses_1243d8e5cffeRrHl0VnAAYOm0C, ses_1243d7de3ffeCEnZ6j2w6GteQv
