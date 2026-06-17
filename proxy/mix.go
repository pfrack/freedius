package proxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/pfrack/freedius/config"
)

type MixAdapter struct {
	anthropic *AnthropicCompatibleAdapter
	openai    *OpenAICompatibleAdapter
	logger    *slog.Logger
}

func NewMixAdapter(logger *slog.Logger) *MixAdapter {
	return &MixAdapter{
		anthropic: NewAnthropicCompatibleAdapter(logger),
		openai:    NewOpenAICompatibleAdapter(logger),
		logger:    logger.With("component", "adapter.mix"),
	}
}

func (a *MixAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	parsedURL, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf("%s adapter (mix): parse base_url: %w", originalOr(m), err)
	}
	if strings.HasSuffix(parsedURL.Path, "/v1/messages") {
		a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "anthropic")
		return a.anthropic.Handle(w, r, m, body)
	}
	a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "openai")
	return a.openai.Handle(w, r, m, body)
}
