# TUI — Error Detail Panel + Provider-Level Defaults — Plan Brief

> Full plan: `context/changes/tui-error-detail-provider-defaults/plan.md`
> Frame brief: `context/changes/tui-error-detail-provider-defaults/frame.md`

## What & Why

Error messages are fully captured in `RequestEvent` but truncated to 80 chars in the TUI with no way to see the full text. Provider-level defaults (`base_url`, `api_key_env`) don't exist in user YAML, forcing users to repeat the same values on every model/mapping entry sharing a provider. This plan adds an error detail panel (Enter to expand) and a `providers:` YAML section with full TUI editing support.

## Starting Point

- Error data is already captured via `EventBusMiddleware` reading `X-Freedius-Error-*` headers — the gap is pure TUI display (no detail view, 80-char truncation).
- Per-entry e/a/d editing already works on the Config tab. The Providers tab is read-only. No `providers:` section exists in `Config` or `freedius.yaml`.
- Cursor pattern (`configCursor`) and form infrastructure (`formMode`/`formFields`/`submitForm`) are proven — this plan copies and extends them.

## Desired End State

- Requests tab: j/k selects a log entry; Enter on an error opens a detail panel with all `RequestEvent` fields (full error message, not truncated). Esc closes.
- Providers tab: j/k selects a provider; e edits its defaults (3-field form), a adds a new provider default, d deletes.
- `freedius.yaml`: new top-level `providers:` section. Merge order: per-entry > user provider default > generated default.
- Backward compatible — existing configs load unchanged.

## Key Decisions Made

| Decision | Choice | Why | Source |
| --- | --- | --- | --- |
| Expand keybinding | Enter on selection | Matches Config tab convention | Plan |
| Detail panel content | Full RequestEvent metadata | All fields already captured; gives full debugging context | Plan |
| Expand scope | Errors only (status >= 400) | User narrowed: "I want to see error, normal request is not so needed" | Frame |
| Provider defaults UX | Make Providers tab editable | Consistent e/a/d pattern, no new tab needed | Plan |
| Override semantics | Per-entry wins over provider default | Intuitive — explicit overrides implicit | Plan |
| Config YAML structure | Top-level `providers:` section | Parallel to `models:` and `mappings:`; clean, flat | Plan |
| Testing strategy | Unit test logic only | No rendering tests (ANSI output fragile); logic coverage is sufficient | Plan |
| User defaults vs generated | User defaults applied before generated | `applyEntryDefaults` does blank-fill — user defaults applied first are not overwritten | Plan |

## Scope

**In scope:**
- Request selection cursor (j/k) on Requests tab
- Error detail panel (Enter expand, Esc close) showing full RequestEvent fields
- Cursor highlight on Requests and Providers tabs
- `config.ProviderDefaults` type and `Config.Providers` field
- `applyDefaults` merge with user provider defaults tier
- Config validation for `providers:` entries
- Marshal/Save round-trip for providers
- Provider edit/add/delete forms in TUI (3-field: provider, base_url, api_key_env)
- Per-entry-wins override semantics
- New form modes: `formProviderEdit`, `formProviderAdd`

**Out of scope:**
- Full request/response body capture
- Detail expansion for successful (2xx) entries
- Provider-level `anthropic_version` or `protocol` defaults
- Dynamic provider health data
- Hot-reload from external file edits
- Changes to per-entry editing (e/a/d on Config tab unchanged)

## Architecture / Approach

Two independent features, ordered by dependency:

**Phase 1 (TUI display):** Add `requestCursor`, `showDetail` to Dashboard. j/k navigates, Enter toggles detail panel. `renderDetailPanel` renders all RequestEvent fields with full error message. `renderRequestsTab` highlights cursor row. Pure TUI — no proxy, config, or event bus changes.

**Phase 2-3 (Config + TUI):** Add `ProviderDefaults{BaseURL, APIKeyEnv}` type and `Providers map[string]ProviderDefaults` to Config. In `applyDefaults()`, apply user provider defaults before `applyEntryDefaults` (blank-fill order: per-entry → user provider default → generated). Update `validate()` and `Marshal()`. In TUI, add `providerCursor`, new form modes, edit/add/delete handlers that mutate `d.config.Providers` and call `config.Save()`.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Request Cursor + Detail Panel | j/k selection, Enter detail panel with full error metadata | Ring buffer cursor bounds edge cases (empty buffer, wrap-around) |
| 2. Provider Defaults — Config | `Providers` map, merge logic, validation, YAML round-trip | Alias provider lookup (zen stored, mix at runtime) |
| 3. Provider Defaults — TUI | Editable Providers tab with e/a/d forms | Form field count mismatch (3 vs 7 fields in existing renderForm) |
| 4. Integration & Polish | Full suite verification, backward compat check | Regression in existing TUI tests from struct field additions |

**Prerequisites:** None — builds on existing `tui-config-setup` infrastructure.
**Estimated effort:** ~2-3 sessions across 4 phases.

## Open Risks & Assumptions

- Alias providers (zen, go, custom) work correctly: `OriginalProvider` lookup in `applyDefaults` ensures user-stored `zen` defaults apply to rewritten `mix` entries.
- Provider form reuses `renderForm` which was built for 7-field forms — the 3-field provider form renders correctly because `renderForm` iterates `d.formFields` length dynamically.
- `handleFormKeyPress` picker trigger for `"provider"` field label must be guarded to skip provider forms.

## Success Criteria (Summary)

- Error detail panel: Enter on red/orange entry opens panel with full error text; Esc closes; green entries no-op.
- Provider defaults: edit/add/delete on Providers tab; saved to `freedius.yaml`; per-entry override takes priority.
- All existing tests pass; `go vet` and `go build` clean.
