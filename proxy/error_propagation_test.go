package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

func TestOpenAICompat_Upstream429_ReturnsAnthropicFormat(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("retry-after", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	a := NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"x","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	err := a.Handle(rec, req, config.Provider{
		Behavior:       "openai",
		DefaultBaseURL: upstream.URL, DefaultAPIKeyEnv: "TEST_API_KEY",
	}, config.Mapping{ProviderName: "nim", ModelString: "x"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != 429 {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("retry-after"); got != "42" {
		t.Errorf("retry-after: got %q, want 42", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "true" {
		t.Errorf("x-should-retry: got %q, want true", got)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	inner := resp["error"].(map[string]any)
	if inner["type"] != "rate_limit_error" {
		t.Errorf("error.type: got %v, want rate_limit_error", inner["type"])
	}
}

func TestOpenAICompat_Timeout_ReturnsAnthropicOverloaded(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test")
	// Bounded wait so the handler returns within a fixed budget even if
	// client-side cancellation never propagates — keeps httptest.Server.Close()
	// from blocking on an active connection.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer upstream.Close()

	a := NewOpenAICompatibleAdapterWithTimeout(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		50*time.Millisecond,
	)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"x","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	// The timeout causes a "do request" error returned to the dispatcher,
	// which translates it to writeAnthropicError(w, 529, "overloaded_error", ...).
	// Coverage for the dispatcher wiring lives in
	// TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded (phase2_test.go);
	// coverage for writeAnthropicError itself lives in TestWriteAnthropicError
	// (errors_test.go). This test only proves the adapter times out and
	// surfaces the error to the caller.
	err := a.Handle(rec, req, config.Provider{
		Behavior:       "openai",
		DefaultBaseURL: upstream.URL, DefaultAPIKeyEnv: "TEST_API_KEY",
	}, config.Mapping{ProviderName: "nim", ModelString: "x"}, body)
	if err == nil {
		t.Fatal("expected error from timeout")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error should mention do request, got: %v", err)
	}
}

func TestAnthropicCompat_TransportError_ReturnsAnthropicOverloaded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx := context.WithValue(req.Context(), requestIDKey, "req-123")
	handler(rec, req.WithContext(ctx), errors.New("dial tcp 10.0.0.1:443: connection refused"))

	if rec.Code != 529 {
		t.Fatalf("status: got %d, want 529", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != "error" {
		t.Errorf("type: got %v, want error", resp["type"])
	}
	inner := resp["error"].(map[string]any)
	if inner["type"] != "overloaded_error" {
		t.Errorf("error.type: got %v, want overloaded_error", inner["type"])
	}
	if got := rec.Header().Get("retry-after"); got != "15" {
		t.Errorf("retry-after: got %q, want 15", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "true" {
		t.Errorf("x-should-retry: got %q, want true", got)
	}
}
