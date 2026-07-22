<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Mapping Card Provenance + README Rewrite

- **Plan**: context/changes/solo-dev-positioning/plan.md
- **Scope**: All phases (1, 2, 3) of 3
- **Date**: 2026-07-21
- **Verdict**: APPROVED
- **Findings**: 0 critical · 2 warnings · 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️ (2 minor README structural drifts) |
| Scope Discipline | PASS ✅ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | WARNING ⚠️ (README length 171 vs ≤140 target) |

## Findings

### F1 — README missing dedicated `## Web Dashboard` section

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: README.md (section list at lines 8–157)
- **Detail**: The plan's required structure (lines 226–229) lists a dedicated `## Web Dashboard` section between Configuration and CLI and Env Vars. The implementation folded the dashboard content (card signals, auth token note, live logs, last-used responder) into the "Reading the system state" section instead. The content is preserved and accurate; only the section grouping diverges. The plan also removed the Web Dashboard section from its own model layout when it consolidated — both choices are defensible, but the literal plan structure is not matched.
- **Fix**: Either (a) extract dashboard content into its own `## Web Dashboard` section to match the plan's structure, or (b) update the plan to note the intentional consolidation. Content is correct either way.
- **Decision**: FIXED — extracted `## Web Dashboard` section with feature list + auth note after Configuration; "Reading the system state" trimmed to brief pointer. Section structure now matches plan (7 sections).

### F2 — README length 171 lines, exceeding the ≤140 plan target

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: README.md
- **Detail**: The plan's Phase 3 constraints (line 248) set a 90–140 line target. The rewrites landed at 171 lines (31 over). The plan's explicit keep-directives (CLI flag table ~18 lines, env var table ~22 lines, multiple YAML examples, fallback-chain block, reference section) collectively add ~80 lines of reference material that the plan itself mandated preserving. The drift is a tension between the plan's "keep reference tables" instruction and its line target — both can't be fully satisfied simultaneously without losing reference content the maintainer uses.
- **Fix**: Consolidate the CLI flag block into a denser table or trim the env var descriptions by a word or two to shave ~15 lines, accepting the rest as the cost of the mandated reference content. Alternatively, relax the line target in the plan to ~175 to reflect the keep-directives honestly.
- **Decision**: FIXED (partial) — consolidated CLI flags to a denser 3-column table (code-block → table, removed scaffolding), trimmed redundant "Fallback chains" / "Mapping resolution" prose. 181 → 171. Residual 31 lines over target is structural: the plan mandates preserving CLI flag table, env var table, and 3 YAML examples. Recommend relaxing the plan's line target to ~175 in the verification log rather than cutting mandated content.

### F3 — `extractFamily` renamed/exported to `ExtractFamily`

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — already resolved; no action needed
- **Dimension**: Pattern Consistency
- **Location**: proxy/families.go:22, proxy/proxy.go:132, proxy/web/handlers.go:325
- **Detail**: The plan's Phase 1 item 4 says "No modification to extractFamily itself." The implementation renamed it to the exported `ExtractFamily`. This was mechanically required: `extractFamily` is unexported in package `proxy`, and `buildMappingRows` lives in package `web`. Go forbids cross-package calls to unexported identifiers. The function body is unchanged; only visibility changed. In-package callers (`proxy.go:132`, `families_test.go`) were updated. An appropriate doc comment was added per Go's exported-function convention.
- **Fix**: No code change needed. Consider noting the deviation in the plan's verification log for traceability.
- **Decision**: FIXED — added an "Adaptations Log" section to the plan noting the export deviation (mechanically required by Go's cross-package visibility rules).

### F4 — `default` catch-all family filtered to empty at call site

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — already resolved; no action needed
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers.go:325-328
- **Detail**: The plan's Phase 1 item 3 says `Family: extractFamily(name)`. The implementation maps the `"default"` catch-all (an empty-regex pattern in `knownFamilies` that matches every input) to an empty string so cards with no explicit family keyword render no badge rather than a "default" badge. This is a sensible deviation that aligns with the plan's stated intent (only show explicit family keywords like opus/sonnet/haiku) and avoids the "default" catch-all — which the package's real consumer at `proxy/proxy.go:resolveMapping` still relies on for catch-all mapping resolution — from leaking into the display path. The catch-all itself is correctly preserved; only the display mapping adapts it.
- **Fix**: No change needed. Intent preserved; deviation justified.
- **Decision**: ACKNOWLEDGED — deviation already recorded in the plan's Adaptations Log (added in F3). Catch-all preserved for resolveMapping; only display path adapts it.
