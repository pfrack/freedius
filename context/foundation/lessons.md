# Lessons Learned

## SSE Encoding: `json.Marshal` over `json.NewEncoder`

`json.NewEncoder(w).Encode(v)` appends `\n` to the marshalled JSON. Using those bytes in `Fprintf(w, "data: %s\n\n", buf)` produces `data: {...}\n\n\n` — three newlines instead of two, corrupting SSE event framing. Always use `json.Marshal` (no trailing newline) when emitting SSE events.

**Source**: `proxy/translate/anthropic_openai.go` — the `anthropicEmitter.emit*` methods use `json.Marshal`. See S-01 research.

## SSE Reader: `bufio.Reader.ReadBytes` over `bufio.Scanner`

`bufio.Scanner` defaults to a 64 KB `MaxScanTokenSize`. Tool-use `arguments` payloads can exceed this, causing silent truncation or scan errors. Use `bufio.Reader.ReadBytes('\n')` instead.

**Source**: `proxy/translate/anthropic_openai.go` — `readSSEEvent` uses `ReadBytes('\n')`. See S-01 research.

## `custom` → `anthropic` Rewrite in `applyDefaults`

The `custom` provider alias is rewritten to `anthropic` in `applyDefaults()` (`config/defaults.go`), which runs before validation. This means error messages about `custom` entries reference `anthropic` as the provider name. Tests must use the post-rewrite name in expected error substrings.

**Source**: `config/config.go` — per-entry validation runs after `applyDefaults`.
