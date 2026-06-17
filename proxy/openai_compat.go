package proxy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

type OpenAICompatibleAdapter struct {
	client        *http.Client
	logger        *slog.Logger
	streamTimeout time.Duration
	translateOpts translate.TranslateOpts
	preSendHook   func([]byte) ([]byte, error)
}

func NewOpenAICompatibleAdapter(logger *slog.Logger) *OpenAICompatibleAdapter {
	return NewOpenAICompatibleAdapterWithTimeout(logger, 5*time.Minute)
}

func NewOpenAICompatibleAdapterWithTimeout(logger *slog.Logger, streamTimeout time.Duration) *OpenAICompatibleAdapter {
	return &OpenAICompatibleAdapter{
		client: &http.Client{
			Timeout: 0, // bounded per-request via context.WithTimeout below
			Transport: &http.Transport{
				DisableKeepAlives: false,
				Proxy:             http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		logger:        logger.With("component", "adapter.openai"),
		streamTimeout: streamTimeout,
	}
}

func (a *OpenAICompatibleAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	if m.BaseURL == "" {
		return fmt.Errorf("%s adapter (openai-compat): missing base_url", originalOr(m))
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s adapter (openai-compat): env var %s is not set", originalOr(m), m.APIKeyEnv)
	}
	upstreamBody, err := translate.TranslateRequest(body, m.Model, a.translateOpts)
	if err != nil {
		return fmt.Errorf("%s adapter (openai-compat): translate request: %w", originalOr(m), err)
	}
	if a.preSendHook != nil {
		upstreamBody, err = a.preSendHook(upstreamBody)
		if err != nil {
			return fmt.Errorf("%s adapter (openai-compat): sanitize body: %w", originalOr(m), err)
		}
	}
	// Bound the upstream call so a hanging provider cannot pin the goroutine.
	// Cancellation still propagates via r.Context() to the upstream request.
	ctx, cancel := context.WithTimeout(r.Context(), a.streamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return fmt.Errorf("%s adapter (openai-compat): build request: %w", originalOr(m), err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s adapter (openai-compat): do request: %w", originalOr(m), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Forward the error; any copy error is ignored because response is already in flight
		_ = forwardUpstreamError(w, resp)
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := translate.TranslateStream(resp.Body, w, rc.Flush); err != nil {
		// Response already started; log the error but do not return it
		a.logger.Error("stream translation error", "err", err)
	}
	return nil
}
