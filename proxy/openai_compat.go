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

// OpenAICompatibleAdapter translates Anthropic-format requests into the
// OpenAI Chat Completions format and streams the translated SSE response back.
type OpenAICompatibleAdapter struct {
	client        *http.Client
	logger        *slog.Logger
	streamTimeout time.Duration
	translateOpts translate.Opts
	preSendHook   func([]byte) ([]byte, error)
}

// NewOpenAICompatibleAdapter returns an adapter with the default stream
// timeout (5 minutes). Use NewOpenAICompatibleAdapterWithTimeout to override.
func NewOpenAICompatibleAdapter(logger *slog.Logger) *OpenAICompatibleAdapter {
	return NewOpenAICompatibleAdapterWithTimeout(logger, 5*time.Minute)
}

// NewOpenAICompatibleAdapterWithTimeout returns an adapter that aborts the
// upstream call after streamTimeout (per-request, via context.WithTimeout).
func NewOpenAICompatibleAdapterWithTimeout(
	logger *slog.Logger,
	streamTimeout time.Duration,
) *OpenAICompatibleAdapter {
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

// Handle translates the incoming Anthropic request, sends it to the OpenAI-
// compatible upstream, and streams the translated SSE response back to the
// caller. The body argument is the already-read request body.
func (a *OpenAICompatibleAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	if m.BaseURL == "" {
		return &configError{
			err:     fmt.Errorf("%s adapter (openai-compat): missing base_url", originalOr(m)),
			errType: "invalid_request_error",
		}
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): env var %s is not set",
				originalOr(m),
				m.APIKeyEnv,
			),
			errType: "authentication_error",
		}
	}
	upstreamBody, err := translate.Request(body, m.Model, a.translateOpts)
	if err != nil {
		return &configError{
			err:     fmt.Errorf("%s adapter (openai-compat): translate request: %w", originalOr(m), err),
			errType: "invalid_request_error",
		}
	}
	if a.preSendHook != nil {
		upstreamBody, err = a.preSendHook(upstreamBody)
		if err != nil {
			return &configError{
				err:     fmt.Errorf("%s adapter (openai-compat): sanitize body: %w", originalOr(m), err),
				errType: "invalid_request_error",
			}
		}
	}
	// Bound the upstream call so a hanging provider cannot pin the goroutine.
	// Cancellation still propagates via r.Context() to the upstream request.
	ctx, cancel := context.WithTimeout(r.Context(), a.streamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		m.BaseURL,
		bytes.NewReader(upstreamBody),
	)
	if err != nil {
		return &configError{
			err:     fmt.Errorf("%s adapter (openai-compat): build request: %w", originalOr(m), err),
			errType: "invalid_request_error",
		}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s adapter (openai-compat): do request: %w", originalOr(m), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		translateUpstreamError(w, resp)
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	var reasoning string
	reasoning, err = translate.Stream(resp.Body, w, rc.Flush)
	_ = reasoning
	if err != nil {
		// Response already started; log the error but do not return it
		a.logger.Error("stream translation error", "err", err)
	}
	return nil
}
