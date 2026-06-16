package proxy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

const nimDefaultBaseURL = "https://integrate.api.nvidia.com"
const nimChatCompletionsPath = "/v1/chat/completions"

type NIMAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *slog.Logger
}

type NIMAdapterConfig struct {
	BaseURL string
	APIKey  string
}

func NewNIMAdapter(cfg NIMAdapterConfig, logger *slog.Logger) *NIMAdapter {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = nimDefaultBaseURL
	}
	return &NIMAdapter{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		client:  &http.Client{},
		logger:  logger.With("component", "adapter.nim"),
	}
}

func (a *NIMAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	upstreamBody, err := translate.TranslateRequest(body, m.Model)
	if err != nil {
		return fmt.Errorf("nim adapter: translate request: %w", err)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		a.baseURL+nimChatCompletionsPath, bytes.NewReader(upstreamBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
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
