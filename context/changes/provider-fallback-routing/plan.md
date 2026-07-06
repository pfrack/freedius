# Provider Fallback Routing Implementation Plan

## Overview

Add explicit per-mapping fallback chains to freedius. Each `config.Mapping`
gains an ordered `Fallback []Mapping` list. When the primary target (or a
fallback target) fails — pre-flight config error, transport failure, or an
upstream HTTP error status (429/5xx/401/403/other 4xx) — before any response
bytes have reached the client, the dispatcher retries against the next entry
in the chain. Exhausting every entry returns an aggregated error describing
every attempt. A fallback that succeeds is logged server-side only (no
response headers, no event-bus change). The web UI's mapping dialog gets a
repeatable, unbounded provider+model row group so chains can be built without
hand-editing YAML.

## Current State Analysis

- `config.Mapping` (`config/config.go:58-61`) is `{ProviderName, ModelString}`
  — strictly single-target. No schema precedent for a list field exists
  anywhere in the project's history.
- `Dispatcher.resolveMapping` (`proxy/proxy.go:94-111`) does exact-match →
  family-pattern → miss, and never retries after a mapping is resolved.
- `Dispatcher.ServeHTTP` (`proxy/proxy.go:268-310`) calls `adapter.Handle`
  exactly once. The `!ww.wroteHeader` branch is the only point that both
  knows a call failed and can prove no bytes reached the client — the sole
  safe location for a retry.
- The **Adapter Return Contract** (`context/foundation/lessons.md:33-43`):
  once an adapter writes any response bytes it must return `nil`. Today,
  `OpenAICompatibleAdapter.Handle` (`proxy/openai_compat.go:146-149`) calls
  `translateUpstreamError(w, resp)` directly on any `resp.StatusCode >= 400`
  and returns `nil` — the dispatcher never learns the call failed. This is
  the specific thing Phase 2 changes.
- `AnthropicCompatibleAdapter.Handle` (`proxy/anthropic_compat.go:86-97`)
  always delegates to `httputil.ReverseProxy`, which always writes a response
  (success or its own `freediusErrorHandler`-produced error) before `Handle`
  returns — `ww.wroteHeader` is always `true` for this adapter today.
- `MixAdapter.Handle` (`proxy/mix.go:49-79`) is a pure router to one of the
  above two; it has no error-handling logic of its own beyond a pre-write
  `*configError` on bad `base_url` parsing.
- The web UI mapping form (`proxy/web/forms.go:48-58`, `handlers.go:495-591`,
  `templates/mappings.html`, `templates/mappings-table.html`) has a single
  Provider-select + Model-input pair; no repeatable-row-group pattern exists
  anywhere in the templates today.
- `2026-07-02-testing-proxy-integration/research.md:99` explicitly validated
  the *absence* of fallback as correct, tested behavior — this plan is a
  deliberate, considered reversal of that invariant, not a bug fix.
- Full architectural grounding: `context/changes/provider-fallback-routing/research.md`.

## Desired End State

A user can add one or more fallback `{provider_name, model_string}` entries
to any mapping, either by hand-editing `config.yaml` or via the web UI's
mapping dialog. When the primary target fails in a way covered by this
plan's trigger scope, freedius transparently retries the next entry, and the
next, until one succeeds or the chain is exhausted. On exhaustion, the
client receives one Anthropic-shaped error response whose message
summarizes every attempt (target + failure reason). A successful fallback
is visible only in the server log (grep for `fallback` in freedius's stdout
JSON logs) — not in response headers, not in the TUI/web dashboard.

**Verification**: `go test ./...` passes; a manually configured mapping with
a deliberately broken primary (bad API key env var) and a working fallback
returns a normal 200 response, and the server log shows the primary's
failure and the fallback's success; a mapping with every target broken
returns one JSON error body listing all attempts.

### Key Discoveries:

- `resp.StatusCode >= 400` inside `OpenAICompatibleAdapter.Handle`
  (`proxy/openai_compat.go:146-149`) is the only place upstream HTTP errors
  are currently swallowed into a `nil` return — this is the crux of Phase 2.
- `config.Mapping` has no existing "identity" concept beyond
  `{ProviderName, ModelString}` value equality — with inline fallback pairs
  (not references to other mapping names), "cycle detection" reduces to
  rejecting an exact duplicate `{ProviderName, ModelString}` pair appearing
  more than once in one chain (counting the primary as the first entry) —
  retrying an identical target twice can never produce a different outcome.
- `wroteHeaderResponseWriter` (`proxy/proxy.go:395-415`) already tracks
  whether headers were written and is reused as-is for each fallback
  attempt — a fresh instance wraps the original `http.ResponseWriter` for
  every attempt in the chain, since a used instance can't be "reset."
- `config.Provider`/`config.Mapping` are passed to `Handle` by value on
  every call (`proxy/provider.go:12-19`) — no adapter carries state across
  calls, so nothing needs resetting between fallback attempts.

## What We're NOT Doing

- No mid-stream fallback (after `200`+SSE headers are sent). A stream that
  starts successfully and then fails is not retried; the client sees the
  broken/truncated stream as it does today.
- No response headers or event-bus fields exposing fallback activity.
  Observability for this feature is server-log-only, by explicit decision.
- No fixed cap on fallback chain length in config or in the web UI — the
  configured list length is the only limit; there is no separate
  "max attempts" ceiling.
- No changes to `extractFamily`/family-pattern mapping-name resolution —
  that mechanism is unrelated and untouched.
- No provider health tracking, circuit breakers, or cross-request state.
  This feature is purely per-request: every request that hits a failing
  primary walks its own fresh copy of the fallback chain.
- No TUI dashboard changes (only the web UI mapping form is in scope, per
  the user's decision — no `tui/`-style interface exists in this codebase
  today to change anyway).

## Implementation Approach

Work bottom-up: schema first (Phase 1), then the adapter contract change
that makes upstream-status fallback possible at all (Phase 2), then the
dispatcher retry loop that actually uses both (Phase 3), then the web UI
(Phase 4), then integration coverage across the whole chain (Phase 5). Each
phase is independently testable — Phase 2 in particular touches shared,
heavily-tested adapter code and must land cleanly with all existing adapter
tests updated before Phase 3 builds on top of it.

## Critical Implementation Details

- **Timing & lifecycle**: the shared timeout budget for the whole chain is
  `perAttemptTimeout × multiplier`, set via one `context.WithTimeout` in
  `Dispatcher.ServeHTTP` wrapping the entire retry loop (not per-adapter).
  Individual adapters keep using `r.Context()` for their own per-call
  cancellation as they do today (e.g. `openai_compat.go:116`), but that
  context now derives from the dispatcher's chain-wide deadline instead of
  a fresh per-call timeout — so a later fallback attempt naturally gets
  whatever time is left in the shared budget, not a fresh full timeout.
  `multiplier` is a new config-level or flag-level knob (see Phase 1) with a
  sensible default (e.g. `2`) so existing single-target configs see
  unchanged effective timeout behavior (chain length 1 → budget == today's
  per-attempt timeout × multiplier, still comfortably above the single
  attempt's own timeout).
- **State sequencing**: the dispatcher must attempt targets strictly in
  chain order (primary first, then `Fallback[0]`, `Fallback[1]`, ...) and
  stop at the first success. A fresh `wroteHeaderResponseWriter` wraps the
  *original* `w` for every attempt — never the previous attempt's used
  wrapper — since a wrapper that never got written to is indistinguishable
  from a fresh one, but reusing one that's already flagged `wroteHeader`
  would incorrectly block writing the eventual success response.

## Phase 1: Config Schema & Validation

### Overview

Add the `Fallback` field to `config.Mapping`, wire cycle/duplicate detection
into `validateMapping`, and add the shared-timeout multiplier knob.

### Changes Required:

#### 1. `Mapping` struct

**File**: `config/config.go`

**Intent**: Add an ordered list of fallback targets to `Mapping`, using the
same `{ProviderName, ModelString}` shape as the primary target (inline
pairs, not references to other mapping names).

**Contract**: `Mapping.Fallback []Mapping` tagged `yaml:"fallback,omitempty"`.
Each entry is a `Mapping` value reusing the existing `provider_name` /
`model_string` YAML keys — no new nested type. `Fallback` entries themselves
never carry their own `Fallback` field populated (not recursively chained);
document this as a struct comment.

#### 2. Fallback validation

**File**: `config/config.go`

**Intent**: Extend `validateMapping` so every fallback entry is validated
exactly like the primary (`provider_name` must reference a known provider,
`model_string` must be non-empty and safe), and reject a chain containing
an exact duplicate `{ProviderName, ModelString}` pair (the primary counts as
the first entry in the dedup check).

**Contract**: `validateMapping(path, name string, m Mapping, providers map[string]Provider) error`
gains a loop over `m.Fallback` calling the same field checks already applied
to the primary, plus a `seen map[Mapping]bool` (or equivalent) dedup pass
across `{primary} + Fallback` before validating each entry, returning a
`config:` prefixed error naming the duplicate pair on collision — following
the existing error-message format used elsewhere in this function
(`config/config.go:253-284`).

#### 3. Shared timeout budget multiplier

**File**: `config/config.go` (or `cmd/freedius/main.go` flag, whichever
matches the existing precedent for `streamTimeout` — check
`resolveStreamTimeout` in `cmd/freedius/main.go` for the established
flag/env/default resolution pattern and mirror it)

**Intent**: Introduce a configurable multiplier applied to the existing
per-attempt stream timeout to derive the whole-chain shared budget (see
Critical Implementation Details).

**Contract**: A new resolved value (e.g. `fallbackTimeoutMultiplier`,
default `2`) threaded from wherever `streamTimeout` is currently resolved
through to `Dispatcher` construction, following the same flag/env/default
precedence pattern `resolveStreamTimeout` already establishes.

### Success Criteria:

#### Automated Verification:

- `go test ./config/...` passes
- `go build ./...` passes
- `go vet ./...` passes

#### Manual Verification:

- A `config.yaml` with a mapping containing 2 fallback entries loads without
  error via `freedius --config <path>` and appears correctly when re-saved
  (round-trip through `Save`/`Load` preserves the fallback list)
- A config with a duplicate `{provider_name, model_string}` pair between the
  primary and a fallback entry is rejected at load time with a clear error

---

## Phase 2: Adapter Return Contract Change

### Overview

Change all three adapters (`OpenAICompatibleAdapter`, `AnthropicCompatibleAdapter`,
`MixAdapter`) so an upstream HTTP error response (4xx/5xx) is surfaced to the
dispatcher as a typed, pre-header error instead of being written directly —
for every mapping, not conditionally. Update every existing test that
currently asserts the old "write directly and return nil" behavior.

### Changes Required:

#### 1. New typed upstream-error type

**File**: `proxy/errors.go`

**Intent**: Introduce a typed error (alongside the existing `configError`)
that carries everything `translateUpstreamError` currently needs to write
an Anthropic-shaped error response — status code, error type, message,
retry-after — but as data on an `error` value instead of directly-written
bytes.

**Contract**: A new struct (e.g. `upstreamError{status int; errType string; message string; retryAfter int}`)
implementing `error`. `translateUpstreamError`'s classification switch
(`proxy/errors.go:87-110`) is refactored into a pure function that builds
this typed value instead of calling `writeAnthropicError` directly; the
existing direct-write helper is kept for the dispatcher's own final-write
use (see Phase 3) so the classification logic isn't duplicated.

#### 2. `OpenAICompatibleAdapter.Handle`

**File**: `proxy/openai_compat.go`

**Intent**: Replace the direct `translateUpstreamError(w, resp); return nil`
call (`openai_compat.go:146-149`) with returning the new typed upstream
error — no response bytes written for this case anymore.

**Contract**: `resp.StatusCode >= 400` branch returns the typed error from
change #1 instead of writing to `w`. The 200-and-above streaming path
(`openai_compat.go:151-175`) is unchanged — it already writes headers before
any failure could occur, consistent with the "pre-response only" fallback
scope decision.

#### 3. `AnthropicCompatibleAdapter.Handle` / `freediusErrorHandler`

**File**: `proxy/anthropic_compat.go`, `proxy/errors.go`

**Intent**: `httputil.ReverseProxy` always writes before `Handle` returns
today, which makes this adapter fallback-ineligible regardless of the
contract change. To bring it in line with the other adapters for the
upstream-4xx/5xx case, stop delegating status-code translation to
`ReverseProxy`'s automatic passthrough of the upstream response and instead
inspect the upstream response before any write — meaning this adapter needs
to move off `httputil.ReverseProxy`'s single-shot `ServeHTTP` for the
response-forwarding step, or wrap it so the response is buffered/inspected
before commit. `freediusErrorHandler` (`proxy/errors.go:202-239`, which
only fires on transport-level errors, not upstream HTTP error statuses) is
unaffected by this specific change — transport errors it handles are
already pre-write today and already fallback-eligible; this task is about
the case where the upstream *responds* with a 4xx/5xx.

**Contract**: `Handle` no longer always returns `nil`. When the upstream
response status is `>= 400`, return the same typed upstream error type from
change #1 (reading/discarding the body per the existing
`translateUpstreamError` snippet-and-drain pattern at `errors.go:73-81`)
instead of forwarding it via `ReverseProxy`. Successful (`< 400`) responses
still stream through unchanged.

#### 4. `MixAdapter.Handle`

**File**: `proxy/mix.go`

**Intent**: No changes needed beyond what changes #2 and #3 propagate
automatically — `MixAdapter` purely delegates to the anthropic/openai
sub-adapters (`proxy/mix.go:56-78`) and returns whatever they return.

**Contract**: Confirm via tests that `MixAdapter.Handle` correctly
propagates the new typed error from either sub-adapter without
modification.

#### 5. Update existing adapter contract tests

**File**: `proxy/error_contract_test.go`, `proxy/adapter_errors_test.go`,
`proxy/error_propagation_test.go`, `proxy/openai_compat_test.go`,
`proxy/anthropic_compat_test.go`, `proxy/mix_test.go`

**Intent**: Every test asserting "adapter writes the error directly and
returns nil" (e.g. `TestOpenAICompat_Upstream429_ReturnsAnthropicFormat`,
`proxy/error_contract_test.go:18-56`) now needs to assert the adapter
returns the new typed error instead, with the response recorder showing no
bytes written. The final client-facing byte-for-byte response shape (status
429, `rate_limit_error`, retry-after 42) is unchanged — it now happens one
layer up, in the dispatcher (Phase 3) — so equivalent coverage of that
shape should exist at the dispatcher level after Phase 3, not be dropped.

**Contract**: Rewrite each affected test's assertions from "recorder has
status X" to "returned error is of the new typed-error kind with fields
matching X" per adapter; do not delete the underlying classification
coverage — move it to whichever layer now owns writing (Phase 3's
dispatcher-level test, or a direct unit test of the refactored
classification function from change #1).

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` passes with all adapter tests updated (no skipped
  or deleted assertions — coverage moved, not dropped)
- `go vet ./...` passes

#### Manual Verification:

- Manually point a mapping at a provider returning a real 429 (or a local
  mock returning 500) with **no fallback configured** and confirm the
  client still receives the exact same Anthropic-shaped error response as
  before this change (status, error type, retry-after all unchanged) —
  this proves the contract change is transparent when no fallback exists

---

## Phase 3: Dispatcher Fallback Loop

### Overview

Implement the retry loop in `Dispatcher.ServeHTTP`: walk the primary + its
`Fallback` chain in order, using the shared timeout budget from Phase 1 and
the typed pre-header errors from Phase 2, stopping at the first success or
returning an aggregated error on exhaustion.

### Changes Required:

#### 1. Retry loop in `ServeHTTP`

**File**: `proxy/proxy.go`

**Intent**: Replace the single `adapter.Handle` call (`proxy/proxy.go:268-310`)
with a loop over `[]config.Mapping{mapping} + mapping.Fallback`, resolving
each entry's provider fresh (reusing the existing provider-lookup logic
from `resolveMapping`, `proxy/proxy.go:106-110`), attempting `Handle` with a
new `wroteHeaderResponseWriter` each iteration, and continuing to the next
entry only when the returned error is one of the typed, pre-header errors
from Phase 2 (i.e. `!ww.wroteHeader` still holds — this invariant doesn't
change). One shared `context.WithTimeout` (Phase 1's budget) wraps the
entire loop and is used to derive `r`'s context for every attempt.

**Contract**: On the first success (`err == nil` or `ww.wroteHeader` became
`true` mid-stream with no retryable error), return immediately — no further
attempts. On exhaustion (every entry returned a pre-header error), build one
aggregated error response (see change #2) instead of writing the last
attempt's error alone. Each attempt's outcome (target, error type/message)
is collected into a slice for both the aggregated response and the log line
in change #3. A provider-not-registered condition for a fallback entry
(mirroring the existing `provider == nil` handling at `proxy.go:211-230`)
counts as that attempt's failure and moves to the next entry, consistent
with treating fallback-entry misconfiguration the same as any other
attempt failure.

#### 2. Aggregated exhaustion error

**File**: `proxy/errors.go` or `proxy/proxy.go`

**Intent**: When every target in the chain fails, build one Anthropic-shaped
error response whose message lists each attempt and its failure reason,
instead of just the last attempt's error.

**Contract**: A new helper (e.g. `writeAggregatedFallbackError(w http.ResponseWriter, attempts []fallbackAttempt)`)
constructs the response using the existing `writeAnthropicError`
(`proxy/errors.go:44-65`) envelope shape (so client-side Anthropic-error
parsing is unaffected), with `message` summarizing all attempts (e.g.
`"all providers failed: nim/step-3.5 (rate_limit_error), zen/claude-sonnet-4-6 (authentication_error)"`)
and a `status`/`errType`/`retryAfter` derived from the *last* attempt's
classification (simplest sensible choice — avoids inventing a new
combination rule across heterogeneous error types).

#### 3. Server-side logging of fallback attempts

**File**: `proxy/proxy.go`

**Intent**: Log every fallback transition (each failed attempt, and whether
the eventual outcome was success-via-fallback or full exhaustion) at a
level that's visible in normal operation, per the "server-side log only"
observability decision.

**Contract**: A `d.Logger.Warn` (per failed attempt) / `d.Logger.Info` (on
eventual fallback success) call inside the retry loop, following the
existing structured-log-field convention used elsewhere in this file (e.g.
`proxy.go:274-282`'s `"request_id"`, `"provider"`, `"err"` fields) — include
enough fields (attempt index, provider, model, error type) to `grep` a
specific request's whole fallback path from the log.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` passes, including new dispatcher-level tests
  covering: primary succeeds (no fallback triggered), primary fails then
  fallback succeeds, all entries fail (aggregated error returned), a
  fallback entry with an unregistered provider is skipped correctly
- `go vet ./...` passes

#### Manual Verification:

- Configure a mapping with a broken primary (bad API key env var) and a
  working fallback; confirm the client gets a normal 200 response and the
  server log shows the primary's failure followed by the fallback's success
- Configure a mapping where every entry is broken; confirm the client
  receives one JSON error body whose message names every attempted
  provider/model and its failure reason
- Confirm total wall-clock time for the full-exhaustion case stays within
  the shared timeout budget (doesn't multiply unboundedly per entry)

---

## Phase 4: Web UI Support

### Overview

Add a repeatable, unbounded provider+model row group to the mapping dialog
so fallback chains can be configured without hand-editing YAML, plus display
of the configured chain in the mappings table.

### Changes Required:

#### 1. Form decode/validate for fallback list

**File**: `proxy/web/forms.go`

**Intent**: Extend `decodeMappingForm` to read a variable-length set of
fallback provider/model pairs from the submitted form, and extend
`validateMappingFields` to validate each one the same way the primary is
validated today, reusing Phase 1's `config.validateMapping` duplicate/cycle
check (or an equivalent local check) so the web path and the raw-YAML path
enforce the same rules.

**Contract**: `decodeMappingForm(r *http.Request) (string, config.Mapping, error)`
reads indexed form fields (e.g. `fallback_provider_name[]` /
`fallback_model_string[]`, paired by position) into `config.Mapping.Fallback`.
`validateMappingFields` gains the same per-entry + duplicate checks Phase 1
added to `config.validateMapping`.

#### 2. Repeatable row group in the mapping dialog

**File**: `proxy/web/templates/mappings.html`

**Intent**: Add an "Add fallback" control that appends another
Provider-select + Model-input row (mirroring the primary target's existing
fields), removable individually, with no upper bound on how many rows can
be added. `editMapping(...)` (currently populating only name/provider/model,
`mappings.html`) is extended to pre-populate any existing fallback rows when
opening the dialog for an existing mapping.

**Contract**: New inline `<script>` logic for add/remove-row behavior
(no existing pattern in this codebase to reuse — this is genuinely new
client-side logic); form field naming matches change #1's expected indexed
names.

#### 3. Fallback chain display in the mappings table

**File**: `proxy/web/templates/mappings-table.html`, `proxy/web/types.go`,
`proxy/web/handlers.go`

**Intent**: Show the configured fallback chain (if any) alongside each
mapping's primary provider/model in the table, so a user can see the whole
chain at a glance without opening the edit dialog.

**Contract**: `mappingRow` (`proxy/web/types.go:52-56`) gains a `Fallback []mappingRow`-shaped
field (or a pre-formatted string) populated by `handleMappings`/
`renderMappingsTable` (`proxy/web/handlers.go:193-234`, `297-339`) from
`m.Fallback`; the table template renders it as a compact list per row (e.g.
"→ zen/claude-sonnet-4-6, → nim/step-3.5").

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/web/...` passes
- `go vet ./...` passes

#### Manual Verification:

- Open the web UI, add a mapping with 2 fallback rows via the dialog, save,
  and confirm the mappings table shows the chain
- Reopen the same mapping for edit and confirm all fallback rows
  pre-populate correctly
- Remove a fallback row, save, and confirm it's gone from both the table
  and the underlying `config.yaml`
- Add and remove rows repeatedly to confirm there's no hard limit on count

---

## Phase 5: End-to-End Testing & Docs

### Overview

Add integration coverage exercising the full chain end-to-end across all
trigger types, and document the feature with a worked example.

### Changes Required:

#### 1. Integration tests

**File**: `proxy/proxy_test.go` (or a new `proxy/fallback_test.go`)

**Intent**: Cover the full dispatcher pipeline (config → resolveMapping →
retry loop → adapter) for each trigger category from the design decisions:
pre-flight config error (missing API key) on primary with working fallback;
transport failure (connection refused) on primary with working fallback;
upstream 429/500/401 on primary with working fallback; full-chain exhaustion
with mixed failure types producing the aggregated error; duplicate-entry
config rejected at load (already covered in Phase 1, cross-referenced here
for completeness).

**Contract**: New test functions following the existing table-driven /
`httptest.NewServer` patterns already used in `proxy/proxy_test.go` and
`proxy/error_contract_test.go`.

#### 2. Example config

**File**: `config.example.yaml`

**Intent**: Add one worked fallback example to the starter config so new
users discover the feature, without changing the behavior of the existing
mappings that ship today.

**Contract**: Extend one existing mapping (e.g. `opus`) with a `fallback:`
list pointing at another already-declared provider, following the existing
YAML formatting style in this file.

### Success Criteria:

#### Automated Verification:

- `go test ./...` passes (full suite, all packages)
- `go vet ./...` passes
- `go build ./...` passes

#### Manual Verification:

- Fresh-install flow: copy `config.example.yaml`, start freedius, confirm
  the documented fallback example loads without error
- Full manual regression pass on the four phase-level manual verification
  steps above, run together in one session to confirm no interaction bugs
  between config loading, dispatch, and the web UI

---

## Testing Strategy

### Unit Tests:

- `config` package: `Fallback` field validation (per-entry checks,
  duplicate/cycle detection), YAML marshal/unmarshal round-trip with
  fallback entries present and absent (`omitempty` behavior)
- `proxy` package: each adapter's new typed-error return path (Phase 2);
  dispatcher retry loop transitions (success, fallback-success, exhaustion,
  unregistered-fallback-provider skip) (Phase 3)
- `proxy/web` package: form decode/validate for variable-length fallback
  lists (Phase 4)

### Integration Tests:

- Full request lifecycle through a real `httptest.Server`-backed provider
  chain covering every trigger category (Phase 5)
- Web UI create/edit/delete cycle for a mapping with a multi-entry fallback
  chain (Phase 4/5)

### Manual Testing Steps:

1. Configure a mapping with a broken primary and working fallback; confirm
   transparent success and check the server log for the fallback trail.
2. Configure a mapping where every entry fails; confirm one aggregated
   error naming every attempt.
3. Build a 3+ entry fallback chain via the web UI dialog, save, reload the
   page, and confirm it round-trips correctly through the table and back
   into the edit dialog.
4. Confirm a plain single-target mapping (no `fallback:` key) behaves
   byte-for-byte identically to today's behavior on both success and
   failure paths.

## Performance Considerations

The shared timeout budget (Phase 1/3) directly bounds worst-case added
latency for the failure path, in line with the PRD's "imperceptible
overhead" NFR (`context/foundation/prd.md:87`) — a fully-exhausted chain
cannot exceed `perAttemptTimeout × multiplier` regardless of how many
fallback entries are configured, at the cost of later entries in a long
chain receiving a shrinking effective timeout.

## Migration Notes

Purely additive: `Fallback` is `omitempty` in YAML and defaults to an empty
slice, so every existing `config.yaml` in the wild (including
`config.example.yaml`'s current single-target mappings) continues to work
unchanged with no fallback behavior triggered. No data migration is needed.

## References

- Research: `context/changes/provider-fallback-routing/research.md`
- Adapter Return Contract: `context/foundation/lessons.md:33-43`
- Historical no-fallback invariant: `context/archive/2026-07-02-testing-proxy-integration/research.md:99`
- Dispatcher retry insertion point: `proxy/proxy.go:268-310`
- Upstream error classification (to be refactored): `proxy/errors.go:72-113`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Config Schema & Validation

#### Automated

- [x] 1.1 `go test ./config/...` passes — 69f869a
- [x] 1.2 `go build ./...` passes — 69f869a
- [x] 1.3 `go vet ./...` passes — 69f869a

#### Manual

- [ ] 1.4 Config with 2 fallback entries loads and round-trips correctly
- [ ] 1.5 Config with a duplicate provider+model pair is rejected at load

### Phase 2: Adapter Return Contract Change

#### Automated

- [ ] 2.1 `go test ./proxy/...` passes with all adapter tests updated
- [ ] 2.2 `go vet ./...` passes

#### Manual

- [ ] 2.3 No-fallback-configured mapping produces byte-for-byte unchanged
      client response on upstream error

### Phase 3: Dispatcher Fallback Loop

#### Automated

- [ ] 3.1 `go test ./proxy/...` passes including new dispatcher fallback
      tests
- [ ] 3.2 `go vet ./...` passes

#### Manual

- [ ] 3.3 Broken primary + working fallback returns transparent 200; log
      shows fallback trail
- [ ] 3.4 Fully broken chain returns one aggregated error naming every
      attempt
- [ ] 3.5 Full-exhaustion wall-clock time stays within shared timeout budget

### Phase 4: Web UI Support

#### Automated

- [ ] 4.1 `go test ./proxy/web/...` passes
- [ ] 4.2 `go vet ./...` passes

#### Manual

- [ ] 4.3 Add mapping with 2 fallback rows via dialog; table shows chain
- [ ] 4.4 Reopen for edit; fallback rows pre-populate correctly
- [ ] 4.5 Remove a fallback row; confirm removal in table and config.yaml
- [ ] 4.6 Repeated add/remove confirms no hard row-count limit

### Phase 5: End-to-End Testing & Docs

#### Automated

- [ ] 5.1 `go test ./...` full suite passes
- [ ] 5.2 `go vet ./...` passes
- [ ] 5.3 `go build ./...` passes

#### Manual

- [ ] 5.4 Fresh-install flow with `config.example.yaml`'s fallback example
      loads cleanly
- [ ] 5.5 Full combined manual regression pass across all phases
