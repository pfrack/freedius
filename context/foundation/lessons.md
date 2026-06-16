# Lessons

Recurring rules and patterns captured from prior changes. Read this before
implementing streaming, JSON-line protocols, or anything that uses `bufio`.

## SSE: `json.NewEncoder.Encode` adds a trailing newline

`json.NewEncoder(w).Encode(v)` appends `\n` to the marshalled JSON bytes.
When emitting Anthropic-format SSE (`event: <type>\ndata: <json>\n\n`),
using `json.NewEncoder` to write the data line produces:

```
data: {...}\n\n\n
```

The extra newline corrupts event framing — Claude Code's SDK buffers until
it sees a blank line, so the extra blank line is interpreted as "empty
event, keep reading", which silently breaks streaming.

**Rule**: use `json.Marshal` (no trailing newline) for SSE data lines, then
`fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ...)`.

## SSE: `bufio.Scanner` has a 64 KB line cap

`bufio.Scanner` defaults to `bufio.MaxScanTokenSize = 64 * 1024` per line.
Tool-use `arguments` payloads (Anthropic's `input_json_delta.partial_json`,
OpenAI's `tool_calls[].function.arguments`) can exceed this in real traffic.
Exceeding the cap fails silently with no error — the scanner just returns
the truncated prefix, and the protocol is broken with no signal.

**Rule**: use `bufio.Reader.ReadBytes('\n')` (no fixed line cap; grows as
needed) for reading SSE streams. Allocate with `bufio.NewReaderSize(r,
64*1024)` for the initial buffer but the reader itself has no cap.

## `httputil.ReverseProxy` strips hop-by-hop headers *after* `Director`

`httputil.ReverseProxy.Director` (the legacy API) sets up the request
before hop-by-hop header cleanup runs — so any header the director sets
can be silently stripped. Use `Rewrite` (Go 1.20+) instead, which
operates on a `ProxyRequest` whose `Out` headers are not stripped.

**Rule**: always use `Rewrite` (not `Director`) for new `ReverseProxy`
configurations in Go 1.20+.

## `httputil.ReverseProxy` body-replacement inside `Rewrite`

When replacing the request body inside `Rewrite` (e.g., for translation),
all three of these must be set together:

- `pr.Out.Body = io.NopCloser(bytes.NewReader(newBody))`
- `pr.Out.ContentLength = int64(len(newBody))`
- `pr.Out.Header.Set("Content-Length", ...)` (Go uses this for HTTP/1.1)

Missing any one of them leads to silent corruption (Content-Length
mismatch, chunked encoding confusion, or HTTP/2 retry failure).

**Rule**: if you change the body, set all three. Also set `pr.Out.GetBody`
if HTTP/2 retries are a concern.

## `http.MaxBytesReader` only caps the *request* body

`http.MaxBytesReader` enforces a limit on the inbound body read. It does
**not** apply to the upstream response. For response size limits (DoS
protection against a runaway upstream), wrap the upstream response body
yourself in a size-capped reader.

**Rule**: pair `MaxBytesReader` for inbound with an explicit response-size
guard if DoS protection matters.
