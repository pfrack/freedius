package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestRequestIDMiddleware_GeneratesAndPropagates(t *testing.T) {
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestIDMiddleware(inner)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Freedius-Request-ID")
	if got == "" {
		t.Fatal("expected X-Freedius-Request-ID header")
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{32}$`, got); !matched {
		t.Errorf("expected 32-hex ID, got %q", got)
	}
	if capturedID != got {
		t.Errorf("context ID %q does not match header %q", capturedID, got)
	}
}

func TestRequestIDMiddleware_UniquePerRequest(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/", nil))

	id1 := rec1.Header().Get("X-Freedius-Request-ID")
	id2 := rec2.Header().Get("X-Freedius-Request-ID")
	if id1 == "" || id2 == "" {
		t.Fatal("expected both requests to receive an ID")
	}
	if id1 == id2 {
		t.Errorf("expected unique IDs, got duplicate %q", id1)
	}
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty string for context without ID, got %q", got)
	}
}

func TestRecoverMiddleware_500WithOpaqueBody(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	// Match main.go's wiring: RequestIDMiddleware (outer) → RecoverMiddleware (inner).
	inner := RecoverMiddleware(logger, false, panicHandler)
	handler := RequestIDMiddleware(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "internal_error" {
		t.Errorf("error code: got %q, want internal_error", body["error"])
	}
	if body["message"] == "" {
		t.Error("expected non-empty message")
	}
	// Body must NOT include the panic value or stack trace (privacy).
	for _, k := range []string{"detail", "panic", "stack"} {
		if _, ok := body[k]; ok {
			t.Errorf("opaque body should not contain %q key (privacy)", k)
		}
	}
	// request_id must be present and match the response header (which RequestIDMiddleware
	// set on the wrapper that RecoverMiddleware forwards).
	id := body["request_id"]
	if id == "" {
		t.Fatal("expected request_id in body")
	}
	if headerID := rec.Header().Get("X-Freedius-Request-ID"); headerID != id {
		t.Errorf("body request_id %q does not match header %q", id, headerID)
	}

	// Stack trace must appear in stderr.
	logs := logBuf.String()
	if !strings.Contains(logs, "panic recovered") {
		t.Errorf("expected 'panic recovered' in logs, got: %s", logs)
	}
	if !strings.Contains(logs, "boom") {
		t.Errorf("expected panic value in logs, got: %s", logs)
	}
	if !strings.Contains(logs, "goroutine") {
		t.Errorf("expected stack trace in logs, got: %s", logs)
	}
}

func TestRecoverMiddleware_NoPanicPassesThrough(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(http.StatusTeapot)
	})
	handler := RecoverMiddleware(logger, false, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status: got %d, want 418 (pass-through)", rec.Code)
	}
	if got := rec.Header().Get("X-Test"); got != "ok" {
		t.Errorf("X-Test: got %q, want ok", got)
	}
}

func TestRecoverMiddleware_PostWriteHeaderDoesNotRewrite(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Panic AFTER WriteHeader was called.
		panic("late panic")
	})
	handler := RecoverMiddleware(logger, false, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	// Status must remain 200 — recover middleware must NOT call WriteHeader again.
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (post-WriteHeader panic)", rec.Code)
	}
	// Body should NOT contain the opaque panic response.
	if strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf(
			"body should not contain panic response after headers written, got: %s",
			rec.Body.String(),
		)
	}
}

func TestAccessLogMiddleware_LogsStatusAndDuration(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Freedius-Matched-Provider", "nim")
		w.Header().Set("X-Freedius-Matched-Model", "test-model")
		w.WriteHeader(http.StatusOK)
	})
	handler := AccessLogMiddleware(logger, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))

	logs := logBuf.String()
	for _, want := range []string{
		"request complete",
		"status=200",
		"method=POST",
		"path=/v1/messages",
		"matched_provider=nim",
		"matched_model=test-model",
		"duration_ms=",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("access log missing %q in: %s", want, logs)
		}
	}
}

func TestAccessLogMiddleware_CapturesErrorStatus(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	handler := AccessLogMiddleware(logger, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if !strings.Contains(logBuf.String(), "status=502") {
		t.Errorf("expected status=502 in log, got: %s", logBuf.String())
	}
}

func TestWroteHeaderResponseWriter_TracksFirstWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &wroteHeaderResponseWriter{ResponseWriter: rec}

	if w.wroteHeader {
		t.Fatal("wroteHeader should be false initially")
	}
	w.WriteHeader(http.StatusCreated)
	if !w.wroteHeader || w.code != http.StatusCreated {
		t.Errorf(
			"expected wroteHeader=true, code=201; got wroteHeader=%v code=%d",
			w.wroteHeader,
			w.code,
		)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("underlying recorder: got %d, want 201", rec.Code)
	}
}

func TestWroteHeaderResponseWriter_WriteImplicitlyOpens(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &wroteHeaderResponseWriter{ResponseWriter: rec}

	_, _ = w.Write([]byte("hello"))
	if !w.wroteHeader || w.code != http.StatusOK {
		t.Errorf(
			"Write without WriteHeader should mark wroteHeader=true with code=200; got wroteHeader=%v code=%d",
			w.wroteHeader,
			w.code,
		)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("underlying recorder: got %d, want 200", rec.Code)
	}
}

func TestWroteHeaderResponseWriter_FlushDelegatesToUnderlyingFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &wroteHeaderResponseWriter{ResponseWriter: rec}

	// http.NewResponseController must see Flush on the wrapper.
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		t.Fatalf("rc.Flush() returned error, wrapper hides Flusher: %v", err)
	}
	if !rec.Flushed {
		t.Error("expected recorder.Flushed to be true after rc.Flush()")
	}
}

// streamHandler is a minimal streaming adapter for middleware flush tests.
type streamHandler struct {
	body []byte
}

func (s *streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	for i := 0; i < len(s.body); i++ {
		if _, err := w.Write(s.body[i : i+1]); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func TestRecoverMiddleware_FlushWorksThroughWrapper(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inner := &streamHandler{body: []byte("data: hello\n\n")}
	handler := RecoverMiddleware(logger, false, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !rec.Flushed {
		t.Error("expected response to be flushed")
	}
}

func TestAccessLogMiddleware_FlushWorksThroughWrapper(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inner := &streamHandler{body: []byte("data: hello\n\n")}
	handler := AccessLogMiddleware(logger, inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !rec.Flushed {
		t.Error("expected response to be flushed")
	}
}

func TestMiddlewareChain_FlushWorksThroughBothWrappers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inner := &streamHandler{body: []byte("data: hello\n\n")}
	handler := RecoverMiddleware(logger, false, inner)
	handler = AccessLogMiddleware(logger, handler)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !rec.Flushed {
		t.Error("expected response to be flushed")
	}
}
