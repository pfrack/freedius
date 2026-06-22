# Split TUI Config Tab — Plan Brief

> Full plan: `context/changes/tui-providers-mappings-split/plan.md`
> Research: `context/changes/tui-providers-mappings-split/research.md`

## What & Why

Decompose the Config tab (which conflates providers and mappings in one list) into two distinct tab surfaces: Providers becomes editable via an overlay modal (reusing the existing Help modal pattern), and Mappings replaces Config with mapping-only content using the existing inline form system. This also adds the `protocol` field (`openai`/`anthropic`) to the provider modal — currently YAML-only — so users can configure wire protocol for `mix` providers without editing config files.

## Starting Point

The TUI has 3 tabs: Log, Providers (read-only table), Config (mixed providers+mappings list with inline-body-swap form editing). All editing shares a single `configCursor` and `formMode` dispatch on Config. The Providers tab uses scroll-offset only (`providerScroll`) with no cursor.

## Desired End State

Three tabs with clear separation:
- **Providers**: cursor-based, `p` adds, `Enter`/`e` opens an overlay modal for editing (6 fields including `protocol`), `d` deletes, `j`/`k` scrolls, click opens modal
- **Mappings**: cursor-based, `a` adds, `Enter`/`e` edits inline, `d` deletes, `Ctrl+S` installs shell RC
- **Log**: unchanged

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Providers tab navigation model | Cursor-based with auto-scroll (`providerCursor`) | Consistent with Mappings tab and Config tab cursor patterns; Enter edits highlighted row | Plan |
| Delete confirmation flow | Keep `formDeleteConfirm` mode | Zero new code, existing tests work with minor renames | Plan |
| Ctrl+S scope after split | Mappings tab only | Minimal change, Config-tab users switch to Mappings | Plan |
| Mouse click on Providers | Open edit modal | Symmetric with Mappings→inline-form, consistent discoverability | Plan |
| Modal field layout | Single modal, all 6 fields | Reuses `renderForm` + `fieldLabelsForMode` as-is; `protocol` field added | Plan |
| Protocol field visibility | Always visible (all provider modes) | Matches `anthropic_version` pattern — visible even when irrelevant; simpler than conditional rendering | Plan |

## Scope

**In scope:**
- Rename `tabConfig` → `tabMappings`, add `providerCursor`/`mappingsCursor`
- Refactor `renderConfigTab` → `renderMappingsTab` (mappings-only)
- Providers tab cursor-based navigation with `providerCursor`
- `renderProviderEditModal` overlay modal (6 fields, behavior + protocol pickers)
- Protocol picker (`newProtocolPicker` in `picker.go`)
- `handleProvidersClick` for mouse support on Providers tab
- Update ~25 existing tests + add 5-7 new modal/cursor tests
- Help text and tab label updates

**Out of scope:**
- Backend/config schema changes (`config.Provider`, `config.Mapping` structs untouched)
- Generated file changes (`providers_gen.go`, `adapters_gen.go`)
- Log tab changes
- Config hot-reload
- MixAdapter routing logic changes (already implemented)
- New themes or styles

## Architecture / Approach

The change is decomposed into 5 independently testable phases. Foundation (Phase 1) renames constants and adds cursor state with no behavioral change. Mappings tab (Phase 2) removes the provider rendering branch from the old Config view. Providers tab (Phase 3) adds cursor-based navigation and keybindings. Overlay modal (Phase 4) replaces the inline provider form with a centered, bordered overlay that includes the `protocol` field and behavior/protocol pickers. Tests (Phase 5) updates ~25 existing tests and adds 5-7 new ones. Each phase has its own automated success criteria (`go build`, `go test`, `go vet`).

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Foundation | `tabConfig` → `tabMappings`, cursor fields, help text | Compilation errors from split-brain rename across files |
| 2. Mappings tab | Mappings-only rendering, `collectMappingEntries` | `findEntryIndex` test helper breaks if not updated |
| 3. Providers tab | Cursor navigation, keybindings, click handler, 6-field form | Form field indices shift when protocol field added |
| 4. Overlay modal | `showProviderModal`, `renderProviderEditModal`, protocol picker | Esc-key capture while picker is active inside modal |
| 5. Tests | Update existing tests, add new modal/cursor tests | 4 tests need significant rewrite (not just constant rename) |

**Prerequisites:** None — this is a self-contained TUI refactor.
**Estimated effort:** ~2-3 implementation sessions across 5 phases.

## Open Risks & Assumptions

- **protocol field index shift**: Adding `protocol` as field[5] shifts the `collectProviderFromForm` indices. All field-referencing code must be audited for index 0-4 → 0-5.
- **picker-in-modal key dispatch**: The `handleFormKeyPress` key dispatch order (picker first, then form) must be preserved when the modal is open. If `showProviderModal` key capture interferes with picker, the picker may not receive keys. Mitigated by delegating to `handleFormKeyPress` when `showPicker` is true, before checking modal `Esc`.
- **detach mode**: All form-open paths guard on `d.detachOnQuit`. The new modal-open paths must also guard. Missing this causes runtime panics in the IPC attach dashboard.

## Success Criteria (Summary)

- Providers tab shows a cursor-highlighted provider list; Enter opens an overlay modal with 6 fields
- Mappings tab shows only mappings; Enter edits inline; Ctrl+S installs shell RC
- Protocol field accepts `""`/`"openai"`/`"anthropic"` and persists through config round-trip
- `go test ./proxy/tui/...` passes with 0 failures; no coverage regression
- Help modal shows updated per-tab descriptions
