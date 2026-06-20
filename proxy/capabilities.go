package proxy

import (
	"github.com/pfrack/freedius/config"
)

// isCountTokensPath reports whether p is the Anthropic count_tokens endpoint.
// Exact match (not suffix) avoids false positives against hypothetical future
// paths like /v2/.../count_tokens. Query strings live in r.URL.RawQuery, not
// r.URL.Path, so they do not affect the match.
func isCountTokensPath(p string) bool {
	return p == "/v1/messages/count_tokens"
}

// supportsCountTokens reports whether the provider routes to an upstream that
// natively serves /v1/messages/count_tokens. The flag is populated at
// config-load time by applyDefaults from the generated providerDefaults
// metadata, so this is a one-line read of Provider.SupportsCountTokens.
func supportsCountTokens(provider config.Provider) bool {
	return provider.SupportsCountTokens
}
