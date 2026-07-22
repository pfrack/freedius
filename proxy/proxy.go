// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. Metadata (model name,
// provider, status code, request_id) is acceptable; message content, tool
// arguments, tool results, and API responses are not.

package proxy

import (
	"bytes"
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
	"sync/atomic"
	"time"

	"github.com/pfrack/freedius/config"
)

// MaxBodyBytes is the upper bound on the size of a request body the dispatcher
// will read into memory before returning 413.
const MaxBodyBytes = 10 * 1024 * 1024

type contextKey int

const requestIDKey contextKey = iota

// Dispatcher is the top-level HTTP handler that resolves a freedius request
// to a configured model, looks up the right Provider in the Registry, and
// forwards the request.
//
// VerboseErrors is read on HTTP handler goroutines (writeErrorJSON) and
// toggled on the TUI goroutine (Ctrl+E in Phase 5). It is stored as
// atomic.Bool so the toggle is race-detector clean without locking the
// hot request path.
type Dispatcher struct {
	Cfg           *config.Config
	Logger        *slog.Logger
	Registry      *Registry
	verboseErrors atomic.Bool
	// fallbackTimeoutMultiplier scales the per-attempt stream timeout to
	// derive the shared budget for the whole fallback chain. Default 2.
	fallbackTimeoutMultiplier int
	// streamTimeout is the per-attempt upstream timeout, used to compute
	// the shared fallback chain budget.
	streamTimeout time.Duration
	// LastResponder aggregates the most recent successful responder index
	// per mapping (nil-safe; the dispatch path skips recording when nil).
	// Wired by main.go after construction; tests leave it nil.
	LastResponder *LastResponder
}

// NewDispatcher returns a Dispatcher wired to the given config, registry, and
// logger. It panics on nil cfg or nil logger so configuration mistakes fail
// loudly at startup.
func NewDispatcher(
	cfg *config.Config,
	registry *Registry,
	logger *slog.Logger,
	verboseErrors bool,
	fallbackTimeoutMultiplier int,
	streamTimeout time.Duration,
) *Dispatcher {
	if cfg == nil {
		panic("proxy: nil config")
	}
	if logger == nil {
		panic("proxy: nil logger")
	}
	if registry == nil {
		panic("proxy: nil registry")
	}
	if fallbackTimeoutMultiplier < 1 {
		fallbackTimeoutMultiplier = 2
	}
	if streamTimeout <= 0 {
		streamTimeout = 5 * time.Minute
	}
	d := &Dispatcher{
		Cfg:                       cfg,
		Logger:                    logger.With("component", "proxy"),
		Registry:                  registry,
		fallbackTimeoutMultiplier: fallbackTimeoutMultiplier,
		streamTimeout:             streamTimeout,
	}
	d.verboseErrors.Store(verboseErrors)
	return d
}

// VerboseErrors reports the current verbose-errors flag value. Safe for
// concurrent callers — uses an atomic load.
func (d *Dispatcher) VerboseErrors() bool { return d.verboseErrors.Load() }

// SetVerboseErrors toggles the verbose-errors flag atomically. Exposed as a
// setter method (rather than a public field) so callers can't bypass the
// atomic load/store.
func (d *Dispatcher) SetVerboseErrors(v bool) { d.verboseErrors.Store(v) }

// resolveMapping looks up the mapping and provider for the given model name,
// holding the config read lock for the duration of both map accesses. Returns
// (name, mapping, provider, mapped, providerFound):
//   - mapped is true when a mapping was found (the model name matched).
//   - providerFound is true when the mapping's referenced Provider is also
//     registered in this build. When mapped && !providerFound, provider is
//     the zero value and the caller should surface provider_not_registered.
//
// The returned name is the mapping key actually matched — may differ from
// `model` when the family fallback fires.
//
// The returned Provider is a value copy of the map entry. Go maps store
// values by value, so the copy is independent of the underlying map and
// remains valid after the lock is released. Returning a value (rather than
// a *Provider pointer to the copy) makes the read-only contract explicit and
// prevents callers from accidentally mutating a Provider that looks shared.
// Treat the returned Provider as read-only — mutating it does not affect the
// original map and would race with other writers.
func (d *Dispatcher) resolveMapping(model string) (string, config.Mapping, config.Provider, bool, bool) {
	d.Cfg.RLock()
	defer d.Cfg.RUnlock()
	if mapping, ok := d.Cfg.Mappings[model]; ok {
		provider, pok := d.Cfg.Providers[mapping.ProviderName]
		return model, mapping, provider, true, pok
	}
	if family, found := ExtractFamily(model); found {
		if mapping, ok := d.Cfg.Mappings[family]; ok {
			provider, pok := d.Cfg.Providers[mapping.ProviderName]
			return family, mapping, provider, true, pok
		}
	}
	return "", config.Mapping{}, config.Provider{}, false, false
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.writeErrorJSON(
			w,
			r,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"method not allowed (only POST is accepted)",
		)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		d.writeErrorJSON(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"unsupported_content_type",
			"unsupported content type, expected application/json",
		)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer func() { _ = r.Body.Close() }()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			d.writeErrorJSON(
				w,
				r,
				http.StatusRequestEntityTooLarge,
				"body_too_large",
				fmt.Sprintf("request body too large (limit: %d bytes)", mbe.Limit),
			)
			return
		}
		d.writeErrorJSON(
			w,
			r,
			http.StatusBadRequest,
			"body_unreadable",
			fmt.Sprintf("request body unreadable: %v", err),
		)
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
		d.writeErrorJSON(
			w,
			r,
			http.StatusBadRequest,
			"invalid_json",
			fmt.Sprintf("invalid request body: %v", err),
		)
		return
	}

	if req.Model == "" {
		d.writeErrorJSON(
			w,
			r,
			http.StatusBadRequest,
			"missing_model",
			"invalid request body: missing or empty \"model\" field",
		)
		return
	}

	mappingName, mapping, provider, mapped, providerFound := d.resolveMapping(req.Model)
	if !mapped {
		d.Logger.Debug(
			"no match for model",
			"request_id",
			RequestIDFromContext(r.Context()),
			"model",
			req.Model,
		)
		d.writeErrorJSON(
			w,
			r,
			http.StatusNotFound,
			"no_match",
			fmt.Sprintf("no configured mapping for model %q", req.Model),
		)
		return
	}
	if !providerFound {
		d.Logger.Error(
			"provider not registered",
			"request_id",
			RequestIDFromContext(r.Context()),
			"provider",
			mapping.ProviderName,
		)
		d.writeErrorJSON(
			w,
			r,
			http.StatusInternalServerError,
			"provider_not_registered",
			fmt.Sprintf(
				"provider %q is not registered in this freedius build",
				mapping.ProviderName,
			),
		)
		return
	}

	d.Logger.Debug(
		"dispatch",
		"request_id",
		RequestIDFromContext(r.Context()),
		"model",
		req.Model,
		"provider",
		mapping.ProviderName,
		"target_model",
		mapping.ModelString,
	)
	w.Header().Set("X-Freedius-Matched-Provider", mapping.ProviderName)
	w.Header().Set("X-Freedius-Matched-Model", mapping.ModelString)
	if isCountTokensPath(r.URL.Path) && !provider.SupportsCountTokens {
		d.serveLocalCountTokens(w, r, mapping, body)
		return
	}

	// Build fallback chain: primary first, then fallback entries.
	chain := make([]config.Mapping, 0, 1+len(mapping.Fallback))
	chain = append(chain, mapping)
	chain = append(chain, mapping.Fallback...)

	// Shared timeout budget for the entire fallback chain.
	chainTimeout := time.Duration(float64(d.fallbackTimeoutMultiplier)) * d.streamTimeout
	chainCtx, chainCancel := context.WithTimeout(r.Context(), chainTimeout)
	defer chainCancel()

	var attempts []fallbackAttempt
	for i, target := range chain {
		// Resolve provider for this target.
		d.Cfg.RLock()
		p, providerExists := d.Cfg.Providers[target.ProviderName]
		d.Cfg.RUnlock()
		if !providerExists {
			attempts = append(attempts, fallbackAttempt{
				provider: target.ProviderName,
				model:    target.ModelString,
				errType:  "provider_not_registered",
				status:   http.StatusInternalServerError,
				message:  fmt.Sprintf("provider %q is not registered in this freedius build", target.ProviderName),
			})
			d.Logger.Warn(
				"fallback: provider not registered",
				"request_id", RequestIDFromContext(r.Context()),
				"attempt", i,
				"provider", target.ProviderName,
				"model", target.ModelString,
			)
			continue
		}

		adapter, ok := d.Registry.Lookup(p.Behavior)
		if !ok {
			attempts = append(attempts, fallbackAttempt{
				provider: target.ProviderName,
				model:    target.ModelString,
				errType:  "provider_not_registered",
				status:   http.StatusInternalServerError,
				message:  fmt.Sprintf("behavior %q is not registered in this freedius build", p.Behavior),
			})
			d.Logger.Warn(
				"fallback: behavior not registered",
				"request_id", RequestIDFromContext(r.Context()),
				"attempt", i,
				"provider", target.ProviderName,
				"behavior", p.Behavior,
			)
			continue
		}

		ww := &wroteHeaderResponseWriter{ResponseWriter: w}
		attemptReq := r.WithContext(chainCtx)
		attemptReq.Body = io.NopCloser(bytes.NewReader(body))
		err := adapter.Handle(ww, attemptReq, p, target, body)

		if err == nil || ww.wroteHeader {
			// Success — response is being written (or already written).
			if i > 0 {
				d.Logger.Info(
					"fallback succeeded",
					"request_id", RequestIDFromContext(r.Context()),
					"attempt", i,
					"provider", target.ProviderName,
					"model", target.ModelString,
				)
				if d.LastResponder != nil && mappingName != "" {
					d.LastResponder.Record(mappingName, i)
				}
			}
			return
		}

		// Pre-WriteHeader error — record and try next entry.
		var errType, message string
		var status int
		var retryAfter int

		var ue *upstreamError
		var ce *configError
		switch {
		case errors.As(err, &ue):
			status = ue.status
			errType = ue.errType
			message = ue.message
			retryAfter = ue.retryAfter
		case errors.As(err, &ce):
			status = 500
			errType = ce.errType
			message = ce.Error()
		default:
			status = 529
			errType = "overloaded_error"
			message = "upstream provider not reachable"
			retryAfter = 15
		}

		attempts = append(attempts, fallbackAttempt{
			provider:   target.ProviderName,
			model:      target.ModelString,
			errType:    errType,
			message:    message,
			status:     status,
			retryAfter: retryAfter,
		})

		d.Logger.Warn(
			"fallback: attempt failed",
			"request_id", RequestIDFromContext(r.Context()),
			"attempt", i,
			"provider", target.ProviderName,
			"model", target.ModelString,
			"status", status,
			"err_type", errType,
		)
	}

	// All entries exhausted.
	if len(attempts) > 0 {
		last := attempts[len(attempts)-1]
		switch {
		case len(chain) > 1:
			d.writeAggregatedFallbackError(w, r, attempts, last.status, last.errType, last.retryAfter)
		case last.errType == "provider_not_registered":
			d.writeErrorJSON(w, r, last.status, last.errType, last.message)
		default:
			writeAnthropicError(w, last.status, last.errType, last.message, last.retryAfter)
		}
	}
}

// fallbackAttempt records one entry in a fallback chain for aggregation and logging.
type fallbackAttempt struct {
	provider   string
	model      string
	errType    string
	message    string
	status     int
	retryAfter int
}

// writeAggregatedFallbackError writes one Anthropic-shaped error response
// summarizing every failed attempt in the fallback chain.
func (d *Dispatcher) writeAggregatedFallbackError(
	w http.ResponseWriter,
	r *http.Request,
	attempts []fallbackAttempt,
	status int,
	errType string,
	retryAfter int,
) {
	var parts []string
	for _, a := range attempts {
		parts = append(parts, fmt.Sprintf("%s/%s (%s)", a.provider, a.model, a.errType))
	}
	msg := fmt.Sprintf("all providers failed: %s", strings.Join(parts, ", "))

	// Server log gets full details; client message redacts error types.
	var clientParts []string
	for _, a := range attempts {
		clientParts = append(clientParts, fmt.Sprintf("%s/%s", a.provider, a.model))
	}
	clientMsg := fmt.Sprintf("all providers failed: %s", strings.Join(clientParts, ", "))

	d.Logger.Error(
		"fallback chain exhausted",
		"request_id", RequestIDFromContext(r.Context()),
		"attempts", len(attempts),
		"message", msg,
	)

	writeAnthropicError(w, status, errType, clientMsg, retryAfter)
}

// ErrorOption mutates the internal errorJSON used by (*Dispatcher).writeErrorJSON.
type ErrorOption func(*errorJSON)

type errorJSON struct {
	detail string
}

// WithDetail returns an ErrorOption that sets the optional "detail" field on
// the JSON error envelope (included only when VerboseErrors is true).
func WithDetail(detail string) ErrorOption {
	return func(e *errorJSON) { e.detail = detail }
}

func (d *Dispatcher) writeErrorJSON(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code, message string,
	opts ...ErrorOption,
) {
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
	if e.detail != "" && d.verboseErrors.Load() {
		body["detail"] = e.detail
	}
	w.Header().Set("X-Freedius-Error-Type", code)
	w.Header().Set("X-Freedius-Error-Message", message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		d.Logger.Error(
			"response encode failed",
			"request_id",
			RequestIDFromContext(r.Context()),
			"err",
			err,
		)
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

// RequestIDFromContext returns the request ID stored in ctx, or "" if none
// was set (i.e. the request did not pass through RequestIDMiddleware).
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware assigns a fresh request ID to every incoming request,
// propagates it via the X-Freedius-Request-ID response header and via
// r.Context(), and then delegates to next.
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

// RecoverMiddleware catches panics from downstream handlers, logs them with
// the request's ID, and (if no response has been written yet) replaces the
// in-flight response with a 500 JSON error envelope.
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
			logger.Error(
				"panic recovered",
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

// AccessLogMiddleware writes one structured log line per request with the
// request ID, matched provider/model, status code, and duration. It does not
// log request or response bodies (see the privacy note at the top of this file).
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
		logger.Info(
			"request complete",
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

// --- Event bus middleware ---

// EventBusMiddleware emits a RequestEvent to the optional event bus after each
// request completes. When bus is nil, it passes through as a no-op.
func EventBusMiddleware(bus *EventBus, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bus == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		modelName := extractModelFromBody(r)
		ww := &wroteHeaderResponseWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)
		id := RequestIDFromContext(r.Context())
		matchedProvider := ww.Header().Get("X-Freedius-Matched-Provider")
		matchedModel := ww.Header().Get("X-Freedius-Matched-Model")
		status := ww.code
		if status == 0 {
			status = http.StatusOK
		}
		ev := RequestEvent{
			RequestID:       id,
			Method:          r.Method,
			Path:            r.URL.Path,
			Model:           modelName,
			Provider:        matchedProvider,
			Status:          status,
			Latency:         time.Since(start),
			MatchedProvider: matchedProvider,
			MatchedModel:    matchedModel,
		}
		if status >= 400 {
			ev.ErrorType = ww.Header().Get("X-Freedius-Error-Type")
			ev.ErrorMessage = ww.Header().Get("X-Freedius-Error-Message")
		}
		bus.Emit(ev)
	})
}

// extractModelFromBody reads the request body, extracts the "model" field from
// the JSON payload, and recreates the body for downstream handlers. On a
// read error, the body is left in its post-error state (closed, not re-seated)
// so the dispatcher's own io.ReadAll returns the original error and produces
// a "body_unreadable" 400 — the previous behavior set r.Body = http.NoBody
// which masked the real failure as a misleading "empty body" error.
func extractModelFromBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		// Don't re-seat the body; the dispatcher's read will return the
		// same error and surface it correctly.
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return ""
	}
	return body.Model
}
