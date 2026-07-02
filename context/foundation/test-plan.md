# Test Plan

> Phased test rollout for this project. Strategy is frozen at the top
> (§1–§5); cookbook patterns at the bottom (§6) fill in as phases ship.
> Read before writing any new test.
>
> Refresh: re-run `/10x-test-plan --refresh` when stale (see §8).
>
> Last updated: 2026-07-02

## 1. Strategy

Tests follow three non-negotiable principles for this project:

1. **Cost × signal.** The cheapest test that gives a real signal for the
   risk wins. Do not promote to e2e because e2e "feels safer." Do not put a
   vision model on top of a deterministic visual diff that already catches
   the regression.
2. **User concerns are first-class evidence.** Risks anchored in "the
   team is worried about X, and the failure would surface somewhere in
   <area>" carry the same weight as PRD lines or hot-spot data.
3. **Risks are scenarios, not code locations.** This plan documents *what
   could fail* and *why we believe it's likely* — drawn from documents,
   interview, and codebase *signal* (churn, structure, test base). It does
   NOT claim to know which line owns the failure. That knowledge is
   produced by `/10x-research` during each rollout phase. If the plan and
   research disagree about where the failure lives, research is the
   ground truth.

Hot-spot scope used for likelihood weighting: `proxy/ cmd/ config/ internal/`.

## 2. Risk Map

The top failure scenarios this project must protect against, ordered by
risk = impact × likelihood. Risks are failure scenarios in user / business
terms, not test names. The Source column cites the *evidence that surfaced
this risk* — never a specific file as "where the failure lives" (that is
research's job, see §1 principle #3).

| # | Risk (failure scenario) | Impact | Likelihood | Source (evidence — not anchor) |
|---|-------------------------|--------|------------|-------------------------------|
| 1 | Translation layer returns wrong format (e.g. OpenAI error body from Anthropic endpoint) — Claude Code rejects response, session breaks | High | High | interview Q2; hot-spot dir `proxy/` (26 commits/30d) |
| 2 | Streaming regression — partial chunks, mid-stream errors, or malformed SSE silently dropped or reassembled wrong — agent freezes or gets truncated output | High | Medium | interview Q4; PRD FR-002 ("streaming responses work") |
| 3 | Config-to-provider routing sends request to wrong provider or model — user gets garbage from a model they didn't choose | High | Medium | interview Q1; PRD FR-003 ("mapping is transparent") |
| 4 | Config validation gap — invalid YAML or missing keys crashes the gateway mid-session instead of surfacing a clear error | Medium | Medium | interview Q1; PRD NFR-Error-handling; hot-spot dir `config/` (7 commits/30d) |
| 5 | Provider error swallowed — 500/timeout from upstream not forwarded as descriptive message to Claude Code | Medium | Medium | interview Q4; PRD NFR-Error-handling |
| 6 | API key or sensitive config leaked in logs, error bodies, or TUI output | Medium | Low | PRD §Access Control (credentials in env vars); abuse lens — proxy handles provider API keys |

### Risk Response Guidance

| Risk | What would prove protection | Must challenge | Context `/10x-research` must ground | Likely cheapest layer | Anti-pattern to avoid |
|------|----------------------------|----------------|--------------------------------------|-----------------------|-----------------------|
| #1 | Anthropic endpoint always returns Anthropic-format response (headers, body shape, error format) regardless of upstream provider | "Status 200 means format is correct" — must verify headers + body schema | Anthropic response spec, per-provider translation paths, error body format | integration | Assertion copied from production logic; only checking happy-path response shape |
| #2 | Streaming response delivers complete, correctly ordered content to Claude Code — partial chunks reassemble, mid-stream errors surface, SSE events parse | "Streaming works because the happy-path test passes" — must test partial/malformed/error SSE | SSE event format per provider, chunk boundary handling, error-in-stream path | integration | Happy-path-only streaming test; no partial-chunk or error-in-stream scenarios |
| #3 | Configured model mapping routes to the correct provider endpoint — wrong/missing mapping produces clear error, not silent fallback | "Default mapping covers all cases" — must verify explicit + missing + ambiguous mappings | Config schema, routing decision logic, default/fallback behavior | integration | Only testing the happy-path mapping; not testing missing/ambiguous config |
| #4 | Invalid config (bad YAML, missing required fields, invalid provider) produces descriptive error without crashing the gateway | "Config validation exists" — must verify error message quality and no-panic guarantee | Config schema, validation entry points, error message format | unit | Testing only valid configs; asserting "no panic" without checking error message content |
| #5 | Provider 500/timeout/429 surfaces as descriptive error to Claude Code — not swallowed, not generic 500 from freedius | "Error forwarding is implemented" — must verify the actual error body reaching Claude Code | Error propagation path, HTTP status mapping, error body translation | integration | Mocking provider to return error but only asserting freedius status code, not the error body |
| #6 | API keys and sensitive config never appear in logs, error responses, or TUI output | "We don't log keys" — must also verify upstream error body snippets don't contain API key patterns; `sanitizePrintable` strips non-printable chars but does NOT redact sensitive patterns | Logging paths, error body construction, TUI data flow, upstream error snippet redaction | integration | Only checking happy-path (no errors = no leakage); not testing error paths where upstream error bodies might contain key prefixes |

## 3. Phased Rollout

Each row is a discrete rollout phase that will open its own change folder
via `/10x-new`. Status moves left-to-right through the values below; the
orchestrator updates Status as artifacts appear on disk.

| # | Phase name | Goal (one line) | Risks covered | Test types | Status | Change folder |
|---|-----------|-----------------|---------------|------------|--------|---------------|
| 1 | Proxy integration — translation, routing, errors | Prove the proxy core correctly translates formats, routes by config, and propagates errors | #1, #3, #4, #5, #6 | integration + unit | planned | testing-proxy-integration |
| 2 | Streaming edge cases | Prove streaming delivers complete, correctly ordered content including partial chunks and mid-stream errors | #2 | integration | not started | — |
| 3 | Quality gates in CI | Lock the floor — every PR must pass proxy integration + streaming tests | cross-cutting | CI gates | not started | — |

## 4. Stack

The classic test base for this project. AI-native tools (if any) carry a
`checked:` date so future readers can see which lines need re-verification.
Recommendations in this section must be grounded in local manifests/configs
plus the MCP/tools actually exposed in the current session. If a useful docs
or search MCP such as Context7 or Exa.ai is not available, say that instead
of assuming access.

| Layer | Tool | Version | Notes |
|-------|------|---------|-------|
| unit + integration | Go `testing` + `httptest` | stdlib | Table-driven tests; `httptest.NewServer` for mock providers |
| test runner | `mage test` | — | Race detection enabled; runs `go test ./...` |
| lint | `gofumpt` + `staticcheck` + `golangci-lint` | — | Enforced in CI via `mage lint` |
| vulnerability scan | `govulncheck` | — | `mage govulncheck` |
| (optional) AI-native | none | — | Not needed — deterministic integration tests cover all identified risks |

If a row reads "none yet — see Phase <N>", that gap is addressed by the
named rollout phase.

**Stack grounding tools (current session):**
- Docs: Context7 — available for Go stdlib, Bubble Tea, tiktoken-go; checked: 2026-07-02
- Search: none — not available in current session
- Runtime/browser: none — not used (CLI proxy, no browser surface)
- Provider/platform: none — not used

## 5. Quality Gates

The full set of gates that must pass before a change reaches production.
"Required for §3 Phase <N>" means the gate is enforced once that rollout
phase lands; before that, the gate is `planned`.

| Gate | Where | Required? | Catches |
|------|-------|-----------|---------|
| lint + typecheck | local + CI | required | syntactic / type drift |
| unit + integration | local + CI | required after §3 Phase 1 | logic regressions in proxy core |
| streaming edge-case suite | CI on PR | required after §3 Phase 2 | streaming regressions (partial chunks, mid-stream errors, SSE quirks) |
| race detection | CI | required (already enabled via `mage test`) | concurrent-session state leak |

## 6. Cookbook Patterns

How to add new tests in this project. Each sub-section is filled in once
the relevant rollout phase ships; before that, the sub-section reads
"TBD — see §3 Phase <N>."

### 6.1 Adding a unit test

- **Location**: next to the file under test as `<file>_test.go`.
- **Naming**: `Test<FunctionOrScenario>` — table-driven with `[]struct{ name string; ... }`.
- **Reference test**: `config/config_test.go`.
- **Run locally**: `mage test`.

### 6.2 Adding an integration test

- **Location**: next to the file under test as `<file>_test.go` (same package).
- **Mocking policy**: `httptest.NewServer` for mock provider endpoints. Never mock internal modules.
- **Reference test**: `proxy/proxy_test.go`.
- **Run locally**: `mage test`.

### 6.3 Adding a streaming test

- TBD — see §3 Phase 2 (streaming edge cases — partial chunks, mid-stream errors, SSE format quirks).

### 6.4 Adding a test for a new provider adapter

- **Location**: `proxy/<adapter>_test.go` or `proxy/adapter_errors_test.go`.
- **Mock upstream**: Use `httptest.NewServer(http.HandlerFunc(...))` to simulate provider responses (success, errors, timeouts).
- **Registry pattern**: Create a `NewRegistry(map[string]Provider{...})` with the adapter under test.
- **Dispatcher chain**: Use `NewDispatcher(cfg, registry, logger, false)` + `RequestIDMiddleware` for full request flow.
- **Error body assertion**: Decode JSON response, assert `body["type"] == "error"`, then check `inner["type"]` and `inner["message"]` fields — never just status code.
- **Reference test**: `proxy/adapter_errors_test.go` — `TestDispatcher_Upstream500_AnthropicErrorEnvelope`.
- **Run locally**: `mage test`.

### 6.5 Adding a test for config validation

- **Location**: `config/config_test.go` — extend `TestLoad` table.
- **Pattern**: Table-driven with `[]struct{ name string; yaml string; wantErr string }`.
- **Error assertion**: Use `strings.Contains(err.Error(), tc.wantErr)` for substring match on validation error message.
- **Edge cases to cover**: empty mapping key (`""`), empty behavior string (`""`), missing provider reference, invalid provider name.
- **Reference test**: `config/config_test.go` — `TestLoad` (22 existing cases).
- **Run locally**: `mage test`.

### 6.6 Per-rollout-phase notes

(Optional. After each phase lands, `/10x-implement` appends a 2-3 line note
here capturing anything surprising the rollout phase taught.)

## 7. What We Deliberately Don't Test

Exclusions agreed during the rollout (Phase 2 interview, Q5). Future
contributors should respect these unless the underlying assumption changes.

- **Generated code (`providers_gen.go`, `adapters_gen.go`)** — the generator is the test; regressions are caught by testing the generator input/output. Re-evaluate if `go generate` output changes shape. (Source: Phase 2 interview Q5.)
- **Magefile build scripts** — low blast radius, rarely touched, no user-facing behavior. Re-evaluate if CI pipeline changes significantly. (Source: Phase 2 interview Q5.)
- **TUI visual layout** — the TUI is a monitoring tool, not the product core. Break it and fix it, but don't slow the pipeline for it. Re-evaluate if TUI becomes a primary user surface. (Source: Phase 2 interview Q5, implied.)

## 8. Freshness Ledger

- Strategy (§1–§5) last reviewed: 2026-07-02
- Stack versions last verified: 2026-07-02
- AI-native tool references last verified: N/A (no AI-native tools in use)

Refresh (`/10x-test-plan --refresh`) when:

- a new top-3 risk surfaces from the roadmap or archive,
- a recommended tool's `checked:` date is older than three months,
- the project's tech stack changes (new framework, new test runner),
- §7 negative-space no longer matches what the team believes.
