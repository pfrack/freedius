package proxy

// isCountTokensPath reports whether p is the Anthropic count_tokens endpoint.
// Exact match (not suffix) avoids false positives against hypothetical future
// paths like /v2/.../count_tokens. Query strings live in r.URL.RawQuery, not
// r.URL.Path, so they do not affect the match.
func isCountTokensPath(p string) bool {
	return p == "/v1/messages/count_tokens"
}
