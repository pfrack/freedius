package proxy

import (
	"bytes"
	"context"
	"encoding/json"
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
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	if provider.DefaultBaseURL == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): missing base_url",
				mapping.ProviderName,
			),
			errType: "invalid_request_error",
		}
	}
	var apiKey string
	if provider.DefaultAPIKeyEnv != "" {
		apiKey = os.Getenv(provider.DefaultAPIKeyEnv)
		if apiKey == "" {
			return &configError{
				err: fmt.Errorf(
					"%s adapter (openai-compat): env var %s is not set",
					mapping.ProviderName,
					provider.DefaultAPIKeyEnv,
				),
				errType: "authentication_error",
			}
		}
	}
	upstreamBody, err := translate.Request(body, mapping.ModelString, a.translateOpts)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): translate request: %w",
				mapping.ProviderName,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	if a.preSendHook != nil {
		upstreamBody, err = a.preSendHook(upstreamBody)
		if err != nil {
			return &configError{
				err: fmt.Errorf(
					"%s adapter (openai-compat): sanitize body: %w",
					mapping.ProviderName,
					err,
				),
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
		provider.DefaultBaseURL,
		bytes.NewReader(upstreamBody),
	)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): build request: %w",
				mapping.ProviderName,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s adapter (openai-compat): do request: %w", mapping.ProviderName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return classifyUpstreamError(resp)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	err = translate.Stream(resp.Body, w, rc.Flush)
	if err != nil {
		// Response already started; write error event in-band
		errPayload := map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "api_error",
				"message": err.Error(),
			},
		}
		errBytes, marshalErr := json.Marshal(errPayload)
		if marshalErr == nil {
			line := fmt.Sprintf("event: error\ndata: %s\n\n", errBytes)
			if _, writeErr := w.Write([]byte(line)); writeErr == nil {
				_ = rc.Flush()
			}
		}
		a.logger.Warn("stream translation error", "err", err)
	}
	return nil
}
