package proxy

import (
	"net/url"
	"strings"

	"github.com/pfrack/freedius/config"
)

// isCountTokensPath reports whether p is the Anthropic count_tokens endpoint.
// Exact match (not suffix) avoids false positives against hypothetical future
// paths like /v2/.../count_tokens. Query strings live in r.URL.RawQuery, not
// r.URL.Path, so they do not affect the match.
func isCountTokensPath(p string) bool {
	return p == "/v1/messages/count_tokens"
}

// supportsCountTokens reports whether m routes to an Anthropic-protocol
// upstream that natively serves /v1/messages/count_tokens.
//
// Rules (mirrored from MixAdapter.Handle):
//   - provider == "anthropic"                              -> true
//   - provider == "mix" with Protocol == "anthropic"       -> true
//   - provider == "mix" with Protocol unset and BaseURL
//     parseable with a path ending in "/v1/messages"       -> true
//   - everything else (nim, openai, mix+openai, mix with
//     unparseable URL, mix with a non-/v1/messages URL)    -> false
//
// IMPORTANT: This duplicates the routing rule from MixAdapter.Handle
// (proxy/mix.go). If MixAdapter.Handle gains a third protocol or a new URL
// sniff, update both sites. TestSupportsCountTokens covers each branch so
// drift surfaces immediately.
func supportsCountTokens(m config.Model) bool {
	if m.Provider == "anthropic" {
		return true
	}
	if m.Provider != "mix" {
		return false
	}
	if m.Protocol == "anthropic" {
		return true
	}
	if m.Protocol == "openai" {
		return false
	}
	if m.BaseURL == "" {
		return false
	}
	parsedURL, err := url.Parse(m.BaseURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(parsedURL.Path, "/v1/messages")
}
