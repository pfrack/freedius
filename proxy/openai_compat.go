package proxy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

type OpenAICompatibleAdapter struct {
	client *http.Client
	logger *slog.Logger
}

func NewOpenAICompatibleAdapter(logger *slog.Logger) *OpenAICompatibleAdapter {
	return &OpenAICompatibleAdapter{
		client: &http.Client{},
		logger: logger.With("component", "adapter.openai"),
	}
}

func (a *OpenAICompatibleAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	if m.BaseURL == "" {
		return fmt.Errorf("openai adapter: missing base_url")
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("openai adapter: env var %s is not set", m.APIKeyEnv)
	}
	upstreamBody, err := translate.TranslateRequest(body, m.Model)
	if err != nil {
		return fmt.Errorf("openai adapter: translate request: %w", err)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, m.BaseURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return fmt.Errorf("openai adapter: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("openai adapter: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return forwardUpstreamError(w, resp)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	return translate.TranslateStream(resp.Body, w, rc.Flush)
}
