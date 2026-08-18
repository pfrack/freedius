---
title: "Invariant Aggregate Refactor — RoutingTable Referential Integrity"
created: 2026-08-10
type: refactor-plan
---

# Invariant Aggregate Refactor: RoutingTable Referential Integrity

> Refactor plan only — no production code modified.

---

## STEP 0 — Discovery

### Source Documents

| Document | Path | Key Insight |
|----------|------|-------------|
| PRD | `context/foundation/prd.md` | FR-003: "map any Claude Code model name to any provider model transparently"; Guardrails: "Claude Code cannot tell the difference" |
| Roadmap | `context/foundation/roadmap.md` | Core value prop: routing + fallback chains are the product's raison d'être |
| Tech Stack | `context/foundation/tech-stack.md` | Go stdlib, no ORM, no external router |
| README | `README.md:128-145` | Fallback chains; default catch-all |
| Existing distillation | `context/domain/01-domain-distillation.md` | 10 aggregate candidates already mapped; Privacy ranked #1 (but it is a cross-cutting concern, not an aggregate) |

### Stack & Layers Where Business Logic Lives

| Layer | Directory | Role |
|-------|-----------|------|
| Domain / Config | `config/` | `Config` struct, validation, load/save, defaults merge |
| Web UI handlers | `proxy/web/` | HTTP handlers that mutate config (CRUD) + form validation |
| Routing / Runtime | `proxy/` | `Dispatcher`, fallback loop, runtime re-checks |
| Persistence | `config/` (Save/Load) | Atomic YAML write with backup |

---

## STEP 1 — Business Invariants Identified

| # | Invariant (MUST always hold) | Source |
|---|------------------------------|--------|
| I1 | Every mapping (primary + each fallback entry) references a provider that exists in the `Providers` map. No orphan mappings. | `config/config.go:393-401`, `config/config.go:434-443` |
| I2 | A provider cannot be deleted while any mapping or fallback entry still references it. | `proxy/web/handlers.go:1104-1120` |
| I3 | The `default` mapping always exists and cannot be deleted (it is the catch-all). | `config/config.go:182-203` (auto-inject), `proxy/web/handlers.go:1262-1266` (delete guard) |
| I4 | Provider behavior is one of {openai, anthropic, mix}; base URL has http/https scheme. | `config/config.go:326-356` |
| I5 | Fallback entries cannot themselves have fallbacks (chain is one level deep). | `config/config.go:94-96` (struct design), enforced by codegen |
| I6 | Mapping model_string must not contain control characters (header-injection guard). | `config/config.go:412-418` |
| I7 | The `model` field in any response must echo the client's requested model, regardless of which fallback served it. | `proxy/anthropic_compat.go:257-275`, `proxy/translate/anthropic_openai.go:399` |
| I8 | Request/response payloads are never logged. | `proxy/proxy.go:1-6` (convention only) |

---

## STEP 2 — Classification & Choice of #1

### Axes Assessment

| Invariant | Core to product (1-5) | Smeared across layers (1-5) | Actually enforced? (1-5, 5=weak) | Composite |
|-----------|----------------------|----------------------------|----------------------------------|-----------|
| **I1+I2+I3: RoutingTable referential integrity** | **5** — routing IS the product (PRD FR-003) | **5** — validation in `config/`, duplicate in `web/forms.go`, runtime re-check in `proxy/`, delete guards in `web/handlers.go` | **4** — deletion guards exist ONLY in HTTP handlers; no domain method encapsulates them | **20** |
| I7: Model field stability | 5 | 2 | 1 — well-enforced via context | 2 |
| I4: Provider field validity | 3 | 2 | 2 — enforced at load | 4 |
| I8: No body logging | 5 | 4 | 5 — convention only | 20 |

### Choice: I1+I2+I3 — RoutingTable Referential Integrity

**Why:** The "no body logging" invariant (I8) is the highest-value cross-cutting concern, but it is **not expressible as an aggregate root** — it is a lint/convention rule spread across every file that touches bodies. The user explicitly asked for a **guardian aggregate with domain methods**. The referential-integrity invariant is the strongest aggregate candidate because:

1. **Most core:** The product exists to route requests through model→provider mappings (PRD FR-003, README tagline). A dangling mapping makes routing fail at runtime with an opaque 500.
2. **Most smeared:** The rule is enforced in four separate places — `config.validateMapping` (load), `web/forms.go:validateMappingFields` (duplicate, UI-only), `web/handlers.go:1104-1120` (manual in-use loop), `proxy/proxy.go:306-324` (runtime re-check).
3. **Weakly enforced at the domain level:** The critical deletion guards ("can't delete provider in use", "can't delete default mapping") live **only** inside HTTP handler functions. There is no `Config.RemoveProvider()` domain method. A future CLI command, API endpoint, or test helper that mutates the map bypasses the guard entirely and produces a broken config that only fails at request time.

---

## STEP 3 — Diagnosis of the Chosen Invariant

### Where the rule lives today (all layers)

| Location | What it does | Layer |
|----------|-------------|-------|
| `config/config.go:382-473` — `validateMapping()` | Checks mapping.ProviderName ∈ Providers at load/save time | Domain |
| `config/config.go:182-203` — `ensureDefaultMapping()` | Auto-injects a `default` mapping if absent | Domain |
| `config/config.go:503-514` — `Save()` | Calls `validate()` before marshal — catches orphans on save | Domain / Persistence |
| `proxy/web/forms.go:105-145` — `validateMappingFields()` | **Duplicate** check: `cfg.HasProvider(m.ProviderName)` | Web UI (client-side guardian) |
| `proxy/web/handlers.go:1104-1120` — in-use loop inside `handleDeleteProvider` | Manual `for _, m := range cfg.Mappings` scan to block deletion | Web UI (only guardian of I2) |
| `proxy/web/handlers.go:1262-1266` — `if name == "default"` inside `handleDeleteMapping` | Hard-coded literal check; the ONLY protector of I3 | Web UI (only guardian of I3) |
| `proxy/proxy.go:306-324` — fallback loop | Runtime re-check of `providerExists`; returns 500 if invariant already violated | Runtime (last resort) |

### Specific weaknesses

- **No domain method for deletion.** `handleDeleteProvider` performs the in-use check by manually iterating `cfg.Mappings` (handlers.go:1104-1120) — this logic is not reusable and not reachable from non-HTTP code paths.
- **The default-protection is a string literal in a handler.** `if name == "default"` (handlers.go:1262) is the only thing standing between the user and a config with no catch-all. It is not a domain rule; it would not fire on a future CLI `freedius config remove-mapping default` command.
- **Validation is duplicated.** `forms.go:validateMappingFields` re-implements the provider-existence check that `config.validateMapping` already does. The two can drift (and already differ slightly: the config layer rejects control chars via `unicode.IsControl`, the forms layer only rejects `\r\n`).
- **Save is not transactional with the guard.** The handler deletes the provider from the map, then marshals, then saves. If save fails, it rolls back the in-memory map — but the rollback logic is hand-written inline at handlers.go:1130-1140 and duplicated across all 6 write handlers.
- **Runtime is the last resort.** `proxy/proxy.go:306-324` re-checks provider existence per-request and emits a 500. This means a broken config can be saved and only fails when a real user hits it.

---

## STEP 4 — Design of the Guardian Aggregate

### Aggregate Root: `RoutingTable`

Rename/augment `Config` to express its role as the consistency boundary for the routing model. The aggregate owns `Providers`, `Mappings`, and the derived `matchers`. All mutations go through domain methods with preconditions.

### Domain Errors (named, exported)

```go
var (
    ErrProviderInUse    = errors.New("provider is referenced by one or more mappings")
    ErrProtectedMapping = errors.New("the default mapping cannot be deleted")
    ErrDuplicateEntry   = errors.New("duplicate provider or mapping name")
    ErrReferenceBroken  = errors.New("mapping references unknown provider")
    ErrInvalidBehavior  = errors.New("behavior must be openai, anthropic, or mix")
)
```

Use struct errors when context matters:

```go
type ProviderInUseError struct {
    ProviderName string
    ReferencedBy []string // mapping names
}
func (e *ProviderInUseError) Error() string { ... }
```

### Domain Methods (signatures + pseudocode)

```go
// AddProvider registers a new provider. Rejects duplicates and invalid fields.
func (c *RoutingTable) AddProvider(name string, p Provider) error {
    if err := validateProviderName(name); err != nil { return err }
    if _, exists := c.Providers[name]; exists { return ErrDuplicateEntry }
    if err := validateProviderFields(p); err != nil { return err } // behavior, URL scheme, protocol
    c.Providers[name] = p
    return nil
}

// RemoveProvider deletes a provider. FAILS FAST if any mapping or fallback references it.
func (c *RoutingTable) RemoveProvider(name string) error {
    refs := c.providerReferences(name)   // scans Mappings + Fallback entries
    if len(refs) > 0 {
        return &ProviderInUseError{ProviderName: name, ReferencedBy: refs}
    }
    delete(c.Providers, name)
    return nil
}

// AddMapping registers a mapping. Rejects orphans and duplicate fallbacks.
func (c *RoutingTable) AddMapping(name string, m Mapping) error {
    if err := validateMappingName(name); err != nil { return err }
    if _, exists := c.Mappings[name]; exists { return ErrDuplicateEntry }
    if err := c.validateMappingTargets(m); err != nil { return err } // provider exists, fallbacks exist, dedup
    c.Mappings[name] = m
    c.rebuildMatchers()
    return nil
}

// UpdateMapping replaces a mapping. Same preconditions as AddMapping.
// Rejects renaming to/overwriting "default" with a broken target.
func (c *RoutingTable) UpdateMapping(name string, m Mapping) error {
    if _, exists := c.Mappings[name]; !exists { return ErrNotFound }
    if err := c.validateMappingTargets(m); err != nil { return err }
    c.Mappings[name] = m
    c.rebuildMatchers()
    return nil
}

// RemoveMapping deletes a mapping. FAILS FAST if name == "default".
func (c *RoutingTable) RemoveMapping(name string) error {
    if name == "default" { return ErrProtectedMapping }
    if _, exists := c.Mappings[name]; !exists { return ErrNotFound }
    delete(c.Mappings, name)
    c.rebuildMatchers()
    return nil
}

// validateMappingTargets: the single place that enforces I1 for a mapping+fallbacks.
func (c *RoutingTable) validateMappingTargets(m Mapping) error {
    if !c.hasProvider(m.ProviderName) { return ErrReferenceBroken }
    seen := map[[2]string]bool{{m.ProviderName, m.ModelString}: true}
    for _, fb := range m.Fallback {
        if !c.hasProvider(fb.ProviderName) { return ErrReferenceBroken }
        if fb.ModelString == "" { return ErrReferenceBroken }
        key := [2]string{fb.ProviderName, fb.ModelString}
        if seen[key] { return ErrDuplicateEntry }
        seen[key] = true
    }
    return nil
}
```

### Repository / Persistence (single transaction)

The existing `Config.Save()` already does atomic write (temp file + rename). The refactor keeps it but makes it a method on the aggregate that is only callable **after** a domain method has succeeded:

```go
// Save persists the aggregate atomically. Invokes validate() as a final guard.
func (c *RoutingTable) Save(path string) error {
    if err := c.validate(); err != nil { return err }      // whole-table invariant check
    data, err := c.Marshal()
    if err != nil { return err }
    return atomicWrite(path, data)                          // temp + rename + backup
}
```

The whole mutation runs as one atomic sequence: domain method (enforces precondition) → `Save` (validates + atomic write). If the domain method returns an error, `Save` is never called — no broken state can be persisted. The inline rollback blocks in the current handlers become unnecessary because the map is only mutated after the precondition passes and the write is a single `Rename`.

### Thin API/Route Layer

```go
// BEFORE (handlers.go:1104-1120): manual loop + inline delete + manual rollback
// AFTER:
func handleDeleteProvider(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
    name, err := pathName(r, "/v1/providers/")
    if err != nil { writeJSONError(w, 400, "bad_path", err.Error()); return }

    if err := h.Cfg.RemoveProvider(name); err != nil {        // <-- domain method, fail-fast
        switch {
        case errors.Is(err, ErrNotFound):
            writeJSONError(w, 404, "not_found", err.Error())
        case errors.Is(err, ErrProviderInUse):
            writeJSONError(w, 409, "provider_in_use", err.Error())
        default:
            writeJSONError(w, 400, "invalid", err.Error())
        }
        return
    }

    if err := h.Cfg.Save(cfgPath); err != nil {              // <-- single atomic write
        writeJSONError(w, 500, "save_failed", err.Error())
        return
    }
    renderProvidersTable(w, r, h)
}
```

Enforcement moves from the client (HTTP handler) to the server (domain aggregate). The handler only maps domain errors to status codes.

---

## STEP 5 — Before/After, Plan, Tests

### Before/after for every place the rule currently lives

| Place | Before | After |
|-------|--------|-------|
| `config/config.go:382-473` — `validateMapping` | Standalone function, only at load/save | Called by `validateMappingTargets` (reusable); `validate()` retained as final save-time guard |
| `config/config.go:182-203` — `ensureDefaultMapping` | Auto-injects default at load | Keep; add invariant that `RemoveMapping("default")` fails (defense in depth) |
| `proxy/web/forms.go:105-145` — `validateMappingFields` | Duplicate validation in UI layer | **Deleted** — `AddMapping`/`UpdateMapping` enforce the same rules; forms.go only decodes |
| `proxy/web/handlers.go:1104-1120` — in-use loop | Manual scan inside handler | **Replaced** by `RemoveProvider()` domain method |
| `proxy/web/handlers.go:1262-1266` — `if name == "default"` | Hard-coded literal in handler | **Replaced** by `RemoveMapping()` domain method |
| `proxy/web/handlers.go` — 6× inline rollback blocks | Hand-written restore-on-error | **Deleted** — no rollback needed; mutation only happens after precondition + atomic write |
| `proxy/proxy.go:306-324` — runtime re-check | 500 at request time | Keep as last-resort defense; should never fire after the refactor |

### Phased Refactor Plan

**Phase 1 — Domain methods + errors (test-first).**
- Add `domainerrors.go` with named errors.
- Add `AddProvider`, `RemoveProvider`, `AddMapping`, `UpdateMapping`, `RemoveMapping` to `Config` (or new `RoutingTable` type).
- Tests first: table-driven for every precondition (see test cases below).

**Phase 2 — Collapse duplication.**
- Delete `validateMappingFields` / `validateProviderFields` from `forms.go`; handlers call domain methods and map errors.
- Remove the 6 inline rollback blocks — rely on domain-method fail-fast + atomic `Save`.

**Phase 3 — Harden persistence.**
- Ensure `Save()` is the only write path; add a test that a broken config (orphan mapping) cannot be saved even by bypassing handlers.

**Phase 4 — Runtime guard audit.**
- Verify `proxy/proxy.go:306-324` still compiles; confirm with a test that the 500 path is unreachable when the aggregate is the only mutation surface.

### Test Cases for the Invariant

| Case | Operation | Expected |
|------|-----------|----------|
| Legal: add provider, then mapping referencing it | `AddProvider("x", …)` → `AddMapping("m", {ProviderName:"x"})` | Success |
| Legal: delete provider with no references | `RemoveProvider("x")` | Success |
| **Illegal: delete provider in use** | `AddProvider` → `AddMapping(ref x)` → `RemoveProvider("x")` | `ErrProviderInUse` with `ReferencedBy` |
| **Illegal: delete provider used only as fallback** | `AddMapping(m, fallback:[{ProviderName:"x"}])` → `RemoveProvider("x")` | `ErrProviderInUse` (fallback reference counts) |
| **Illegal: delete default mapping** | `RemoveMapping("default")` | `ErrProtectedMapping` |
| **Illegal: add mapping with unknown provider** | `AddMapping("m", {ProviderName:"ghost"})` | `ErrReferenceBroken` |
| **Illegal: add mapping with unknown fallback provider** | `AddMapping("m", fallback:[{ProviderName:"ghost"}])` | `ErrReferenceBroken` |
| **Illegal: add mapping with duplicate fallback** | `AddMapping("m", fallback:[{p,x},{p,x}])` | `ErrDuplicateEntry` |
| **Illegal: add provider with invalid behavior** | `AddProvider("x", {Behavior:"wat"})` | `ErrInvalidBehavior` |
| **Illegal: save config with orphan mapping bypassing handlers** | Direct map mutation + `Save()` | `validate()` rejects save |

### Load-Bearing Names to Register

| Name | Kind | Purpose |
|------|------|---------|
| `ErrProviderInUse` | domain error | Provider deletion blocked — maps to HTTP 409 |
| `ErrProtectedMapping` | domain error | Default mapping deletion blocked — maps to HTTP 409 |
| `ErrReferenceBroken` | domain error | Orphan mapping — maps to HTTP 400 |
| `ErrDuplicateEntry` | domain error | Name collision — maps to HTTP 409 |
| `ErrInvalidBehavior` | domain error | Bad behavior field — maps to HTTP 400 |
| `RoutingTable` (or `Config`) | aggregate root | Single mutation surface |
| `RoutingTable.RemoveProvider` | domain method | Only way to delete a provider |
| `RoutingTable.RemoveMapping` | domain method | Only way to delete a mapping |
| `RoutingTable.Save` | repository | Only write path; atomic |

---

## Summary

The routing-table referential-integrity invariant (every mapping references a known provider; no deleting referenced providers; no deleting the default mapping) is the product's most core consistency rule and is currently smeared across four layers — `config.validateMapping`, a duplicate in `web/forms.go`, manual in-use loops in `web/handlers.go`, and a runtime re-check in `proxy/proxy.go`. The deletion guards exist only inside HTTP handler functions, so any future non-HTTP mutation path would bypass them and produce a config that fails opaquely at request time. The fix is a `RoutingTable` aggregate root with domain methods (`AddProvider`, `RemoveProvider`, `AddMapping`, `UpdateMapping`, `RemoveMapping`) that enforce preconditions and fail fast with named domain errors (`ErrProviderInUse`, `ErrProtectedMapping`, `ErrReferenceBroken`). Persistence goes through a single atomic `Save()` that validates before writing, eliminating the six inline rollback blocks. The web handlers become thin: decode input → call domain method → map error to status code. Enforcement moves from the client (UI handler) to the server (domain aggregate), and the runtime 500 path becomes unreachable under normal operation.
