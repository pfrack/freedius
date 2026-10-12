<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider Writeback Merge

- **Plan**: context/changes/provider-writeback-merge/plan.md
- **Scope**: Phases 1-4 (full plan, all automated items `[x]`; manual items 1.3 / 2.5 / 3.3 / 4.5 unrun)
- **Implementation**: 75a11c9 (parent 0956851), closed out in 54abdff
- **Date**: 2026-10-12
- **Verdict**: NEEDS ATTENTION
- **Findings**: 3 warnings, 5 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | ⚠️ WARNING — D1/D2/D4 faithful; D3 not implemented as written |
| Scope Discipline | ✅ PASS — mapping handlers, `providers.yaml`, and the `Provider` schema all untouched |
| Safety & Quality | ⚠️ WARNING — new silent-override path on create; ghost-entry rollback hole |
| Architecture | ✅ PASS — shared merge is real; aliasing invariant holds |
| Pattern Consistency | ✅ PASS — `go vet` / `gofmt` clean, error wrapping `%w`, exported symbols documented |
| Success Criteria | ✅ PASS — build, `-race` suite, and codegen-invariant gates all green |

### Verification re-run at review time

```
go build ./...                                             → BUILD_OK
go test ./... -race -count=1                               → ok (8 packages)
go generate ./... && git diff --exit-code config/ proxy/   → GEN_DIFF_CLEAN
gofmt -l .                                                 → clean
go vet ./...                                               → VET_CLEAN
```

Every claim below marked *probe-verified* was reproduced with a throwaway
`*_test.go` probe run under `-race`, then deleted (`git status` clean).

## Findings

### F1 — D3 was not implemented: `Save(cfgPath)` is never called, and `Config.Save` is now dead in production

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 MEDIUM — real duplication risk and dead code; the fix is mechanical and low-risk
- **Dimension**: Plan Adherence
- **Location**: `proxy/web/handlers.go:1093,1104,1116` and `1173,1180,1188`
- **Detail**: Plan D3 (`plan.md:114-120`) states verbatim: *"Replace `SaveData(cfgPath,
  data)` with `Save(cfgPath)` in the two provider write handlers. `Save` already does
  `validate()` → `Marshal()` → `SaveData()`, so this collapses the manual `Marshal()` +
  `marshal_failed` branch into one call and gains validation for free."*

  The implementation did the opposite — it kept the entire hand-rolled sequence and
  bolted a separate gate onto the front:

  ```go
  1173:  if verr := cfg.Validate(cfgPath); verr != nil { ...400 validation_failed... }
  1180:  data, mErr := cfg.Marshal()
  1184:      writeJSONError(w, 500, "marshal_failed", mErr.Error())
  1188:      if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil { ...500... }
  ```

  Verified by grep: `.Save(` has **zero** non-test call sites anywhere in the tree —
  `Config.Save` (`config/config.go:517`) is now reachable only from
  `config/config_test.go:793,827`. Consequences: (a) the `marshal_failed` branch D3
  wanted deleted survives as effectively dead code guarding a `yaml.Marshal` that
  cannot fail on this struct; (b) `handlers.go` still duplicates `Save`'s body, so the
  two drift if `Save` ever changes — precisely the class of duplication D2 was
  extracted to eliminate; (c) the marshal error loses `Save`'s
  `"config: marshal %s: %w"` context wrapping (`config/config.go:524`).

  Behaviour is otherwise correct: validation does gate the write and does return 400.
- **Fix**: in both handlers, delete the `Marshal()`/`marshal_failed` block and call
  `cfg.Save(cfgPath)` inside the existing `if cfgPath != ""` guard. To keep 400-vs-500
  distinguishable, either keep the leading `Validate` call (then `Save` re-validates —
  harmless, idempotent) or type-switch on the returned error.
  - Strength: honours the plan literally, removes ~12 duplicated lines and one dead branch.
  - Tradeoff: `Save` runs `validate()` a second time if `Validate` is also kept; negligible cost on a dashboard write.
  - Confidence: HIGH — `Save`'s body is four lines and already does exactly this sequence.
  - Blind spot: none.
- **Decision**: _pending_

### F2 — Create path silently installs generated options onto a colliding name, overriding user intent

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 HIGH — silently rewrites request bodies / routes to a gateway the user never associated with the generated contract
- **Dimension**: Safety & Quality
- **Location**: `proxy/web/handlers.go:1091` + `config/defaults.go:30-38`
- **Detail**: `handleCreateProvider` calls `ReapplyProviderDefaults(name)` unconditionally
  for a brand-new entry. `mergeProviderDefaults` returns early only when the name is
  absent from `providerDefaults`; on a **name collision** it installs the shared
  `OpenAI` pointer (`defaults.go:30-32`) and **unconditionally overwrites**
  `RequireBaseURL` and `SupportsCountTokens` (`:33`, `:38`) — the latter two are not
  guarded by an emptiness check, unlike the base-URL fields.

  *Probe-verified* — POST `/v1/providers` with
  `name=nim&behavior=openai&default_base_url=https://my.own.gateway/v1/chat/completions&default_api_key_env=MY_GW_KEY`
  returns **201** and persists:

  ```yaml
  nim:
    behavior: openai
    default_base_url: https://my.own.gateway/v1/chat/completions
    default_api_key_env: MY_GW_KEY
    openai:
      no_stream_usage: true
      pre_send_hook: sanitizeNIMBody
  ```

  `sanitizeNIMBody` is resolved per-request at `proxy/openai_compat.go:95-111`, so the
  user's own gateway now gets NIM-specific body rewriting (tool schema `type` →
  `_fcc_arg_type`, boolean subschemas stripped) applied to an upstream that has nothing
  to do with NVIDIA. *Probe-verified* mirror case: creating a provider named
  `anthropic` forces `SupportsCountTokens: true`, so the dispatcher passes
  `/v1/messages/count_tokens` upstream to a gateway that may not implement it — the
  exact hazard the comment at `defaults.go:34-37` warns about, in the opposite
  direction. This behaviour is **new**: before this commit `handleCreateProvider` did
  no merging at all. `TestCreateProvider` (`handlers_write_test.go:46`) uses the name
  `newprov` and never exercises a colliding name.
- **Fix**: only re-apply on create when the write is an upsert (`hadOld == true`); for a
  genuinely new entry, skip the re-apply — there is no pre-existing contract to restore.
  If a collision is considered legitimate, at minimum gate the `OpenAI` install on the
  form not having supplied its own `DefaultBaseURL`, and make `SupportsCountTokens` /
  `RequireBaseURL` fill-if-empty like the URL fields.
  - Strength: restores the "the form overwrites what the user typed" contract on create.
  - Tradeoff: a user who creates `nim` deliberately hoping to inherit the generated block loses that convenience — but they can edit YAML, and that is the documented accepted tradeoff for *clearing* options.
  - Confidence: HIGH — reproduced twice under `-race`.
  - Blind spot: unclear whether any user relies on name-collision inheritance; low likelihood, and the UI offers no hint that a name means anything special.
- **Decision**: _pending_

### F3 — Rollback hole: a concurrently-deleted provider is resurrected as a zero `Provider`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — needs a concurrent `DELETE` on the same name; single-user dashboard makes it unlikely, but the resulting state is persistent poison
- **Dimension**: Safety & Quality
- **Location**: `proxy/web/handlers.go:1166` (rollback at `:1174`, `:1182`, `:1189`)
- **Detail**: The new pre-lock existence check reads `existing, existed` under `RLock`
  (`:1146`) but **discards `existed`**. Inside the write lock the code does
  `old := cfg.Providers[name]` (`:1166`) without re-checking presence. If a
  `DELETE /v1/providers/{name}` interleaves between the `RUnlock` at `:1147` and the
  `Lock` at `:1165`, `old` is the zero `Provider`, and any of the three error branches
  writes `cfg.Providers[name] = old` — re-inserting a `Provider{Behavior: ""}` ghost.
  `validateProvider` (`config/config.go:338-349`) rejects empty behavior, so that ghost
  makes **every subsequent dashboard provider write return 400 `validation_failed` until
  the process restarts**, and it is persistable by the next successful save of any
  handler. Not reachable by the race detector in the current single-threaded tests.

  Note the pre-lock read is otherwise *correct* and slightly better than the plan's
  wording: the overlay base comes from the pre-lock read (`:1155`) while rollback uses
  the in-lock re-read (`:1166`), which is the right split.
- **Fix**: capture presence and value together under the lock and branch the rollback,
  mirroring the `hadOld` pattern already used correctly in `handleCreateProvider`:

  ```go
  old, hadOld := cfg.Providers[name]
  cfg.Providers[name] = p
  ...
  // on every error branch:
  if hadOld { cfg.Providers[name] = old } else { delete(cfg.Providers, name) }
  ```
  - Strength: closes the poison-state window and makes the two handlers symmetric.
  - Tradeoff: none.
  - Confidence: HIGH — the `handleCreateProvider` counterpart at `:1094-1098` already does exactly this.
  - Blind spot: none.
- **Decision**: _pending_


### F4 — Blank Base URL still silently erased for user-defined providers

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 MEDIUM — a 200 OK that drops the field, then every request fails `missing base_url`
- **Dimension**: Safety & Quality
- **Location**: `config/defaults.go:20-23`, `proxy/web/handlers.go:1171`
- **Detail**: `mergeProviderDefaults` returns `p` untouched when the name is not in
  `providerDefaults`, so a blank `default_base_url` on a user-defined provider survives
  all the way to disk: `decodeProviderForm` sets it to `""` (`forms.go:49`), nothing
  backfills, `validateProvider` skips the URL check when empty and `RequireBaseURL` is
  false (`config.go:351`), and `Marshal` omits the field (`omitempty`). *Probe-verified*:
  PUT to `mygateway` with `default_base_url=` returns 200 and the persisted stanza
  loses the URL entirely.

  **This is out of the plan's scope, not a regression** — `plan.md:94-97` explicitly
  scopes D2 to "anything the *generated table* declares", and `plan.md:66-68` limits the
  blast radius to known providers. The commit message's phrasing ("a blank Base URL
  input erases the codegen'd `default_base_url`") is accurate as written. Flagged
  because the fix as shipped reads broader than it is, and gap #4 in the plan's overview
  is only half-closed.
- **Fix**: either (a) leave as-is and note in the plan that gap #4 is closed for known
  providers only, or (b) treat an empty `default_base_url` on update as "unchanged"
  rather than "cleared" — which needs a way to distinguish the two, and is a real
  design decision.
  - Confidence: MEDIUM — (b) requires semantics the plan did not settle.
- **Decision**: _pending_

### F5 — `Config.Validate` is new exported API not listed in the plan's Files Touched

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — follows directly from F1; disappears if F1 is fixed as written
- **Dimension**: Plan Adherence
- **Location**: `config/config.go:296-303`
- **Detail**: The plan's Files Touched table (`plan.md:174-181`) lists six files and does
  not include `config/config.go`. F1's separate-gate approach forced 9 lines of new
  public surface. Had D3 been implemented as written, no new export would exist at all
  — `Save` already gates validation. The exported symbol is well documented and
  harmless, but it is unplanned API growth. Its doc also omits the
  caller-must-hold-lock convention that `ReapplyProviderDefaults` (`defaults.go:49-50`)
  documents, a minor internal inconsistency.
- **Fix**: fold into F1's fix. If `Validate` is retained for any reason, add the locking
  note to its doc comment.
- **Decision**: _pending_

### F6 — Orphaned truncated doc comment in the new test file

- **Severity**: 👁️ OBSERVATION (NIT — cosmetic)
- **Impact**: 🏃 LOW — one-line edit
- **Dimension**: Pattern Consistency
- **Location**: `proxy/web/handlers_write_test.go:512-513`
- **Detail**: Line 512 ends mid-sentence — `// TestUpdateProvider_InvalidConfigReturns400 asserts the validation gate: a` — and line 513 is blank, so the comment is attached to nothing and the actual test at `:624` carries no doc comment while all its neighbours do. Leftover from an edit.
- **Fix**: complete the sentence and remove the blank line.
- **Decision**: _pending_


### F7 — Test-quality gaps: misleading comment, uncovered rollback branch, unpinned aliasing

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — test-only; the behaviours they would guard are already correct today
- **Dimension**: Success Criteria
- **Location**: `proxy/web/handlers_write_test.go:80-114`, `:571-572`; `config/config_test.go:1163-1197`
- **Detail**: Three gaps in otherwise strong coverage:
  1. `TestUpdateProvider_UserDefinedProviderKeepsSettings`'s comment says *"Submit
     without a base_url"* but the request body at `:574` **does** include
     `default_base_url=https://my.gateway/v1/chat/completions`. The comment contradicts
     the code and the test does not cover what it claims (see F4).
  2. `TestCreateProvider_SaveFailure` exercises only the `hadOld == false` branch, so the
     `if hadOld { cfg.Providers[name] = old }` rollback at `handlers.go:1094-1098` is
     untested.
  3. `TestApplyDefaults_IdempotentAfterWrite` deep-compares `Provider` structs whose only
     pointer field is `OpenAI`, so the comparison is pointer *identity*; the real
     aliasing check is the separate `*second.OpenAI != before` at `:1189`. Nothing pins
     the "`providerDefaults` is uncontaminated after a write" contract — the invariant
     `defaults.go:15-18` depends on. `-race` cannot catch it because no shipped code
     writes the pointee *today*, which is exactly the condition that comment warns about.

  Also note the five new `config/config_test.go` tests are standalone funcs rather than
  table-driven, unlike the file's existing `TestLoad`/`TestValidate` style that
  AGENTS.md prefers. The `proxy/web` tests correctly do use subtests where they vary
  inputs.
- **Fix**: correct the `:571` comment (or change the body to actually omit the URL);
  add a `hadOld == true` create-rollback case; add an aliasing assertion that
  `providerDefaults` is unchanged after a full handler write.
  - Confidence: HIGH on 1 and 2; 3 is a guard rather than a defect fix.
- **Decision**: _pending_

### F8 — `handleCreateProvider` still silently upserts (G4) — correctly deferred

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 NONE — deliberate, documented scope call
- **Dimension**: Scope Discipline
- **Location**: `proxy/web/handlers.go:1086-1087`
- **Detail**: POST to an existing name is a silent upsert while PUT 404s. Listed as G4 in
  the plan's "What We're NOT Doing" (`plan.md:159-161`) and correctly left alone. Recorded
  only because F2's create-path re-apply means the upsert branch is now the one that
  inherits generated options — worth remembering when G4 is eventually fixed.
- **Decision**: ACCEPTED — matches plan scope

## Areas verified clean

- **Aliasing / shared-pointer invariant holds.** Grep for `OpenAI.<field> =` across the
  tree returns zero hits; `providerDefaults` is read only at `defaults.go:20` and
  `:69`. `proxy/openai_compat.go:95-111` only reads. The `p.OpenAI == nil` check at
  `:30` correctly *preserves* an existing pointer rather than clobbering it. Serializing
  the shared pointee to disk is materialization, not mutation — reload allocates fresh.
- **D2 shared-merge refactor is real.** `applyDefaults` (`:79`) and
  `ReapplyProviderDefaults` (`:56`) both call `mergeProviderDefaults` and nothing else
  mutates those fields, so load and write paths cannot drift. `applyDefaults` still
  injects absent providers (`:69-78`); `ReapplyProviderDefaults` returns early on a
  missing key (`:52-55`) and never injects — pinned by
  `TestReapplyProviderDefaults_DoesNotInjectAbsentProviders` and end-to-end by
  `TestUpdateProvider_DoesNotInjectOtherProviders` (which correctly asserts a *delta*,
  not an absolute count, since load-time `applyDefaults` injects ~18 providers).
- **Lock discipline correct.** No `RLock` while holding `Lock`. Every error branch
  between `Lock` and `Unlock` releases in both handlers (`:1099/1111/1122/1127`,
  `:1175/1183/1190/1195`). The 404 early path holds no lock. `renderProvidersTable` runs
  only after `Unlock` and uses `ProvidersSnapshot`. `BuildMatchers` (which self-locks) is
  called only in mapping handlers, never provider handlers.
- **No dangling-reference hazard from the wider `Validate`.** `handleUpdateProvider`
  discards the form's `name` (`:1155` — `_, p, err :=`) and keys off the URL path, so
  provider rename is impossible and no mapping can be dangled. Adding a provider cannot
  invalidate another.
- **Scope discipline held.** Mapping handlers keep `SaveData` (`:1298`, `:1354`, `:1404`);
  `handleDeleteProvider` untouched. No `openai:` UI exposure, no `providers.yaml` or
  codegen change, no `Provider` field or YAML-tag change. Codegen invariant verified:
  `go generate ./...` produces zero diff.
- **All seven D4 tests present with real assertions** — `TestApplyDefaults_IdempotentAfterWrite`,
  `TestDecodeProviderForm_PreservesOpenAIOptions`, `..._PreservesAnthropicVersion`,
  `TestUpdateProvider_ZenKeepsNoStreamUsage` (asserts `no_stream_usage: true` in
  persisted YAML), `..._EmptyBaseURLRestoredFromDefaults`,
  `..._InvalidConfigReturns400` (400 + `validation_failed` + byte-identical file),
  `..._HTMXStillRendersTable` (asserts `<table` present and `"status"` absent). No test
  weakened; no `t.Skip` anywhere in `config/` or `proxy/web/`.
- **Conventions compliant.** `%w` wrapping at `forms.go:44`, exported symbols documented,
  stdlib `http.ServeMux` Go 1.22 patterns at `handlers.go:347-355`, no external router.

## Unrun manual criteria

1.3, 2.5, 3.3, 4.5 remain `[ ]` — all four require a browser against `mage run`. Note
that F2 was found precisely in the territory item 2.5 covers (user-defined provider
survives an edit), which suggests that check should be extended to cover *creating* a
provider whose name matches a known one.

