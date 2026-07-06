package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// configError wraps an adapter pre-flight configuration error with an
// Anthropic error.type string so the dispatcher can return 500 with the
// correct error type instead of collapsing to 529 overloaded_error.
type configError struct {
	err     error
	errType string
}

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

// upstreamError carries the classified result of an upstream HTTP error
// response (4xx/5xx). Adapters return this instead of writing the error
// directly, so the dispatcher can decide whether to retry via fallback.
type upstreamError struct {
	status     int
	errType    string
	message    string
	retryAfter int
}

func (e *upstreamError) Error() string {
	return e.message
}

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
func writeAnthropicError(
	w http.ResponseWriter,
	statusCode int,
	errType, message string,
	retryAfter int,
) {
	if retryAfter > 0 {
		w.Header().Set("retry-after", strconv.Itoa(retryAfter))
		w.Header().Set("x-should-retry", "true")
	}
	w.Header().Set("X-Freedius-Error-Type", errType)
	w.Header().Set("X-Freedius-Error-Message", message)
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

// classifyUpstreamError reads up to 256 bytes of the upstream body for the
// message, drains up to 4 KiB more so the http.Transport can reuse the
// keep-alive connection, and returns a classified *upstreamError. Does NOT
// close resp.Body — caller owns that.
func classifyUpstreamError(resp *http.Response) *upstreamError {
	// Read a snippet of the upstream body for the message.
	snippet := make([]byte, 256)
	n, _ := io.ReadAtLeast(resp.Body, snippet, 1)
	msg := sanitizePrintable(snippet[:n])
	msg = redactSensitive(msg)

	// Drain remaining body (capped) so the http.Transport can reuse the
	// keep-alive connection. Caller still owns resp.Body.Close() via defer.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))

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
		status = resp.StatusCode
		errType = "api_error"
		retryAfter = 15
	}

	return &upstreamError{
		status:     status,
		errType:    errType,
		message:    msg,
		retryAfter: retryAfter,
	}
}

// translateUpstreamError is a thin wrapper around classifyUpstreamError +
// writeAnthropicError, kept for callers that still need the direct-write
// behavior (e.g. the dispatcher's final-write path in Phase 3).
func translateUpstreamError(w http.ResponseWriter, resp *http.Response) {
	ue := classifyUpstreamError(resp)
	writeAnthropicError(w, ue.status, ue.errType, ue.message, ue.retryAfter)
}

func parseRetryAfter(header string, fallback int) int {
	if v, err := strconv.Atoi(header); err == nil && v > 0 {
		return v
	}
	return fallback
}

func sanitizePrintable(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for _, r := range string(b) {
		if unicode.IsPrint(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

var (
	reOpenAIKey    = regexp.MustCompile(`\bsk-[a-zA-Z0-9]{20,}\b`)
	reAnthropicKey = regexp.MustCompile(`\bsk-ant-[a-zA-Z0-9-]{20,}\b`)
	reBearerToken  = regexp.MustCompile(`Bearer [a-zA-Z0-9._-]{20,}`)
	reKeyAdjacent  = regexp.MustCompile(`(?i)(key|token|secret|api_key)[\s=:]+[a-zA-Z0-9._-]{40,}`)
)

// redactSensitive replaces API key patterns in s with [REDACTED].
// Patterns redacted:
//   - sk-... (OpenAI-style keys, 20+ alphanumeric chars)
//   - sk-ant-... (Anthropic-style keys, 20+ alphanumeric chars)
//   - Bearer ... (Bearer tokens, 20+ alphanumeric/dot/dash chars)
//   - key/token/secret/api_key = <40+ alphanumeric/dot/dash chars>
func redactSensitive(s string) string {
	s = reOpenAIKey.ReplaceAllString(s, "[REDACTED]")
	s = reAnthropicKey.ReplaceAllString(s, "[REDACTED]")
	s = reBearerToken.ReplaceAllString(s, "[REDACTED]")
	s = reKeyAdjacent.ReplaceAllStringFunc(s, func(match string) string {
		// Preserve the keyword prefix, redact only the value.
		for _, kw := range []string{"key", "token", "secret", "api_key"} {
			lower := strings.ToLower(match)
			idx := strings.Index(lower, kw)
			if idx >= 0 {
				// Find where the keyword ends and the value begins.
				rest := match[idx+len(kw):]
				// Skip separator chars (spaces, =, :, etc.)
				i := 0
				for i < len(rest) && (rest[i] == ' ' || rest[i] == '=' || rest[i] == ':') {
					i++
				}
				return match[:idx+len(kw)+i] + "[REDACTED]"
			}
		}
		return "[REDACTED]"
	})
	return s
}

// isPermanentTransportError returns true when err is a permanent transport
// failure (DNS resolution failure, TLS certificate/handshake error) that
// should not be retried. Connection refused, connection reset, and I/O
// timeout errors are considered transient and return false.
func isPermanentTransportError(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return true
	}
	var x509Err *x509.UnknownAuthorityError
	if errors.As(err, &x509Err) {
		return true
	}
	var x509HostnameErr *x509.HostnameError
	if errors.As(err, &x509HostnameErr) {
		return true
	}
	var x509InvalidErr *x509.CertificateInvalidError
	return errors.As(err, &x509InvalidErr)
}

// freediusErrorHandler returns a transport-error handler for httputil.ReverseProxy
// that emits the Anthropic error envelope on transport failures. Client
// cancellations are still logged at Debug and produce no response body — the
// connection simply closes. The `verboseErrors` flag gates a Debug-level
// log line that includes the full upstream error string for local debugging
// (the Anthropic envelope has no `detail` field, so this is server-side only).
func freediusErrorHandler(
	logger *slog.Logger,
	verboseErrors bool,
) func(http.ResponseWriter, *http.Request, error) {
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
		if verboseErrors {
			logger.Debug(
				"upstream transport error detail (verbose)",
				"request_id", RequestIDFromContext(r.Context()),
				"path", r.URL.Path,
				"err", err.Error(),
			)
		}
		if isPermanentTransportError(err) {
			writeAnthropicError(w, 502, "api_error",
				"upstream not reachable", 0)
		} else {
			writeAnthropicError(w, 529, "overloaded_error",
				"upstream not reachable", 15)
		}
	}
}
