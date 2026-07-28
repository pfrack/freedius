// Package proxy implements the freedius HTTP reverse proxy: provider adapters,
// middleware (request ID, recover, access log), and the request dispatcher.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
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

	// Success path: forward the upstream response, rewriting model field if needed.
	// For streaming responses, only the first message_start event contains the model field.
	// For non-streaming, it's a top-level field in the JSON response.
	contentType := resp.Header.Get("Content-Type")
	originalModel := RequestModelFromContext(r.Context())

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// If model rewriting is needed, handle based on Content-Type.
	if originalModel != "" && strings.Contains(contentType, "application/json") {
		// Non-streaming JSON response: read, rewrite, write.
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			rewritten := rewriteAnthropicModelField(body, originalModel)
			_, _ = w.Write(rewritten)
			return nil
		}
		// On read error, fall back to passthrough.
	} else if originalModel != "" && strings.Contains(contentType, "text/event-stream") {
		// Streaming SSE response: rewrite first message_start event, passthrough rest.
		br := bufio.NewReader(resp.Body)
		err := forwardSSEWithModelRewrite(w, br, originalModel)
		if err != nil && err != io.EOF {
			// Log but don't error (response already started).
			a.logger.Warn("sse forward error", "err", err)
		}
		return nil
	}

	// No model rewriting needed or not a recognized content type: passthrough.
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

// rewriteAnthropicModelField rewrites the top-level "model" field in a JSON
// response body. Used for non-streaming application/json responses.
func rewriteAnthropicModelField(body []byte, newModel string) []byte {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// Malformed JSON; return unchanged.
		return body
	}
	if _, ok := data["model"]; !ok {
		// No model field; return unchanged.
		return body
	}
	// Rewrite model field.
	data["model"] = newModel
	rewritten, err := json.Marshal(data)
	if err != nil {
		// Marshalling error; return original.
		return body
	}
	return rewritten
}

// forwardSSEWithModelRewrite reads an SSE stream from br and writes it to w,
// rewriting the first message_start event's message.model field to newModel.
// After the first event is rewritten, subsequent events are passed through unchanged.
func forwardSSEWithModelRewrite(w io.Writer, br *bufio.Reader, newModel string) error {
	rewritten := false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			// Check if this is a data: line
			trimmed := bytes.TrimRight(line, "\r\n")
			if bytes.HasPrefix(trimmed, []byte("data:")) && !rewritten {
				// This might be the message_start event; try to parse and rewrite.
				dataPayload := bytes.TrimPrefix(trimmed, []byte("data:"))
				dataPayload = bytes.TrimLeft(dataPayload, " ")
				if bytes.Equal(dataPayload, []byte("[DONE]")) {
					// Not a JSON event, just pass through.
					if _, writeErr := w.Write(line); writeErr != nil {
						return writeErr
					}
				} else {
					// Try to parse as JSON; if it's message_start, rewrite the model.
					rewritten = tryRewriteSSEEvent(w, dataPayload, newModel, line)
				}
			} else {
				// Not a data line or already rewritten; pass through.
				if _, writeErr := w.Write(line); writeErr != nil {
					return writeErr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// tryRewriteSSEEvent attempts to parse a data payload from an SSE event and
// rewrite the message.model field if it's a message_start event. Returns true
// if rewritten, false otherwise. Always writes to w.
func tryRewriteSSEEvent(w io.Writer, payload []byte, newModel string, fallback []byte) bool {
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		// Not JSON; write original line and return false.
		_, _ = w.Write(fallback)
		return false
	}
	if eventType, ok := event["type"].(string); !ok || eventType != "message_start" {
		// Not a message_start event; write original and return false.
		_, _ = w.Write(fallback)
		return false
	}
	// This is a message_start event; rewrite the message.model field.
	if message, ok := event["message"].(map[string]any); ok {
		message["model"] = newModel
		rewritten, err := json.Marshal(event)
		if err != nil {
			// Marshal failed; write original.
			_, _ = w.Write(fallback)
			return false
		}
		// Write rewritten event and flush line ending.
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(rewritten)
		_, _ = w.Write([]byte("\n"))
		return true
	}
	// message field not found or wrong type; write original.
	_, _ = w.Write(fallback)
	return false
}
