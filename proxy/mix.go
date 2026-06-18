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
// based on the suffix of the configured base URL path. A path ending in
// "/v1/messages" goes through the Anthropic adapter; everything else goes
// through the OpenAI-compatible adapter.
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
// on m.Protocol (if set) or the suffix of m.BaseURL's path.
func (a *MixAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	switch m.Protocol {
	case "anthropic":
		a.logger.Debug("mix routing", "protocol", m.Protocol, "selected", "anthropic")
		return a.anthropic.Handle(w, r, m, body)
	case "openai":
		a.logger.Debug("mix routing", "protocol", m.Protocol, "selected", "openai")
		return a.openai.Handle(w, r, m, body)
	default:
		a.logger.Warn("mix: unknown protocol, falling back to URL sniffing", "protocol", m.Protocol)
	}
	parsedURL, err := url.Parse(m.BaseURL)
	if err != nil {
		return &configError{
			err:     fmt.Errorf("%s adapter (mix): parse base_url: %w", originalOr(m), err),
			errType: "invalid_request_error",
		}
	}
	if strings.HasSuffix(parsedURL.Path, "/v1/messages") {
		a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "anthropic")
		return a.anthropic.Handle(w, r, m, body)
	}
	a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "openai")
	return a.openai.Handle(w, r, m, body)
}
