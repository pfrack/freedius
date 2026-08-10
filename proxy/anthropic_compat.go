// Package proxy implements the freedius HTTP reverse proxy: provider adapters,
// middleware (request ID, recover, access log), and the request dispatcher.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		// Transport error — return an upstreamError so the dispatcher can
		// fallback to the next provider. When the chain exhausts, the
		// dispatcher writes the Anthropic-shaped error via writeAnthropicError.
		a.logTransportError(r, err)
		if isPermanentTransportError(err) {
			return &upstreamError{
				status:  502,
				errType: "api_error",
				message: "upstream not reachable",
			}
		}
		return &upstreamError{
			status:     529,
			errType:    "overloaded_error",
			message:    "upstream not reachable",
			retryAfter: 15,
		}
	}
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 400 {
		return classifyUpstreamError(resp)
	}

	contentType := resp.Header.Get("Content-Type")
	originalModel := RequestModelFromContext(r.Context())

	// If model rewriting is needed for a non-streaming JSON response, read and
	// transform it before copying headers so Content-Length cannot describe the
	// upstream body after the model field changes.
	if originalModel != "" && strings.Contains(contentType, "application/json") {
		responseBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxBodyBytes)+1))
		if err == nil {
			if len(responseBody) > MaxBodyBytes {
				// Oversized JSON responses are passed through unchanged rather than
				// retained in memory for rewriting.
				for k, vv := range resp.Header {
					for _, v := range vv {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(responseBody)
				_, _ = io.Copy(w, resp.Body)
				return nil
			}

			rewritten := rewriteAnthropicModelField(responseBody, originalModel)
			for k, vv := range resp.Header {
				if strings.EqualFold(k, "Content-Length") {
					continue
				}
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(rewritten)
			return nil
		}
	}

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if originalModel != "" && strings.Contains(contentType, "text/event-stream") {
		// Streaming SSE response: rewrite first message_start event, passthrough rest.
		br := bufio.NewReader(resp.Body)
		rc := http.NewResponseController(w)
		err := forwardSSEWithModelRewrite(w, br, originalModel, rc.Flush)
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

// logTransportError logs a transport-level error. The adapter returns the
// error to the dispatcher (fallback-eligible); the Anthropic-shaped response
// is written by the dispatcher when the fallback chain exhausts.
func (a *AnthropicCompatibleAdapter) logTransportError(
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
// After the first event is rewritten, the remainder is copied unchanged.
func forwardSSEWithModelRewrite(
	w io.Writer,
	br *bufio.Reader,
	newModel string,
	flush func() error,
) error {
	for {
		lines, err := readAnthropicSSEEvent(br)
		if len(lines) > 0 {
			if rewritten, ok := rewriteAnthropicSSEMessageStart(lines, newModel); ok {
				if _, writeErr := w.Write(rewritten); writeErr != nil {
					return writeErr
				}
				if flushErr := flush(); flushErr != nil {
					return flushErr
				}
				_, copyErr := io.Copy(w, br)
				return copyErr
			}

			for _, line := range lines {
				if _, writeErr := w.Write(line); writeErr != nil {
					return writeErr
				}
			}
			if flushErr := flush(); flushErr != nil {
				return flushErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// readAnthropicSSEEvent reads complete SSE event lines through the blank
// delimiter. It returns a partial event with io.EOF if the stream ends early.
func readAnthropicSSEEvent(br *bufio.Reader) ([][]byte, error) {
	var lines [][]byte
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lines = append(lines, line)
			if len(bytes.TrimRight(line, "\r\n")) == 0 {
				return lines, nil
			}
		}
		if err != nil {
			return lines, err
		}
	}
}

// rewriteAnthropicSSEMessageStart rewrites a complete message_start event while
// preserving its non-data lines and event delimiter. SSE data fields are joined
// with newlines before JSON parsing, as required by the SSE format.
func rewriteAnthropicSSEMessageStart(lines [][]byte, newModel string) ([]byte, bool) {
	payload := anthropicSSEDataPayload(lines)
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return nil, false
	}

	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, false
	}
	if eventType, ok := event["type"].(string); !ok || eventType != "message_start" {
		return nil, false
	}
	message, ok := event["message"].(map[string]any)
	if !ok {
		return nil, false
	}
	message["model"] = newModel
	rewritten, err := json.Marshal(event)
	if err != nil {
		return nil, false
	}

	var output bytes.Buffer
	dataWritten := false
	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			if dataWritten {
				continue
			}
			output.WriteString("data: ")
			output.Write(rewritten)
			output.Write(anthropicSSELineEnding(line))
			dataWritten = true
			continue
		}
		output.Write(line)
	}
	return output.Bytes(), true
}

func anthropicSSEDataPayload(lines [][]byte) []byte {
	var payload []byte
	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r\n")
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		value := bytes.TrimPrefix(trimmed, []byte("data:"))
		value = bytes.TrimPrefix(value, []byte(" "))
		if payload != nil {
			payload = append(payload, '\n')
		}
		payload = append(payload, value...)
	}
	return payload
}

func anthropicSSELineEnding(line []byte) []byte {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return []byte("\r\n")
	}
	return []byte("\n")
}
