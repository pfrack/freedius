package proxy

import (
	"net/url"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestIsCountTokensPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "exact match",
			path: "/v1/messages/count_tokens",
			want: true,
		},
		{
			name: "trailing slash — not the same path",
			path: "/v1/messages/count_tokens/",
			want: false,
		},
		{
			name: "regular messages endpoint",
			path: "/v1/messages",
			want: false,
		},
		{
			name: "v2 prefix variant — suffix match would falsely accept this",
			path: "/v2/messages/count_tokens",
			want: false,
		},
		{
			name: "empty path",
			path: "",
			want: false,
		},
		{
			name: "root",
			path: "/",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCountTokensPath(tt.path); got != tt.want {
				t.Errorf("isCountTokensPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestIsCountTokensPathIgnoresQuery verifies the realistic scenario: callers
// pass r.URL.Path (which never contains the query string in net/http — the
// query is in r.URL.RawQuery). Parsing a URL and feeding its Path component
// into the helper must work for any query string, so callers don't have to
// strip "?..." before checking.
func TestIsCountTokensPathIgnoresQuery(t *testing.T) {
	raw := "/v1/messages/count_tokens?anthropic-beta=prompt-caching&foo=bar"
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if !isCountTokensPath(parsed.Path) {
		t.Errorf("isCountTokensPath(%q) = false, want true (query is in RawQuery)", parsed.Path)
	}
}

func TestSupportsCountTokens(t *testing.T) {
	// supportsCountTokens is now a one-line read of Provider.SupportsCountTokens
	// (set at config-load time by applyDefaults from generated providerDefaults).
	// Behavior at request time is governed by the provider's runtime flag.
	tests := []struct {
		name string
		p    config.Provider
		want bool
	}{
		{
			name: "anthropic behavior with SupportsCountTokens true",
			p:    config.Provider{Behavior: "anthropic", SupportsCountTokens: true},
			want: true,
		},
		{
			name: "openai behavior with SupportsCountTokens false",
			p:    config.Provider{Behavior: "openai", SupportsCountTokens: false},
			want: false,
		},
		{
			name: "mix behavior with SupportsCountTokens true (set by applyDefaults when base_url path is /v1/messages)",
			p:    config.Provider{Behavior: "mix", DefaultBaseURL: "https://x/v1/messages", SupportsCountTokens: true},
			want: true,
		},
		{
			name: "mix behavior with SupportsCountTokens false (set by applyDefaults when base_url path is /v1/chat/completions)",
			p: config.Provider{
				Behavior:            "mix",
				DefaultBaseURL:      "https://x/v1/chat/completions",
				SupportsCountTokens: false,
			},
			want: false,
		},
		{
			name: "empty provider",
			p:    config.Provider{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportsCountTokens(tt.p); got != tt.want {
				t.Errorf("supportsCountTokens(%+v) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}
