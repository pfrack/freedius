---
id: regex-mapping-matching
title: Loose regex-based model→mapping matching with a protected default catch-all
status: preparing
created: 2026-08-10
updated: 2026-08-10
tags:
  - proxy
  - mappings
  - routing
---

# Frame Brief: Loose regex mapping matching + protected default

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

A requested model name only routes to a mapping when it exactly equals a
mapping key or contains one of four hardcoded family keywords
(`opus|sonnet|haiku|auto`). User-defined mapping keys do not match loosely,
and a model that matches nothing just fails. The user wants looser matching
so any model routes to a mapping instead of erroring.

## Initial Framing (preserved)

- **User's stated cause or approach**: The matcher should be regex-based; if a
  mapping name is not a regex, treat it as `.*name.*`.
- **User's proposed direction**: A non-deletable "default" mapping that always
  exists and acts as a `*` catch-all mapper.
- **Pre-dispatch narrowing**: Symptom = both exact-match-too-strict AND no
  catch-all; scope = apply loose matching to *all* mappings automatically;
  default = a catch-all router (true `*`).

## Dimension Map

The observation could originate at any of these dimensions:

1. **Matching mechanism** — `resolveMapping` does exact key lookup + a fixed
   4-keyword family fallback. User-defined keys never match loosely.
   `proxy/proxy.go:135,139`  ← initial framing lands here
2. **Catch-all existence** — a `default` family already matches everything via
   an empty regex pattern (`proxy/families.go:15`), so a catch-all *already
   exists*. The "no catch-all" premise is only partially true.
3. **Deletion guard** — nothing prevents deleting the `default` mapping today
   (`proxy/web/handlers.go:1270` deletes unconditionally). The "always
   available" requirement is a real gap.
4. **Collision / precedence** — if multiple keys regex-match one model, which
   wins? Today family order is fixed in `knownFamilies`
   (`proxy_test.go:412`); a generic matcher needs a defined precedence.
5. **`default` vs `.*default.*` conflict** — auto-wrapping `default` as
   `.*default.*` would *break* the catch-all (it would only match models
   containing "default"). `default` must be exempt from auto-wrap.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Exact key match only; families substring-match 4 fixed keywords | `proxy/proxy.go:135` exact lookup; `proxy/families.go:11-14` four patterns; `Mapping` struct has no regex field (`config/config.go:75`) | STRONG |
| A catch-all already exists via `default` family | `proxy/families.go:15` empty regex matches all; `proxy/proxy.go:139` falls through to it; `starter.yaml:74` defines `default` mapping | STRONG (contradicts "no catch-all" premise) |
| `default` mapping is deletable today | `proxy/web/handlers.go:1270` `delete(cfg.Mappings, name)` with no guard | STRONG |
| Auto-wrapping all keys breaks `default` catch-all | `families.go:15` + `proxy.go:139`; `default` as `.*default.*` stops matching arbitrary models | STRONG (design conflict) |
| Collision precedence undefined for generic matcher | `proxy_test.go:412` family priority is hardcoded/order-independent; no collision rule exists for arbitrary keys | STRONG (open plan decision) |

## Narrowing Signals

- User confirmed symptom is BOTH rigidity and missing-catch-all, scope is ALL
  mappings auto, and default is a catch-all router — so the plan must cover
  generalization + protection together.
- Reading the code shows a catch-all already fires today via the `default`
  family, so "missing catch-all" is really "catch-all can be deleted / is
  tied to a hardcoded keyword set" rather than "absent."

## Cross-System Convention

- Family matching uses a **fixed priority list** (`knownFamilies`), NOT YAML/map
  order — `proxy_test.go:412` `TestServeHTTPFamilyPriorityIndependentOfYAMLOrder`
  asserts `claude-opus-4-1` → `opus` even when `auto` is listed first.
- A `default` mapping is already required in the starter template and is the
  documented catch-all (`starter.yaml:67-83`).
- Any new matching behavior must preserve these tests and the existing
  exact-match-followed-by-family behavior.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: mapping-to-model matching is
> hardcoded to four family keywords, so user-defined mapping keys only match
> exactly; generalize matching so every mapping key matches as a
> regex/substring (`*key*`), keep `default` as a specially-exempt,
> always-evaluated-last catch-all, and protect it from deletion.

The initial framing was *mostly* right but misdiagnosed "no catch-all" — one
already exists via the `default` family. The genuine new work is (a)
extending loose matching from the 4 fixed families to all keys, and (b)
guaranteeing an undeletable `default`. Two corrections the plan MUST honor:
`default` must be exempt from the `.*key.*` auto-wrap (or it stops being a
`*`), and a collision/precedence rule must be chosen for when several keys
match one model.

## Confidence

- **MEDIUM** — strong evidence for every dimension, but the catch-all premise
  was partly already satisfied and the collision-precedence rule is an open
  decision the plan must make (see "What Changes").

## What Changes for /10x-plan

The plan is about: (1) a regex/substring matcher over all mapping keys with
`*key*` auto-wrap for plain keys, (2) exempting + always-last-evaluating
`default`, (3) a delete guard for `default` in `handleDeleteMapping` (and the
TUI delete flow), and (4) a documented precedence rule (recommend: exact key >
regex match, with the 4 legacy families keeping their fixed priority, then
`default` last). It should preserve `proxy_test.go:412/444` and the starter
`default` mapping.

## References

- `proxy/proxy.go:132` resolveMapping (exact + family fallback)
- `proxy/families.go:10-29` knownFamilies + ExtractFamily (default = empty regex)
- `proxy/web/handlers.go:1255-1295` handleDeleteMapping (no delete guard)
- `cmd/freedius/templates/starter.yaml:67-94` default/auto catch-all mappings
- `config/config.go:75-80` Mapping struct (no regex field yet)
- `proxy/proxy_test.go:412` family priority independent of YAML order
