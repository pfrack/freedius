package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

func forwardUpstreamError(w http.ResponseWriter, resp *http.Response) error {
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

// writeAnthropicError writes an Anthropic-shaped error JSON response with
// appropriate retry headers. If retryAfter > 0, sets retry-after and
// x-should-retry: true headers.
func writeAnthropicError(w http.ResponseWriter, statusCode int, errType, message string, retryAfter int) {
	if retryAfter > 0 {
		w.Header().Set("retry-after", strconv.Itoa(retryAfter))
		w.Header().Set("x-should-retry", "true")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
}

// translateUpstreamError maps a non-Anthropic upstream error response to an
// Anthropic-format error and writes it to w. Reads up to 256 bytes of the
// upstream body for the message. Does NOT close resp.Body.
func translateUpstreamError(w http.ResponseWriter, resp *http.Response) {
	// Read a snippet of the upstream body for the message.
	snippet := make([]byte, 256)
	n, _ := io.ReadAtLeast(resp.Body, snippet, 1)
	msg := sanitizePrintable(snippet[:n])

	var status int
	var errType string
	var retryAfter int

	switch {
	case resp.StatusCode == 429:
		status = 429
		errType = "rate_limit_error"
		retryAfter = parseRetryAfter(resp.Header.Get("retry-after"), 15)
	case resp.StatusCode == 503 || resp.StatusCode == 529:
		status = 529
		errType = "overloaded_error"
		retryAfter = parseRetryAfter(resp.Header.Get("retry-after"), 15)
	case resp.StatusCode == 500:
		status = 500
		errType = "api_error"
		retryAfter = 15
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		status = 401
		errType = "authentication_error"
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		status = resp.StatusCode
		errType = "invalid_request_error"
	default: // other 5xx
		status = 529
		errType = "overloaded_error"
		retryAfter = 15
	}

	writeAnthropicError(w, status, errType, fmt.Sprintf("upstream: %s", msg), retryAfter)
}

func parseRetryAfter(header string, fallback int) int {
	if v, err := strconv.Atoi(header); err == nil && v > 0 {
		return v
	}
	return fallback
}

func sanitizePrintable(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c >= 0x20 && c <= 0x7e {
			out = append(out, c)
		}
	}
	return string(out)
}

// freediusErrorHandler returns a transport-error handler for httputil.ReverseProxy
// that emits the unified error JSON shape (`error` / `message` / `detail` /
// `request_id`). Client cancellations are still logged at Debug and produce
// no response body — the connection simply closes. The `detail` field is
// gated on verboseErrors, matching writeErrorJSON (proxy.go:165).
func freediusErrorHandler(
	logger *slog.Logger,
	verboseErrors bool,
) func(http.ResponseWriter, *http.Request, error) {
	_ = verboseErrors // reserved for future structured logging gating
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			logger.Debug(
				"client disconnect",
				"request_id",
				RequestIDFromContext(r.Context()),
				"path",
				r.URL.Path,
			)
			return
		}
		logger.Error(
			"upstream transport error",
			"request_id", RequestIDFromContext(r.Context()),
			"path", r.URL.Path,
			"err", err,
		)
		writeAnthropicError(w, 529, "overloaded_error",
			"upstream not reachable", 15)
	}
}
