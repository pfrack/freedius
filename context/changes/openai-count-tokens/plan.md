# Local Token Counting for OpenAI-Protocol Upstreams Implementation Plan

## Overview

Replace the 501 rejection of `POST /v1/messages/count_tokens` for OpenAI-protocol providers (NIM, OpenCode Go, custom OpenAI-compat) with a locally-computed `input_tokens` estimate that matches Anthropic's response shape byte-for-byte. The estimate uses a BPE tokenizer (`tiktoken-go` with `cl100k_base` + `o200k_base` encodings embedded via `tiktoken-go-loader`) so the count is within ~5% of the upstream tokenizers used by most OpenAI-protocol upstreams. The dispatcher short-circuits the adapter path for these requests — no upstream round-trip is needed.

## Current State Analysis

The previous change (now archived as `context/archive/count-tokens-passthrough/`) added path-aware routing: the dispatcher detects `/v1/messages/count_tokens` and, when the resolved model routes to a non-Anthropic-protocol provider, returns 501 with a freedius-format error envelope. All supporting infrastructure is already in place — the body is fully buffered at `proxy/proxy.go:97` (max 10 MiB), the capability rule at `proxy/capabilities.go:33-54` correctly classifies each provider, and the wire-format structs at `proxy/translate/types.go:52-75` model every tokenizable region of the request body. What is missing is a counter implementation and the dispatcher branch that calls it.

## Desired End State

After this plan is complete, `POST /v1/messages/count_tokens` succeeds for every configured provider — not just Anthropic-protocol ones. The 200 response body matches Anthropic's native shape:

```json
{"input_tokens": 1247, "context_management": {"original_input_tokens": 1247}}
```

The estimate is computed locally in freedius using a BPE tokenizer; no upstream round-trip. Claude Code's pre-flight count probe works identically whether the request routes to Anthropic, NIM, OpenCode Go, or a custom OpenAI-compatible endpoint. The existing Anthropic-protocol passthrough path is unchanged.

Verification: `make ci` passes. The 2 inverted test cases in `TestServeHTTPCountTokens` (`nim` and `mix+openai`) return 200 with non-zero `input_tokens` and the adapter is not invoked. A round-trip accuracy test (gated on `ANTHROPIC_API_KEY`) confirms the estimate is within 10% of the real Anthropic count for a fixed corpus.

### Key Discoveries:

- The body is fully buffered (`proxy/proxy.go:94-97`) and re-readable — the counter does not need to touch `r.Body` again
- The `model` field is already extracted at `proxy/proxy.go:125-128`; the full body is still valid for a second unmarshal
- `tiktoken-go` v0.1.8 (MIT) with `tiktoken-go-loader` v0.0.2 (MIT) embeds the BPE dictionary via `go:embed` — no network or disk access at runtime (NFR-Privacy)
- The existing `anthropicMessage` / `anthropicMsgItem` / `anthropicTool` structs in `proxy/translate/types.go:52-75` are unexported; the counter must live in the `translate` package to use them without API widening
- `AccessLogMiddleware` already logs the `path` field (`proxy/proxy.go:415-442`) so the local-counter branch is distinguishable from regular messages in logs
- The dispatcher already sets `X-Freedius-Matched-*` headers at `proxy/proxy.go:198-199`; the new branch must set them too (and earlier, before the call) so the access log records them

## What We're NOT Doing

- Touching the Anthropic-protocol passthrough path (already works via `httputil.ReverseProxy`)
- Changing the `Provider` interface or `Registry` type
- Adding per-encoding config fields or per-model encoding selection rules beyond a simple heuristic
- Implementing image/document token-cost math from Anthropic's published formula (fixed constants only)
- Making the counter return an error to the client on malformed bodies (best-effort re-parse, then 0)
- Bundling all four encodings (`p50k_base`, `r50k_base`) — only `cl100k_base` and `o200k_base` are used by current OpenAI-protocol upstreams
- Changing the `MaxBodyBytes` 10 MiB cap
- Logging the local count at info level
- Adding a CLI command to verify accuracy manually (the env-gated test is sufficient)

## Implementation Approach

Four phases, each independently shippable and rollable back:

1. **Counter implementation** — add the `tiktoken-go` + `tiktoken-go-loader` dependencies, write `proxy/translate/count.go` with the encoder singleton, the `CountInputTokens` function, the content-block walk, the best-effort re-parse fallback, the image/document fixed-cost constants, and the encoding-selection heuristic. Unit-test each block type and the re-parse fallback.

2. **Dispatcher integration** — write `proxy/count_tokens_local.go` with the `serveLocalCountTokens` dispatcher method and the `countTokensResponse` struct. Modify `proxy/proxy.go:188-197` to replace the 501 branch with a local-counter branch that sets `X-Freedius-Matched-*` headers and calls the new method. Invert the 2 sub-cases of `TestServeHTTPCountTokens` (nim and mix+openai) to assert 200 + non-zero `input_tokens` + adapter-not-invoked.

3. **Accuracy verification** — add a round-trip test in `proxy/translate/count_test.go` (or a new `proxy/translate/count_accuracy_test.go`) gated on `ANTHROPIC_API_KEY`. The test sends a fixed corpus of representative Claude Code request bodies to the real Anthropic `/v1/messages/count_tokens` endpoint and compares the local estimate to the upstream count. Tolerance: within 10% per body, 95% of corpus within tolerance.

4. **CI gate** — verify `go test ./...`, `go vet ./...`, `go build -o freedius .`, `gofumpt -l proxy/ proxy/translate/`, and `govulncheck ./...` all pass. The accuracy test is skipped in CI (no `ANTHROPIC_API_KEY`); unit tests run normally.

## Phase 1: Counter implementation

### Overview

Add the tokenizer dependency and the counter function. The counter lives in `proxy/translate/` so it can use the unexported `anthropicMessage` / `anthropicMsgItem` / `anthropicTool` wire-format structs without widening the package's public API. The function is the only public surface added to `translate`; everything else is package-internal.

### Changes Required:

#### 1. Add dependencies

**File**: `go.mod`, `go.sum`

**Intent**: Bring in `tiktoken-go` (the BPE tokenizer) and `tiktoken-go-loader` (the embedded BPE blob loader). Both are MIT-licensed, actively maintained (latest releases Sep 2025), zero transitive dependencies beyond what they already declare.

**Contract**: `go.mod` gains two `require` lines. `go.sum` is regenerated. No other modules change. Run `go mod tidy` to confirm no unused or missing entries.

#### 2. Counter function

**File**: `proxy/translate/count.go` (new)

**Intent**: Provide a single exported function `CountInputTokens(body []byte) (int, error)` that returns the estimated input token count for an Anthropic-format `/v1/messages` request body. All encoding infrastructure (singleton encoder, loader wiring) is package-internal.

**Contract**:

- Exported function: `func CountInputTokens(body []byte) (int, error)`. Returns 0 and a non-nil error if the body is unparseable; otherwise returns the integer count and a nil error.
- Package-internal helpers: `countSystem`, `countMessages`, `countContent`, `countTools`, `countBlock` (the per-type switch for content blocks).
- Package-internal encoder accessor: `getEncoder(encodingName string) (*tiktoken.Tiktoken, error)` — uses `sync.Once` per encoding to lazily load and cache the BPE decoder. The first call to `getEncoder("cl100k_base")` or `getEncoder("o200k_base")` initializes the BPE; subsequent calls return the cached value.
- Package-internal `init()` function calls `tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())` so the BPE blob is read from the embedded binary, not downloaded. This must run before any `tiktoken.GetEncoding` call.
- Package-internal `pickEncoding(modelName string) string` function returns `"o200k_base"` if `modelName` matches the gpt-4o / gpt-4.1 / gpt-4.5 / o1 / o3 / o4 / o200k target model family (substring match against a small hard-coded list), else `"cl100k_base"`.
- Package-internal constants: `imageTokenCost = 160`, `documentTokenCost = 500` (fixed costs per image or document block, in line with Anthropic's lower bound for small images / 5-page PDFs).
- Package-internal constant: `cl100kBase = "cl100k_base"`, `o200kBase = "o200k_base"`.
- Best-effort re-parse: if the strict `json.Unmarshal(body, &anthropicMessage)` fails, retry with `json.NewDecoder(bytes.NewReader(body))` using `dec.UseNumber()` to a `map[string]any`, then walk the map extracting only the fields the counter can validate. Fields that fail validation (wrong type, missing) contribute 0 to the count; the function still returns nil error.
- Content block walk (counter increments):
  - `text` block → `len(encoder.Encode(text))`
  - `tool_use` block → `len(encoder.Encode(name))` + `len(encoder.Encode(json.Marshal(input)))`
  - `tool_result` block → recurse on `content` (string or block array)
  - `thinking` block → `len(encoder.Encode(thinking))`; skip `signature`
  - `image` block → `+imageTokenCost`
  - `document` block → `+documentTokenCost`
  - `redacted_thinking`, `server_tool_use`, server-tool result blocks (web_search / web_fetch / code_execution / bash_code_execution / text_editor_code_execution / tool_search) → contribute 0
- Tool definition walk: `len(encoder.Encode(name))` + `len(encoder.Encode(description))` + `len(encoder.Encode(json.Marshal(input_schema)))`
- System walk: if string → encode directly; if array of text blocks → encode each `.text`
- Messages walk: recurse on each `content` (string or block array)

#### 3. Unit tests

**File**: `proxy/translate/count_test.go` (new)

**Intent**: Cover each block type, each branch of the encoding selection, the best-effort re-parse fallback, and the fixed-cost constants. Deterministic, no network.

**Contract**: Table-driven test with cases for:

- Empty body → returns 0, error
- Body with only `system` string → non-zero
- Body with only `system` as text-block array → non-zero
- Body with single user message (string content) → non-zero
- Body with single user message (text block) → non-zero
- Body with tool_use block → includes tool name + JSON-marshalled input
- Body with tool_result block (string content) → non-zero
- Body with tool_result block (block-array content) → non-zero
- Body with thinking block → includes thinking text, not signature
- Body with image block → exactly `imageTokenCost` added
- Body with document block → exactly `documentTokenCost` added
- Body with redacted_thinking block → contributes 0
- Body with multiple messages, multiple tools → sum is monotonic
- Body with `tools[].input_schema` (JSON schema object) → schema contributes tokens
- Best-effort re-parse: body that is valid JSON but not a valid Anthropic messages object → returns 0, nil error (or non-nil error — see note below)
- `pickEncoding("gpt-4o")` → `"o200k_base"`
- `pickEncoding("gpt-4")` → `"cl100k_base"`
- `pickEncoding("gpt-3.5-turbo")` → `"cl100k_base"`
- `pickEncoding("meta/llama-3.1-70b-instruct")` → `"cl100k_base"` (default)
- `pickEncoding("o1-preview")` → `"o200k_base"`

Note on best-effort re-parse: the test asserts the function returns nil error and a count of 0 for bodies that are valid JSON but fail strict validation. The exact boundary (where strict fails but lenient succeeds) is implementation-defined; the test should pick a body that exercises the lenient path explicitly, e.g. one with an extra unknown field plus a missing required field — strict fails, lenient recovers with 0.

### Success Criteria:

#### Automated Verification:

- `go build ./...` — entire module compiles
- `go test ./proxy/translate/...` — all count_test.go cases pass
- `go vet ./...` — clean
- `gofumpt -l proxy/translate/` — no formatting issues
- `go mod tidy` — no missing or unused entries
- `go test -cover ./proxy/translate/...` — coverage of `count.go` ≥ 80%

#### Manual Verification:

- None required for this phase (pure library code, fully covered by unit tests)

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 2.

---

## Phase 2: Dispatcher integration

### Overview

Wire the counter into the dispatcher's count_tokens branch. The 501 rejection path at `proxy/proxy.go:188-197` is replaced with a call to a new `serveLocalCountTokens` method on `Dispatcher`. The response struct mirrors Anthropic's native count_tokens response shape.

### Changes Required:

#### 1. New dispatcher method

**File**: `proxy/count_tokens_local.go` (new)

**Intent**: Provide the dispatcher-side glue: write a 200 response with the Anthropic-format `count_tokens` JSON envelope. Logs the count at debug level so the access log + a debug-level log line together let operators trace count activity.

**Contract**:

- Exported (package-level) types: `countTokensResponse` with JSON-tagged fields `input_tokens` (int) and `context_management` (pointer to `countTokensContextManagement`, always set so the field appears in the JSON). `countTokensContextManagement` has JSON-tagged field `original_input_tokens` (int).
- Method on `*Dispatcher`: `serveLocalCountTokens(w http.ResponseWriter, r *http.Request, m config.Model, body []byte)`. Calls `translate.CountInputTokens(body)`. On error, logs at debug and uses 0 as the count (the best-effort re-parse in Phase 1 should make this rare). Writes `Content-Type: application/json`, `WriteHeader(200)`, then `json.NewEncoder(w).Encode(resp)`. The encoder is fine here because the response is a single JSON object, not an SSE event.
- Logs a debug line per request with: request_id, provider (via `originalOr(m)`), target_model, input_tokens.

#### 2. Replace the 501 branch in ServeHTTP

**File**: `proxy/proxy.go:188-197` (modified)

**Intent**: When the path is `/v1/messages/count_tokens` and the capability rule rejects, serve a local 200 response instead of a 501. The matched provider / model headers must be set so the access log and any future telemetry identify which provider the count was for.

**Contract**:

- The block at lines 188-197 (currently `if isCountTokensPath(r.URL.Path) && !supportsCountTokens(m) { d.writeErrorJSON(...); return }`) is replaced with a two-branch block:
  - First, if `isCountTokensPath(r.URL.Path)`:
    - Set `w.Header().Set("X-Freedius-Matched-Provider", originalOr(m))` and `w.Header().Set("X-Freedius-Matched-Model", m.Model)` BEFORE the capability check. (This mirrors the existing lines 198-199 for the non-count-tokens path.)
    - If `!supportsCountTokens(m)`: call `d.serveLocalCountTokens(w, r, m, body)` and return.
    - Else: fall through to the existing adapter dispatch (no change).
- The `X-Freedius-Matched-*` headers for the count-tokens branch are set here, NOT at lines 198-199 (which become unreachable for count-tokens requests because the new block returns earlier on the local-counter path and falls through to the adapter on the passthrough path).
- For the passthrough path (count-tokens + supportsCountTokens), the headers must be set before `adapter.Handle` is called. The dispatcher currently sets them at lines 198-199, which still run because the new block only returns on the `!supportsCountTokens` branch. So the existing lines 198-199 continue to set the headers for the passthrough case. To keep behavior identical to the pre-change for both paths, the new block sets the headers at the top of the `if isCountTokensPath(...)` block (covering both passthrough and local-counter paths), and lines 198-199 are removed (they would double-set).

#### 3. Invert the 2 sub-cases of TestServeHTTPCountTokens

**File**: `proxy/proxy_test.go:511-557` (modified)

**Intent**: The test currently asserts 501 for `nim` and `mix+openai` count_tokens requests. After Phase 2, these should assert 200 with a non-zero `input_tokens` field and prove the adapter is NOT invoked (the local counter short-circuits the adapter dispatch).

**Contract**:

- The two sub-cases `"nim provider + count_tokens path -> 501 not_supported"` (lines 511-527) and `"mix + Protocol openai + count_tokens -> 501 not_supported"` (lines 541-558) are renamed to `"... -> local counter (200)"` and their assertions are inverted:
  - `wantStatus`: `http.StatusOK` (was `http.StatusNotImplemented`)
  - `wantBodyHas`: `[]string{`"input_tokens":`, `"context_management":`, `"original_input_tokens":`}` — at least one of these substrings is present; the exact number depends on the body, so the test must send a non-empty body and assert the substrings without asserting the exact count.
  - `wantHeader`: `map[string]string{"X-Freedius-Matched-Provider": "nim"/"mix", "X-Freedius-Matched-Model": "x"}` (was `wantHeaderMiss`)
- Replace the `mockOK` registered for the provider with a `recordingProvider` (existing helper at `proxy/proxy_test.go:453-469`) whose `called` field is asserted `false` after `d.ServeHTTP` returns — proves the adapter was not invoked.
- The body sent in the test must be a non-empty Anthropic-format messages object so the counter returns a non-zero count. Use a body like `{"model":"claude-opus-4","messages":[{"role":"user","content":"hello world"}]}` — short, exercises the messages path, gives a deterministic small count.
- The test runner asserts that the recorded input_tokens value is > 0 (proves the counter is computing, not just returning 0).

#### 4. No other test changes

**Files**: `proxy/capabilities_test.go`, `proxy/capabilities.go`, `proxy/anthropic_compat.go`, `proxy/openai_compat.go`, `proxy/mix.go`, `proxy/errors.go`, `proxy/translate/anthropic_openai_test.go`, etc.

**Intent**: The capability rule itself is unchanged. The 4 success cases in `TestServeHTTPCountTokens` (passthrough for `anthropic`, `mix+anthropic`, `mix` with URL sniff) continue to work as before because the passthrough path is unchanged.

**Contract**: No edits to these files in this phase.

### Success Criteria:

#### Automated Verification:

- `go test ./...` — all existing tests pass plus the 2 inverted sub-cases
- `go test ./proxy/...` — `TestServeHTTPCountTokens` passes with 6 sub-cases (4 unchanged, 2 inverted)
- `go vet ./...` — clean
- `gofumpt -l proxy/` — no formatting issues
- `go build -o freedius .` — static binary builds

#### Manual Verification:

- Start freedius with a NIM provider configured. Send a count_tokens request via `curl`. Verify the response is 200 with `input_tokens` populated (no 501).
- Send the same request to an `anthropic` provider. Verify the response is 200 with the upstream's exact count (passthrough).
- Send the same request to a `mix+openai` provider. Verify the response is 200 with the local estimate.

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 3.

---

## Phase 3: Accuracy verification

### Overview

Add a round-trip test that exercises the counter against the real Anthropic `/v1/messages/count_tokens` endpoint for a fixed corpus of representative Claude Code request bodies. The test is gated on `ANTHROPIC_API_KEY` and skipped otherwise — so it runs in dev/CI with a key but does not block CI builds without one.

### Changes Required:

#### 1. Accuracy test

**File**: `proxy/translate/count_accuracy_test.go` (new)

**Intent**: Prove that the local counter's estimates are within tolerance of the real Anthropic count for a representative corpus. Provides a regression signal if the encoder is changed, the block-walk is modified, or the fixed-cost constants are adjusted.

**Contract**:

- Test function: `TestCountInputTokens_RoundTrip(t *testing.T)`
- Test gate: `if os.Getenv("ANTHROPIC_API_KEY") == "" { t.Skip("ANTHROPIC_API_KEY not set; skipping round-trip accuracy test") }`
- Test setup: starts an `httptest.NewServer` that proxies to `https://api.anthropic.com/v1/messages/count_tokens`, OR uses the Anthropic Go SDK directly. The simpler approach is to call the upstream HTTP endpoint directly via `http.Post` (no new dep needed).
- Test corpus: a `[]struct{name string; body []byte}` array with 8-12 entries covering:
  - Simple user message (short text)
  - User message with system prompt (long text)
  - Multi-turn conversation (3+ messages)
  - Tool-use request (3 tools defined, no tool_use in messages)
  - Tool-result request (1 tool result block, content as string)
  - Image content (one image block, base64 1x1 PNG)
  - Document content (one document block, short PDF)
  - Mixed content (text + tool_use + tool_result in one conversation)
  - Empty system (system omitted)
  - Long prompt (~5 KB of code)
- For each corpus entry:
  1. Build an Anthropic-format body with `model: "claude-opus-4-0"`, the test-specific `messages`/`system`/`tools`, and send a real HTTP request to `https://api.anthropic.com/v1/messages/count_tokens` with the API key.
  2. Capture the upstream `input_tokens` from the JSON response.
  3. Call `translate.CountInputTokens(body)` and capture the local count.
  4. Compute `abs(local - upstream) / upstream` as the relative error.
  5. Assert relative error ≤ 0.10 (10% tolerance).
- Final assertion: at least 95% of the corpus is within tolerance (allows 1-2 outliers in case Anthropic's count has small non-determinism).
- Test logs each corpus entry's upstream count, local count, and relative error for debugging.

#### 2. No production-code changes

**Files**: none in this phase

**Intent**: Phase 3 adds a test only. No production code is modified.

**Contract**: No production-code edits in this phase.

### Success Criteria:

#### Automated Verification:

- `ANTHROPIC_API_KEY=<key> go test ./proxy/translate/... -run TestCountInputTokens_RoundTrip` — passes with ≥95% of corpus within 10% tolerance
- `go test ./proxy/translate/...` (without `ANTHROPIC_API_KEY`) — round-trip test is skipped, unit tests pass
- `go test ./...` — all existing tests plus new unit tests pass

#### Manual Verification:

- Run the round-trip test with a personal `ANTHROPIC_API_KEY` and review the per-corpus relative errors in the test log output. Confirm the worst case is within 15% (the assertion is 10% with 95% pass rate, but reviewing the actual distribution is good due diligence).
- If a corpus entry exceeds 15% relative error, document it as a known limitation in the test comment and decide whether to adjust the counter's logic (e.g., add a constant for a specific block type) or adjust the tolerance.

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 4.

---

## Phase 4: CI gate

### Overview

Run the project's standard CI commands and confirm the new code passes all checks. No new CI infrastructure is required — the existing `make ci` target (or the equivalent `go test ./...` + `go vet ./...` + `go build -o freedius .` + `gofumpt -l ...` + `govulncheck ./...`) covers everything.

### Changes Required:

#### 1. CI verification

**Files**: none modified

**Intent**: Run the existing CI commands to confirm Phase 1-3 changes pass all checks.

**Contract**: The following commands all exit 0:

- `go test ./...`
- `go vet ./...`
- `go build -o freedius .`
- `gofumpt -l proxy/ proxy/translate/`
- `govulncheck ./...`
- `go mod tidy` (idempotent — no diff after running)

The `govulncheck` step is the new addition beyond the prior change set — it must confirm the two new dependencies (`tiktoken-go` and `tiktoken-go-loader`) have no known vulnerabilities.

#### 2. Final commit and push

**Files**: staged and committed per the repository's commit conventions (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`)

**Intent**: Land the change with a clean commit history that follows the conventions in `AGENTS.md` and the existing `git log`.

**Contract**: One or more commits on the `token_count` branch (the branch already created for this work). The progress section of this plan is updated with commit SHAs as each step lands. No `--force` pushes, no skipping hooks.

### Success Criteria:

#### Automated Verification:

- `make ci` (or the equivalent set of commands above) exits 0
- `go test -cover ./...` — coverage of `proxy/translate/count.go` ≥ 80% (already verified in Phase 1; spot-check here)
- `go test -cover ./...` — coverage of `proxy/count_tokens_local.go` ≥ 70% (new file, mostly thin glue)
- `git status` is clean after `go mod tidy` and `gofumpt -w` (any final formatting fixes land in a follow-up commit)

#### Manual Verification:

- `make ci` passes on a clean checkout (no uncommitted changes)
- The 2 sub-cases of `TestServeHTTPCountTokens` that previously asserted 501 now assert 200 + non-zero `input_tokens`
- `git log --oneline` shows a clean conventional-commit-prefixed history for the change

**Implementation Note**: After completing this phase and all automated verification passes, the change is ready for review and merge.

---

## Testing Strategy

### Unit Tests:

- `proxy/translate/count_test.go` — each block type, each encoding selection branch, the best-effort re-parse fallback, the fixed-cost constants
- `proxy/translate/count.go` (coverage ≥ 80%)

### Integration Tests:

- `proxy/proxy_test.go` `TestServeHTTPCountTokens` — 6 sub-cases (4 unchanged, 2 inverted from 501 to 200)
- `proxy/proxy_test.go` (coverage of `serveLocalCountTokens` ≥ 70%)

### Accuracy Tests (dev-only, gated on env):

- `proxy/translate/count_accuracy_test.go` `TestCountInputTokens_RoundTrip` — 8-12 corpus entries, 10% tolerance, 95% pass rate, skipped if `ANTHROPIC_API_KEY` is unset

### Manual Testing Steps:

1. Configure freedius with `provider: nim` and a known API key. Send a `POST /v1/messages/count_tokens` request with a small body via `curl`. Verify the response is 200, contains `input_tokens` and `context_management.original_input_tokens`, and that the X-Freedius-Matched-Provider header is `nim`.
2. Configure freedius with `provider: anthropic` and a valid key. Send the same `count_tokens` request. Verify the response matches Anthropic's native count exactly (passthrough).
3. Configure freedius with `provider: mix` and `protocol: openai` (or `provider: nim`). Send a `count_tokens` request with a body containing tools and tool_use blocks. Verify the local count is non-zero and within ~20% of what Anthropic would return (compare with a separate curl to `https://api.anthropic.com/v1/messages/count_tokens` with the same body).
4. With a body containing image blocks, verify the local count includes the `imageTokenCost` per image (160 each).
5. With a body containing a document block, verify the local count includes `documentTokenCost` (500).
6. With a malformed body (invalid JSON), verify the response is 200 with `input_tokens: 0` and a debug log line, not a 400 or 500.

## Performance Considerations

- **Encoder initialization**: First call to `getEncoder("cl100k_base")` or `getEncoder("o200k_base")` loads the BPE blob from the embedded binary, ~50-100ms one-time cost. Subsequent calls are O(1) via the `sync.Once` cache. Acceptable because count_tokens is not a hot path.
- **Per-request encoding**: ~5 μs per KB of tokenizable text. A 100 KB prompt encodes in ~500 μs. Within the latency budget for a count probe.
- **Memory**: The two BPE merge-rank tables together occupy ~12 MB resident after first load. Within the 50 MB NFR footprint budget.
- **Binary size**: +12 MB for both encodings. Slightly more than the +6 MB for cl100k_base only, but the user picked both for accuracy. Within budget.
- **No streaming complexity**: The body is fully buffered before the counter runs. No I/O, no partial reads, no flush.

## Migration Notes

- **Backwards compatible for clients**: The 501 response is replaced with 200. Clients that previously treated 501 as "feature not supported" now get a 200 — strictly more permissive, no behavior change for clients that ignored 501.
- **Backwards compatible for config**: No config schema changes. No new env vars. No new required fields. Existing configs work unchanged.
- **Dependency addition**: `tiktoken-go` and `tiktoken-go-loader` are added to `go.mod`. Both MIT-licensed, both actively maintained. No CLA required.
- **Disk/network**: Zero at runtime — the BPE is embedded in the binary via `go:embed` and the offline loader reads from the binary, not from disk or network. Aligned with NFR-Privacy (`context/foundation/prd.md:90`).
- **Rollback**: Reverting the four changes (Phase 1: `go.mod`/`go.sum` + `proxy/translate/count.go` + `count_test.go`; Phase 2: `proxy/count_tokens_local.go` + `proxy/proxy.go` edit + `proxy_test.go` edit; Phase 3: `count_accuracy_test.go`; Phase 4: none) restores the prior 501 behavior. No data migrations or persistent state changes.

## References

- Research: `context/changes/openai-count-tokens/research.md`
- Prior change: `context/archive/count-tokens-passthrough/` — added the 501 path this change replaces
- Anthropic count_tokens API: https://docs.anthropic.com/en/api/messages-count-tokens
- `tiktoken-go` (MIT, v0.1.8): https://github.com/pkoukk/tiktoken-go
- `tiktoken-go-loader` (MIT, v0.0.2): https://github.com/pkoukk/tiktoken-go-loader
- 501 site: `proxy/proxy.go:188-197`
- Capability rule: `proxy/capabilities.go:33-54`
- Wire-format structs: `proxy/translate/types.go:52-75`
- Test pattern: `proxy/proxy_test.go:475-642` (`TestServeHTTPCountTokens`)
- Body buffering: `proxy/proxy.go:94-97`
- Matched-headers invariant: `proxy/proxy.go:198-199`
- Adapter Return Contract: `context/foundation/lessons.md:33-43` (this change respects it: the new method returns nothing; it writes the full response and does not return an error to the dispatcher)
- NFR-Privacy: `context/foundation/prd.md:90` (no payload to disk; offline BPE loader satisfies this)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Counter implementation

#### Automated

- [x] 1.1 `go build ./...` — entire module compiles — 25f3da8
- [x] 1.2 `go test ./proxy/translate/...` — all count_test.go cases pass — 25f3da8
- [x] 1.3 `go vet ./...` — clean — 25f3da8
- [x] 1.4 `gofumpt -l proxy/translate/` — no formatting issues — 25f3da8
- [x] 1.5 `go mod tidy` — no missing or unused entries — 25f3da8
- [x] 1.6 `go test -cover ./proxy/translate/...` — coverage of count.go ≥ 80% — 25f3da8

#### Manual

- [x] 1.7 (none — pure library code, fully covered by unit tests) — 25f3da8

### Phase 2: Dispatcher integration

#### Automated

- [x] 2.1 `go test ./...` — all existing tests pass plus the 2 inverted sub-cases — e08cdfb
- [x] 2.2 `go test ./proxy/...` — TestServeHTTPCountTokens passes with 6 sub-cases — e08cdfb
- [x] 2.3 `go vet ./...` — clean — e08cdfb
- [x] 2.4 `gofumpt -l proxy/` — no formatting issues — e08cdfb
- [x] 2.5 `go build -o freedius .` — static binary builds — e08cdfb

#### Manual

- [x] 2.6 NIM count_tokens → 200 with input_tokens populated (not 501) — e08cdfb
- [x] 2.7 Anthropic count_tokens → 200 with upstream's exact count (passthrough) — e08cdfb
- [x] 2.8 mix+openai count_tokens → 200 with local estimate — e08cdfb

### Phase 3: Accuracy verification

#### Automated

- [ ] 3.1 `ANTHROPIC_API_KEY=<key> go test ./proxy/translate/... -run TestCountInputTokens_RoundTrip` — passes with ≥95% of corpus within 10% tolerance
- [x] 3.2 `go test ./proxy/translate/...` (without ANTHROPIC_API_KEY) — round-trip test skipped, unit tests pass
- [x] 3.3 `go test ./...` — all existing tests plus new unit tests pass

#### Manual

- [ ] 3.4 Review per-corpus relative errors in round-trip test log; confirm worst case ≤ 15%

### Phase 4: CI gate

#### Automated

- [ ] 4.1 `make ci` (or equivalent: `go test ./...` + `go vet ./...` + `go build -o freedius .` + `gofumpt -l ...` + `govulncheck ./...` + `go mod tidy`) exits 0
- [ ] 4.2 `go test -cover ./...` — coverage of `proxy/translate/count.go` ≥ 80%
- [ ] 4.3 `go test -cover ./...` — coverage of `proxy/count_tokens_local.go` ≥ 70%
- [ ] 4.4 `git status` clean after `go mod tidy` and `gofumpt -w`

#### Manual

- [ ] 4.5 `make ci` passes on a clean checkout
- [ ] 4.6 The 2 inverted sub-cases of TestServeHTTPCountTokens pass
- [ ] 4.7 `git log --oneline` shows a clean conventional-commit-prefixed history
