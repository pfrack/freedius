package proxy

import "net/http"

// fallbackUserAgent is the User-Agent sent upstream when the client did not
// provide a usable one. It must never be Go's default "Go-http-client/1.1":
// Cloudflare-protected upstreams (inference-api.nousresearch.com) return a
// 403 HTML challenge for it, which the dispatcher classifies as 401
// authentication_error — a misleading auth error for what is really a bot
// block. Verified live 2026-10-12: any non-Go UA (even "freedius/1.0")
// passes; "Go-http-client/1.1" is blocked.
const fallbackUserAgent = "freedius/1.0 (+https://github.com/pfrack/freedius)"

// goDefaultUserAgent is what net/http's client sends when no UA is set.
const goDefaultUserAgent = "Go-http-client/1.1"

// resolveUserAgent returns the User-Agent to attach to an upstream request.
// The incoming client's User-Agent is forwarded so upstreams that gate on
// caller identity see the real client:
//   - Cloudflare bot rules on inference-api.nousresearch.com block the Go
//     default UA (403 → surfaced as 401).
//   - OpenRouter's "agentic harness" gate passes claude-cli's UA and 403s
//     everything else on free models.
//
// A missing or Go-default incoming UA falls back to fallbackUserAgent.
func resolveUserAgent(r *http.Request) string {
	if r == nil {
		return fallbackUserAgent
	}
	ua := r.Header.Get("User-Agent")
	if ua == "" || ua == goDefaultUserAgent {
		return fallbackUserAgent
	}
	return ua
}
