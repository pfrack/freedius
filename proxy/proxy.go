// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. Metadata (model name,
// provider, status code, request_id) is acceptable; message content, tool
// arguments, tool results, and API responses are not.

package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/pfrack/freedius/config"
)

const MaxBodyBytes = 10 * 1024 * 1024

type contextKey int

const requestIDKey contextKey = iota

type Dispatcher struct {
	Cfg           *config.Config
	Logger        *slog.Logger
	Registry      *Registry
	VerboseErrors bool
}

func NewDispatcher(cfg *config.Config, registry *Registry, logger *slog.Logger, verboseErrors bool) *Dispatcher {
	if cfg == nil {
		panic("proxy: nil config")
	}
	if logger == nil {
		panic("proxy: nil logger")
	}
	if registry == nil {
		panic("proxy: nil registry")
	}
	return &Dispatcher{
		Cfg:           cfg,
		Logger:        logger.With("component", "proxy"),
		Registry:      registry,
		VerboseErrors: verboseErrors,
	}
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.writeErrorJSON(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed (only POST is accepted)")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		d.writeErrorJSON(w, r, http.StatusUnsupportedMediaType, "unsupported_content_type", "unsupported content type, expected application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			d.writeErrorJSON(w, r, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("request body too large (limit: %d bytes)", mbe.Limit))
			return
		}
		d.writeErrorJSON(w, r, http.StatusBadRequest, "body_unreadable", fmt.Sprintf("request body unreadable: %v", err))
		return
	}

	if len(body) == 0 {
		d.writeErrorJSON(w, r, http.StatusBadRequest, "empty_body", "invalid request body: empty")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		d.writeErrorJSON(w, r, http.StatusBadRequest, "invalid_json", fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Model == "" {
		d.writeErrorJSON(w, r, http.StatusBadRequest, "missing_model", "invalid request body: missing or empty \"model\" field")
		return
	}

	m, ok := d.Cfg.Models[req.Model]
	if !ok {
		m, ok = d.Cfg.Mappings[req.Model]
		if !ok {
			if family, found := extractFamily(req.Model); found {
				m, ok = d.Cfg.Mappings[family]
			}
		}
	}
	if !ok {
		d.Logger.Debug("no match for model", "request_id", RequestIDFromContext(r.Context()), "model", req.Model)
		d.writeErrorJSON(w, r, http.StatusNotFound, "no_match", fmt.Sprintf("no configured mapping for model %q", req.Model))
		return
	}

	d.Logger.Debug("dispatch", "request_id", RequestIDFromContext(r.Context()), "model", req.Model, "provider", originalOr(m), "target_model", m.Model)
	w.Header().Set("X-Freedius-Matched-Provider", originalOr(m))
	w.Header().Set("X-Freedius-Matched-Model", m.Model)

	adapter, ok := d.Registry.Lookup(m.Provider)
	if !ok {
		d.Logger.Error("provider not registered", "request_id", RequestIDFromContext(r.Context()), "provider", m.Provider)
		d.writeErrorJSON(w, r, http.StatusInternalServerError, "provider_not_registered", fmt.Sprintf("provider %q is not registered in this freedius build", originalOr(m)))
		return
	}
	ww := &wroteHeaderResponseWriter{ResponseWriter: w}
	if err := adapter.Handle(ww, r, m, body); err != nil {
		if !ww.wroteHeader {
			// Pre-WriteHeader error — safe to forward to the client.
			d.Logger.Error("adapter failed", "request_id", RequestIDFromContext(r.Context()), "provider", originalOr(m), "err", err)
			d.writeErrorJSON(w, r, http.StatusBadGateway, "upstream_error", "request to upstream provider failed", WithDetail(err.Error()))
		} else {
			// Post-WriteHeader error — adapter already sent a response.
			// Log and discard to avoid "superfluous WriteHeader" panics.
			d.Logger.Error("adapter returned error after writing response headers", "request_id", RequestIDFromContext(r.Context()), "provider", originalOr(m), "err", err)
		}
	}
}

func originalOr(m config.Model) string {
	if m.OriginalProvider != "" {
		return m.OriginalProvider
	}
	return m.Provider
}

type ErrorOption func(*errorJSON)

type errorJSON struct {
	detail string
}

func WithDetail(detail string) ErrorOption {
	return func(e *errorJSON) { e.detail = detail }
}

func (d *Dispatcher) writeErrorJSON(w http.ResponseWriter, r *http.Request, status int, code, message string, opts ...ErrorOption) {
	e := &errorJSON{}
	for _, opt := range opts {
		opt(e)
	}
	body := map[string]string{
		"error":   code,
		"message": message,
	}
	if id := RequestIDFromContext(r.Context()); id != "" {
		body["request_id"] = id
	}
	if e.detail != "" && d.VerboseErrors {
		body["detail"] = e.detail
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		d.Logger.Error("response encode failed", "request_id", RequestIDFromContext(r.Context()), "err", err)
	}
}

// --- Request ID middleware ---

func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Freedius-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- Panic recovery middleware ---

type wroteHeaderResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	code        int
}

func (w *wroteHeaderResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *wroteHeaderResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.code = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher.
// Without this, http.NewResponseController(wrapper).Flush() returns "feature not
// supported" because the wrapper hides Flusher behind the embedded interface.
// See proxy.go and proxy/middleware_test.go for the regression test.
func (w *wroteHeaderResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func RecoverMiddleware(logger *slog.Logger, verboseErrors bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &wroteHeaderResponseWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// RecoverMiddleware is wired one layer below RequestIDMiddleware, so
			// r.Context() carries the request ID. We read from the response header
			// as a deliberate robustness choice — it works regardless of middleware
			// reordering.
			id := ww.Header().Get("X-Freedius-Request-ID")
			stack := debug.Stack()
			logger.Error("panic recovered",
				"request_id", id,
				"path", r.URL.Path,
				"panic", fmt.Sprintf("%v", rec),
				"stack", string(stack),
			)
			_ = verboseErrors // reserved for future detail-gating on internal errors
			if !ww.wroteHeader {
				writeInternalErrorResponse(w, id)
			}
		}()
		next.ServeHTTP(ww, r)
	})
}

func writeInternalErrorResponse(w http.ResponseWriter, requestID string) {
	body := map[string]string{
		"error":   "internal_error",
		"message": "freedius encountered an internal error",
	}
	if requestID != "" {
		body["request_id"] = requestID
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(body)
}

// --- Access log middleware ---

func AccessLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &wroteHeaderResponseWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)
		id := RequestIDFromContext(r.Context())
		matchedProvider := ww.Header().Get("X-Freedius-Matched-Provider")
		matchedModel := ww.Header().Get("X-Freedius-Matched-Model")
		status := ww.code
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("request complete",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"matched_provider", matchedProvider,
			"matched_model", matchedModel,
		)
	})
}
