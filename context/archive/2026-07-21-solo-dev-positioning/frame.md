# Frame Brief: Mapping provenance visualization

> Framing step. Captures the reframe that emerged from discussion: this is
> not a positioning / cold-start problem. It's a maintainer-provenance
> problem surfaced via visualization.

## Reported Observation

> "Nie pewnie chodzi o to jak pamiętać skąd ten mapping"
> (It's not about positioning — it's about remembering / seeing where
> a mapping came from and why it exists.)

The existing `mapping-graph-visualization` change (2026-07-06) renders
each mapping as a chain card — model → provider → upstream model —
showing the **shape** of the mapping. It does **not** show:

- **When** the mapping was added (added: date / git commit).
- **Why** it was added (original problem being solved, credentials
  rationale, fallback reason).
- **Key status** (whether the env var it needs is actually set, and
  what the consequence of a missing key is at request time).

The maintainer reaches for a mapping and cannot quickly reconstruct its
provenance or current fitness. The graph card answers the data shape;
it does not answer the decision.

## Dimension Map

1. **Shape visualization** — breadcrumb-chain cards already shipped
   (`/context/archive/mapping-graph-visualization/`).  ← current state
2. **Decision provenance layer** — who/when/why each mapping exists,
   and whether its required env is present today.  ← user's actual
   need (NEW)
3. **Name / model binding** — whether the mapping key (`opus`, `sonnet`,
   `haiku`) guarantees correct resolution for the client actually in
   use. ← implied concern, may be answered by #2

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| #1 Shipped graph lacks provenance | `mapping-graph-visualization` plan renders shape cards only, no commit/env data in any file:line referenced in the plan. | STRONG |
| #2 Env-presence-at-a-glance is the deeper need | `cmd/freedius/main.go:134` discards the result of `checkRequiredEnvVars` — the mapping graph shows a configured mapping but cannot surface that the key is missing. Provider env lookup lives in `config/providers.go` (`DefaultAPIKeyEnv`) and is never rendered as a status on the card. | STRONG |
| #3 Mapping-key ↔ client-model binding is independent | Family-based prefix matching (`proxy/families.go`) handles this at request time; the card should make it explicit on the breadcrumb so the maintainer can see which client models will resolve to this mapping. | WEAK (may be derivative of #2 + #3) |

## Narrowing Signals

- The user shifted the reframe twice in discussion:
  cold-start-onboarding → resumability → "I reached for something
  and it didn't behave as I expected" → explicit "where did this mapping
  come from?"
- Each earlier framing (README positioning, cold-start arc, PRD drift)
  was a *reading-the-code* shape that did not match the actual user
  pain. The probe that landed was a **concrete prior feature**
  (mapping-graph) and the question "what's missing from its cards?"
- Rule: "shit I don't know probably B" is a weak signal in response to
  "which option"; under duress it usually means the question is wrong,
  not the answer. Re-framing as a narrower, concrete feature-gap worked.

## Cross-System Convention

Routing tools (OpenLiteSpeed, Envoy, Traefik, Caddy) land on the same
pattern: **each route shows its target + its health + a provenance
handle** (generated-by / deploy-ref / last-modified). A mapping graph
that shows shape without credential-status is a known smell — it tells
you where traffic *will* go without telling you whether it *can* go
there right now.

## Reframed Problem Statement

> **The actual problem to plan around is:** the mapping-graph
> visualization renders data shape (model → provider → upstream) but
> not decision provenance (when/why/env status), leaving the maintainer
> unable to reconstruct why a mapping exists and whether it will work
> for the next request — **and the README doesn't anchor any of this
> either.** The surface make the same gap legible in prose is 218 lines
> of copy that header "what it connects," not "what it's for / how to
> read its current state."

The fix is two complementary deliverables under one change:

1. **Enrich the breadcrumb cards** with three signals:
   - Added-at timestamp (git blame metadata on the mapping).
   - Env-var presence status for the mapping's provider.
   - Fallback rationale (why this fallback chain exists, if it does).
2. **Rephrase README top-to-bottom** so that when the maintainer
   returns after a gap, 30 seconds of reading re-grounds them in:
   - what this tool exists to do,
   - who it's for (the solo dev maintainer),
   - how to read the system's current state from the dashboard
     (now including provenance on each card),
   - what its current honest packaging story is (no stale "no web UI
     in v1" claims, no "no setup required" gloss that hides the
     credential gate).

The README rephrase is bounded by the lock that the maintainer IS the
end user — no cold-start persona write-up, no alternative-tool pitch.
It answers *legibility of the system as built*, not *adoption*.

## Confidence

- **HIGH** — reframe survived two challenge rounds; matched a concrete
  existing artifact the maintainer already uses; matched cross-system
  convention (routing tools always render provenance + health together).

## What Changes for /10x-plan

Plan the **enrichment of breadcrumb-chain cards** to carry provenance
and env-rider status. The plan should:

- Start from the existing graph renderer (`proxy/web/handlers.go` and
  the templates driven by `mapping-graph-visualization`) and extend the
  card footprint, not re-architect it.
- Source env-presence data from the existing `checkRequiredEnvVars`
  machinery (potentially un-discarding its result at
  `cmd/freedius/main.ts:134` as a side effect).
- Source added-at timestamps from `git blame` annotations in the config
  file or from the mapping's first-seen timestamp in app state.
- NOT expand into README / cold-start / PRD terrain — that framing
  was deliberately rejected and does not serve the maintainer's actual
  moment of friction.

Flag the "RU weak" finding as an explicit scope decision in the plan:
either fold mapping-key ↔ resolution binding into the breadcrumb card
(as an extra column "also matches: claude-opus-4-5-…, …") or cut it to
keep the change tight.

## References

- Existing graph renderer:
  `context/archive/mapping-graph-visualization/plan.md`
  (renders breadcrumb-chain cards).
- Existing-but-discarded env-check:
  `cmd/freedius/main.go:134`,
  `cmd/freedius/main.go:376-393` (could be un-discarded as upstream data
  for the new cards).
- Existing provider env store:
  `config/providers.go` (`DefaultAPIKeyEnv` field).
- Mapping shape data passed to the renderer:
  `proxy/web/handlers.go` (where `renderMappingsTable` builds the rows;
  the enrich step happens here or just after).
- Supporting context:
  `context/archive/2026-07-07-routing-visibility/frame.md:34`
  ("Dashboard as landing page — Zero routing context") confirms the
  routing context is still thin.
