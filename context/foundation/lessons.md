# Lessons Learned

## SSE Encoding: `json.Marshal` over `json.NewEncoder`

`json.NewEncoder(w).Encode(v)` appends `\n` to the marshalled JSON. Using those bytes in `Fprintf(w, "data: %s\n\n", buf)` produces `data: {...}\n\n\n` — three newlines instead of two, corrupting SSE event framing. Always use `json.Marshal` (no trailing newline) when emitting SSE events.

**Source**: `proxy/translate/anthropic_openai.go` — the `anthropicEmitter.emit*` methods use `json.Marshal`. See S-01 research.

## SSE Reader: `bufio.Reader.ReadBytes` over `bufio.Scanner`

`bufio.Scanner` defaults to a 64 KB `MaxScanTokenSize`. Tool-use `arguments` payloads can exceed this, causing silent truncation or scan errors. Use `bufio.Reader.ReadBytes('\n')` instead.

**Source**: `proxy/translate/anthropic_openai.go` — `readSSEEvent` uses `ReadBytes('\n')`. See S-01 research.

## `custom` → `mix` Rewrite in `applyDefaults`

The `custom` provider alias is rewritten to `mix` in `applyEntryDefaults()` (`config/defaults.go`), which runs before validation. This means error messages about `custom` entries reference `mix` as the provider name. Tests must use the post-rewrite name in expected error substrings.

**Source**: `config/config.go` — per-entry validation runs after `applyDefaults`.

## Embrace Extra Tests

**Context**: The implementation included additional test files (`provider_test.go`, `errors_test.go`, `main_test.go`) beyond those explicitly enumerated in the change plan.

**Problem**: Strict adherence to the plan might discard valuable tests that improve codebase quality, regression safety, and future refactor confidence.

**Rule**: When an implementation naturally produces extra tests that provide meaningful coverage for core components, retain and integrate them. Treat the plan as a minimum baseline, not a strict ceiling. Document such additions in the change description or a follow-up.

**Applies to**: All change implementations; valuable tests should be kept even if they expand the planned test suite.

**Source**: `proxy/provider_test.go`, `proxy/errors_test.go`, `main_test.go` (not in original plan but provide useful coverage).

## Adapter Return Contract

**Context**: OpenAICompatibleAdapter.Handle may encounter errors after it has already written the HTTP response (status and headers).

**Problem**: Returning a non-nil error after `WriteHeader` causes the dispatcher to attempt writing a 502 response on top of the in-flight response, leading to "superfluous response.WriteHeader" panics and connection resets. This violates the Provider.Handle contract defined in the plan.

**Rule**: Once an adapter has written any part of the response (including forwarding an upstream error with its body), it must return `nil`. Any subsequent errors should be logged (or ignored) but not returned to the dispatcher.

**Applies to**: All implementations of the `Provider` interface, especially `OpenAICompatibleAdapter` and future adapters.

**Source**: `proxy/openai_compat.go` — Handle method return value handling.

## Custom Provider: `x-api-key` + `anthropic-version` required

**Context**: `proxy/anthropic_compat.go` — Anthropic-compatible adapter Handle method.

**Problem**: The S-01 plan specified `Authorization: Bearer <key>` as the only auth header for custom providers, and explicitly deferred the `anthropic-version` header. The Anthropic API does not accept Bearer — it requires `x-api-key` and `anthropic-version: 2023-06-01`. The plan was wrong; code is right.

**Rule**: Always set `x-api-key` and `anthropic-version: 2023-06-01` on outgoing request headers for Anthropic-compatible upstreams. Strip any stray `Authorization` header via `pr.Out.Header.Del("Authorization")` in the Rewrite function.

**Applies to**: Future Anthropic-shaped adapters

**Source**: `proxy/anthropic_compat.go` — Handle method lines 38-46.
