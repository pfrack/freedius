// Package proxy implements the freedius HTTP reverse proxy: provider adapters,
// middleware (request ID, recover, access log), and the request dispatcher.
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pfrack/freedius/config"
)

// AnthropicCompatibleAdapter forwards requests to an Anthropic-API-compatible
// upstream. For upstream HTTP errors (4xx/5xx), it returns a typed
// upstreamError instead of writing directly, enabling the dispatcher's
// fallback loop to retry against another provider.
type AnthropicCompatibleAdapter struct {
	logger        *slog.Logger
	verboseErrors bool
	streamTimeout time.Duration
}

// NewAnthropicCompatibleAdapter returns an adapter with the default stream
// timeout (5 minutes). Use NewAnthropicCompatibleAdapterWithTimeout to override.
func NewAnthropicCompatibleAdapter(
	logger *slog.Logger,
	verboseErrors bool,
) *AnthropicCompatibleAdapter {
	return NewAnthropicCompatibleAdapterWithTimeout(logger, verboseErrors, 5*time.Minute)
}

// NewAnthropicCompatibleAdapterWithTimeout returns an adapter that aborts the
// upstream call after streamTimeout (per-request, via context.WithTimeout).
func NewAnthropicCompatibleAdapterWithTimeout(
	logger *slog.Logger,
	verboseErrors bool,
	streamTimeout time.Duration,
) *AnthropicCompatibleAdapter {
	return &AnthropicCompatibleAdapter{
		logger:        logger.With("component", "adapter.anthropic"),
		verboseErrors: verboseErrors,
		streamTimeout: streamTimeout,
	}
}

// Handle rewrites the request for the upstream Anthropic-API-compatible base
// URL, sets x-api-key / anthropic-version, and forwards the response. On
// upstream HTTP errors (>= 400), returns a typed upstreamError for fallback
// eligibility. On transport errors, writes the Anthropic error envelope
// directly via freediusErrorHandler (pre-write, already fallback-eligible).
func (a *AnthropicCompatibleAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	if provider.DefaultBaseURL == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): missing base_url",
				mapping.ProviderName,
			),
			errType: "invalid_request_error",
		}
	}
	apiKey := os.Getenv(provider.DefaultAPIKeyEnv)
	if apiKey == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): env var %s is not set",
				mapping.ProviderName,
				provider.DefaultAPIKeyEnv,
			),
			errType: "authentication_error",
		}
	}
	target, err := url.Parse(provider.DefaultBaseURL)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): invalid base_url %q: %w",
				mapping.ProviderName,
				provider.DefaultBaseURL,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	apiVersion := provider.AnthropicVersion
	if apiVersion == "" {
		apiVersion = "2023-06-01"
	}

	// Bound the upstream call so a hanging provider cannot pin the goroutine.
	ctx, cancel := context.WithTimeout(r.Context(), a.streamTimeout)
	defer cancel()

	// Build the upstream request directly so we can inspect the response
	// before committing to write — enables fallback on 4xx/5xx.
	upstreamReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): build request: %w",
				mapping.ProviderName,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("x-api-key", apiKey)
	upstreamReq.Header.Set("anthropic-version", apiVersion)

	// Use a shared HTTP client — transport errors surface here.
	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		// Transport error — write the Anthropic error envelope directly.
		// This is pre-write, so it's fallback-eligible at the dispatcher level.
		a.writeTransportError(w, r, err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return classifyUpstreamError(resp)
	}

	// Success path: stream the upstream response through.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return nil
}

// writeTransportError writes the Anthropic error envelope for transport-level
// errors. Mirrors freediusErrorHandler's logic but is a method on the adapter
// so it can access the logger and verboseErrors flag.
func (a *AnthropicCompatibleAdapter) writeTransportError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if context.Cause(r.Context()) != nil || r.Context().Err() != nil {
		a.logger.Debug(
			"client disconnect",
			"request_id", RequestIDFromContext(r.Context()),
			"path", r.URL.Path,
		)
		return
	}
	a.logger.Error(
		"upstream transport error",
		"request_id", RequestIDFromContext(r.Context()),
		"path", r.URL.Path,
		"err", err,
	)
	if a.verboseErrors {
		a.logger.Debug(
			"upstream transport error detail (verbose)",
			"request_id", RequestIDFromContext(r.Context()),
			"path", r.URL.Path,
			"err", err.Error(),
		)
	}
	if isPermanentTransportError(err) {
		writeAnthropicError(w, 502, "api_error", "upstream not reachable", 0)
	} else {
		writeAnthropicError(w, 529, "overloaded_error", "upstream not reachable", 15)
	}
}
