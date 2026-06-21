package proxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

// MixAdapter routes each request to either the Anthropic or OpenAI code path
// based on the Provider's Protocol field or, when unset, the suffix of the
// configured base URL path. A path ending in "/v1/messages" goes through the
// Anthropic adapter; everything else goes through the OpenAI-compatible
// adapter. When Protocol is set, missing endpoint suffixes are appended
// automatically.
type MixAdapter struct {
	anthropic *AnthropicCompatibleAdapter
	openai    *OpenAICompatibleAdapter
	logger    *slog.Logger
}

// NewMixAdapter returns a mix adapter wired to fresh Anthropic and OpenAI
// sub-adapters, with OpenAI's stream-usage suppressed because mix providers
// (zen, go) cannot return usage on the last chunk.
func NewMixAdapter(
	logger *slog.Logger,
	verboseErrors bool,
	streamTimeout time.Duration,
) *MixAdapter {
	openai := NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout)
	openai.translateOpts = translate.Opts{NoStreamUsage: true}
	return &MixAdapter{
		anthropic: NewAnthropicCompatibleAdapter(logger, verboseErrors),
		openai:    openai,
		logger:    logger.With("component", "adapter.mix"),
	}
}

// Handle dispatches the request to the Anthropic or OpenAI sub-adapter based
// on the Provider's Protocol field (if set) or the suffix of its
// DefaultBaseURL path. When Protocol is set, the base URL is normalized:
// missing endpoint suffixes (/v1/messages, /v1/chat/completions) are appended
// automatically.
func (a *MixAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	switch provider.Protocol {
	case "anthropic":
		provider.DefaultBaseURL = a.normalizeBaseURL(provider.DefaultBaseURL, "/messages", "/chat/completions")
		a.logger.Debug("mix routing", "protocol", provider.Protocol, "url", provider.DefaultBaseURL)
		return a.anthropic.Handle(w, r, provider, mapping, body)
	case "openai":
		provider.DefaultBaseURL = a.normalizeBaseURL(provider.DefaultBaseURL, "/chat/completions", "/messages")
		a.logger.Debug("mix routing", "protocol", provider.Protocol, "url", provider.DefaultBaseURL)
		return a.openai.Handle(w, r, provider, mapping, body)
	}
	parsedURL, err := url.Parse(provider.DefaultBaseURL)
	if err != nil {
		return &configError{
			err:     fmt.Errorf("%s adapter (mix): parse base_url: %w", mapping.ProviderName, err),
			errType: "invalid_request_error",
		}
	}
	if strings.HasSuffix(parsedURL.Path, "/v1/messages") {
		a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "anthropic")
		return a.anthropic.Handle(w, r, provider, mapping, body)
	}
	a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "openai")
	return a.openai.Handle(w, r, provider, mapping, body)
}

// normalizeBaseURL adjusts base URL so its path ends with wantSuffix. If the
// path already ends with wantSuffix it is left unchanged. If it ends with
// otherSuffix the suffix is replaced. Otherwise wantSuffix is appended.
func (a *MixAdapter) normalizeBaseURL(baseURL, wantSuffix, otherSuffix string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	p := parsed.Path
	if strings.HasSuffix(p, wantSuffix) {
		return baseURL
	}
	if strings.HasSuffix(p, otherSuffix) {
		parsed.Path = p[:len(p)-len(otherSuffix)] + wantSuffix
		return parsed.String()
	}
	parsed.Path = strings.TrimRight(p, "/") + wantSuffix
	return parsed.String()
}
