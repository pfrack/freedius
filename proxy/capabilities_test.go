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
	tests := []struct {
		name string
		m    config.Model
		want bool
	}{
		{
			name: "anthropic provider",
			m:    config.Model{Provider: "anthropic"},
			want: true,
		},
		{
			name: "mix with anthropic protocol",
			m:    config.Model{Provider: "mix", Protocol: "anthropic"},
			want: true,
		},
		{
			name: "mix with openai protocol",
			m:    config.Model{Provider: "mix", Protocol: "openai"},
			want: false,
		},
		{
			name: "mix no protocol + /v1/messages URL sniff",
			m: config.Model{
				Provider: "mix",
				BaseURL:  "https://api.minimax.io/anthropic/v1/messages",
			},
			want: true,
		},
		{
			name: "mix no protocol + other URL",
			m: config.Model{
				Provider: "mix",
				BaseURL:  "https://integrate.api.nvidia.com/v1/chat/completions",
			},
			want: false,
		},
		{
			name: "mix no protocol + empty BaseURL",
			m:    config.Model{Provider: "mix"},
			want: false,
		},
		{
			name: "mix no protocol + unparseable BaseURL",
			m: config.Model{
				Provider: "mix",
				BaseURL:  "://bad",
			},
			want: false,
		},
		{
			name: "nim provider",
			m:    config.Model{Provider: "nim"},
			want: false,
		},
		{
			name: "openai provider",
			m:    config.Model{Provider: "openai"},
			want: false,
		},
		{
			name: "empty provider",
			m:    config.Model{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportsCountTokens(tt.m); got != tt.want {
				t.Errorf("supportsCountTokens(%+v) = %v, want %v", tt.m, got, tt.want)
			}
		})
	}
}
