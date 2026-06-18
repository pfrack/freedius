# Extend TUI with Mappings/Models Setup and Plain Error Display — Plan Brief

> Full plan: `context/changes/tui-config-setup/plan.md`
> Research: `context/changes/tui-config-setup/research.md`

## What & Why

The TUI dashboard currently shows config read-only and displays only HTTP status codes for errors — users can't see *what* error occurred or edit config without switching to a text editor. This plan adds interactive config editing (CRUD for mappings and models via modal forms with a provider picker) and surfaces upstream error messages directly in the requests tab, making the TUI the single interface for both monitoring and configuration.

## Starting Point

- TUI dashboard exists with 3 tabs (Requests, Providers, Config) using Bubble Tea v2 + Lip Gloss v2
- `RequestEvent` has `Status int` but no error message — errors are written to response and discarded
- `config.Config` has `Load()` but no `Save()` or YAML serialization — no round-trip path
- No form/input widgets in the codebase — `charm.land/bubbles/v2` not in go.mod
- Original TUI plan explicitly scoped config editing to "future web UI" — but nothing technical blocks it

## Desired End State

Users press `e` on the Config tab to edit a mapping/model in a modal form. Typing Tab navigates fields; Enter on the provider field opens a scrollable list of 7 known providers. Enter submits, validates, and saves to `freedius.yaml` (with `.bak` backup). Changes take effect immediately — the running proxy routes to the updated model. On the Requests tab, failed requests show the actual error message ("no configured mapping for model 'gpt-4'") next to the status code.

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Config editing scope | Full CRUD (add, edit, delete) | Users should never need to edit YAML manually once TUI exists | Plan |
| Provider field UX | List picker from KnownProviders | Prevents typos; shows behavior/alias context; 7 providers is manageable for a list | Plan |
| Error capture format | ErrorType + ErrorMessage fields in RequestEvent | Mirrors the API error envelope; enables filtering + human-readable display | Plan |
| Form UX pattern | Modal overlay on Config tab | Preserves 3-tab layout; feels like a popup editor; natural Esc-to-cancel | Plan |
| Save strategy | Write to disk on confirm (with .bak backup) | Changes persist across restarts; backup prevents data loss; no hot-reload complexity | Plan |
| Validation UX | Inline field errors | User sees exactly what to fix; matches config validation message style | Plan |

## Scope

**In scope:**
- ErrorType and ErrorMessage on RequestEvent, populated by EventBusMiddleware
- Error message column in requests tab
- Config.Marshal() YAML serialization with OriginalProvider alias preservation
- Config.Save() with validation + backup
- Provider list picker component (7 providers with metadata)
- Modal overlay form for model/mapping editing (6 fields: name, provider, model, base_url, api_key_env, protocol)
- Tab, Enter, Esc keyboard navigation in forms
- Full CRUD: add, edit, delete entries
- In-memory config mutation visible to running proxy
- Config entry selection cursor (j/k navigation on Config tab)

**Out of scope:**
- Web UI for config editing
- File watcher for external config edits
- Multi-line text areas
- Custom provider addition (fixed 7 providers)
- Undo/redo stack
- Request log filtering/search

## Architecture / Approach

Six phases, bottom-up dependency order:

```
Phase 1: Error Capture ──► Phase 2: YAML Serialization ──► Phase 3: Provider Picker
                                                                     │
                                                                     ▼
                                                          Phase 4: Modal Forms
                                                                     │
                                                                     ▼
                                                          Phase 5: CRUD + Save
                                                                     │
                                                                     ▼
                                                          Phase 6: Polish + Tests
```

- **Phase 1** extends `RequestEvent` + `EventBusMiddleware` + `writeErrorJSON` to pipe error metadata through response headers
- **Phase 2** builds `Config.Marshal()` and `Save()` using `goccy/go-yaml`, with `OriginalProvider` recovery for alias round-trip
- **Phase 3** adds `charm.land/bubbles/v2` and wraps a `list.Model` into a provider picker
- **Phase 4** adds form state to `Dashboard`, delegates keystrokes to `textinput.Model` widgets, renders modal overlay
- **Phase 5** wires form submit/delete to config mutation + `Save()` + in-memory update
- **Phase 6** adds keyboard shortcuts, selection cursor, integration tests, clean shutdown

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Error Capture | ErrorMessage+ErrorType in events, displayed in requests tab | Header-based conduit must be set before response write |
| 2. YAML Serialization | Marshal/Save with OriginalProvider preservation | Alias (zen→mix) must survive round-trip |
| 3. Provider Picker | Scrollable list of 7 providers with context | New dependency (bubbles/v2); list.Model API learning curve |
| 4. Modal Forms | Text input form overlay with field navigation | Form focus management; keystroke delegation complexity |
| 5. CRUD + Save | Add/edit/delete entries, save to disk, live proxy routing | Signature change to NewDashboard — one call site |
| 6. Polish + Tests | Keyboard hints, cursor nav, integration tests | Testability of Bubble Tea programs |

**Prerequisites:** Existing TUI dashboard must be functional (`proxy/tui/` package, `charm.land/bubbletea/v2` in go.mod)
**Estimated effort:** ~3-4 sessions across 6 phases

## Open Risks & Assumptions

- **Header conduit assumption**: Error metadata flows through `X-Freedius-Error-Type`/`X-Freedius-Error-Message` response headers. If a reverse proxy strips custom headers, this breaks. Mitigation: the freedius proxy itself sets these on the response it writes — no external proxy in the path.
- **Bubbles v2 compatibility**: `charm.land/bubbles/v2` must be compatible with `charm.land/bubbletea/v2 v2.0.7` already in go.mod. Charm ecosystem is well-maintained; v2 modules are designed to work together.
- **Config editing during active requests**: The dispatcher reads `Config` maps per-request. Map mutations (add/delete) on the TUI goroutine between requests are safe — Go maps support concurrent reads with a single writer. No mutex needed.
- **goccy/go-yaml marshal fidelity**: The YAML library must marshal `map[string]Model` with the same structure as the hand-written `freedius.yaml`. The `omitempty` tags should produce clean output; `yaml:"-"` on `OriginalProvider` prevents it from leaking.

## Success Criteria (Summary)

- Error messages visible in requests tab alongside status codes
- Modal form opens on Config tab, navigable by keyboard, saves to disk with backup
- Provider field shows a scrollable list of 7 known providers with context
- All CRUD operations work: add entry, edit entry, delete entry with confirmation
- Edited config routes live — no restart needed
- All tests pass with `-race`, lint clean, single binary builds
