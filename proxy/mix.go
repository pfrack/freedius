package proxy

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/pfrack/freedius/config"
)

type MixAdapter struct {
	anthropic *AnthropicCompatibleAdapter
	openai    *OpenAICompatibleAdapter
}

func NewMixAdapter(logger *slog.Logger) *MixAdapter {
	return &MixAdapter{
		anthropic: NewAnthropicCompatibleAdapter(logger),
		openai:    NewOpenAICompatibleAdapter(logger),
	}
}

func (a *MixAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	parsedURL, err := url.Parse(m.BaseURL)
	if err != nil {
		return err
	}
	if strings.HasSuffix(parsedURL.Path, "/v1/messages") {
		return a.anthropic.Handle(w, r, m, body)
	}
	return a.openai.Handle(w, r, m, body)
}
