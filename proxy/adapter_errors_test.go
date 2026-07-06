package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

// preWriteHeaderErrProvider returns a fixed error from Handle() WITHOUT writing
// any response headers — simulates pre-WriteHeader adapter failure so the
// dispatcher forwards the error in the unified error JSON body.
type preWriteHeaderErrProvider struct {
	err error
}

func (s *preWriteHeaderErrProvider) Handle(
	_ http.ResponseWriter,
	_ *http.Request,
	_ config.Provider,
	_ config.Mapping,
	_ []byte,
) error {
	return s.err
}

func TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "nim", ModelString: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("without verbose-errors detail is omitted", func(t *testing.T) {
		registry := NewRegistry(map[string]Provider{
			"openai": &preWriteHeaderErrProvider{err: errors.New("upstream connection refused")},
		})
		d := NewDispatcher(cfg, registry, logger, false, 2, 5*time.Minute)
		handler := RequestIDMiddleware(d)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude-opus-4"}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)

		if rec.Code != 529 {
			t.Fatalf("status: got %d, want 529", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["type"] != "error" {
			t.Errorf("type: got %v, want error", body["type"])
		}
		inner := body["error"].(map[string]any)
		if inner["type"] != "overloaded_error" {
			t.Errorf("error.type: got %v, want overloaded_error", inner["type"])
		}
		if got := rec.Header().Get("retry-after"); got != "15" {
			t.Errorf("retry-after: got %q, want 15", got)
		}
		if got := rec.Header().Get("x-should-retry"); got != "true" {
			t.Errorf("x-should-retry: got %q, want true", got)
		}
	})

	t.Run("with verbose-errors same Anthropic shape", func(t *testing.T) {
		registry := NewRegistry(map[string]Provider{
			"openai": &preWriteHeaderErrProvider{err: errors.New("upstream connection refused")},
		})
		d := NewDispatcher(cfg, registry, logger, true, 2, 5*time.Minute)
		handler := RequestIDMiddleware(d)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude-opus-4"}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)

		if rec.Code != 529 {
			t.Fatalf("status: got %d, want 529", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		inner := body["error"].(map[string]any)
		if inner["type"] != "overloaded_error" {
			t.Errorf("error.type: got %v, want overloaded_error", inner["type"])
		}
	})
}

func TestFreediusErrorHandler_AnthropicFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	// Inject a request_id so we can verify context is handled.
	ctx := context.WithValue(req.Context(), requestIDKey, "abc123")
	handler(rec, req.WithContext(ctx), errors.New("dial tcp: connection refused"))

	if rec.Code != 529 {
		t.Fatalf("status: got %d, want 529", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "error" {
		t.Errorf("type: got %v, want error", body["type"])
	}
	inner := body["error"].(map[string]any)
	if inner["type"] != "overloaded_error" {
		t.Errorf("error.type: got %v, want overloaded_error", inner["type"])
	}
	if inner["message"] != "upstream not reachable" {
		t.Errorf("error.message: got %v, want upstream not reachable", inner["message"])
	}
	if got := rec.Header().Get("retry-after"); got != "15" {
		t.Errorf("retry-after: got %q, want 15", got)
	}
}

func TestAdapter_ErrorTemplate_UsesProviderName(t *testing.T) {
	tests := []struct {
		name         string
		provider     config.Provider
		mapping      config.Mapping
		apiKeyEnv    string
		envValue     string
		adapterCtor  func(logger *slog.Logger) Provider
		wantContains []string
	}{
		{
			name: "nim via openai-compat names provider nim",
			provider: config.Provider{
				Behavior:         "openai",
				DefaultBaseURL:   "https://x/v1/chat/completions",
				DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY",
			},
			mapping:   config.Mapping{ProviderName: "nim", ModelString: "x"},
			apiKeyEnv: "NVIDIA_NIM_API_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewOpenAICompatibleAdapter(l)
			},
			wantContains: []string{"nim adapter (openai-compat)", "NVIDIA_NIM_API_KEY"},
		},
		{
			name: "custom via anthropic-compat names provider custom",
			provider: config.Provider{
				Behavior:         "anthropic",
				DefaultBaseURL:   "https://x",
				DefaultAPIKeyEnv: "CUSTOM_KEY",
			},
			mapping:   config.Mapping{ProviderName: "custom", ModelString: "x"},
			apiKeyEnv: "CUSTOM_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewAnthropicCompatibleAdapter(l, false)
			},
			wantContains: []string{"custom adapter (anthropic-compat)", "CUSTOM_KEY"},
		},
		{
			name: "zen via mix/anthropic-compat names provider zen",
			provider: config.Provider{
				Behavior:         "mix",
				DefaultBaseURL:   "https://x/v1/messages",
				DefaultAPIKeyEnv: "OPENCODE_API_KEY",
			},
			mapping:   config.Mapping{ProviderName: "zen", ModelString: "x"},
			apiKeyEnv: "OPENCODE_API_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewMixAdapter(l, false, 5*time.Minute)
			},
			wantContains: []string{"zen adapter (anthropic-compat)", "OPENCODE_API_KEY"},
		},
		{
			name: "go via openai-compat names provider go",
			provider: config.Provider{
				Behavior:         "openai",
				DefaultBaseURL:   "https://x/v1/chat/completions",
				DefaultAPIKeyEnv: "OPENAI_API_KEY",
			},
			mapping:   config.Mapping{ProviderName: "go", ModelString: "x"},
			apiKeyEnv: "OPENAI_API_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewOpenAICompatibleAdapter(l)
			},
			wantContains: []string{"go adapter (openai-compat)", "OPENAI_API_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.apiKeyEnv, tt.envValue)
			a := tt.adapterCtor(slog.New(slog.NewTextHandler(io.Discard, nil)))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
			err := a.Handle(rec, req, tt.provider, tt.mapping, []byte("{}"))
			if err == nil {
				t.Fatal("expected error")
			}
			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			// Anti-regression: never leak the inner adapter name only.
			for _, leak := range []string{"openai adapter:", "anthropic adapter:"} {
				if strings.Contains(msg, leak) {
					t.Errorf("error %q leaks inner adapter name %q", msg, leak)
				}
			}
		})
	}
}

func TestOpenAICompat_MissingBaseURL_UsesProviderName(t *testing.T) {
	a := NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	err := a.Handle(
		rec,
		req,
		config.Provider{Behavior: "openai"},
		config.Mapping{ProviderName: "go", ModelString: "x"},
		[]byte("{}"),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "go adapter (openai-compat)") {
		t.Errorf("missing base_url error should name provider go, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "missing base_url") {
		t.Errorf("error should mention missing base_url, got %q", err.Error())
	}
}

func TestAnthropicCompat_MissingBaseURL_UsesProviderName(t *testing.T) {
	a := NewAnthropicCompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	err := a.Handle(
		rec,
		req,
		config.Provider{Behavior: "anthropic"},
		config.Mapping{ProviderName: "custom", ModelString: "x"},
		[]byte("{}"),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "custom adapter (anthropic-compat)") {
		t.Errorf("missing base_url error should name provider custom, got %q", err.Error())
	}
}

func TestOpenAICompat_StreamTimeout_Honored(t *testing.T) {
	// Stub upstream that hangs for 5 seconds.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			return
		}
	}))
	defer upstream.Close()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewOpenAICompatibleAdapterWithTimeout(logger, 100*time.Millisecond)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))

	start := time.Now()
	err := a.Handle(rec, req, config.Provider{
		Behavior:         "openai",
		DefaultBaseURL:   upstream.URL,
		DefaultAPIKeyEnv: "OPENAI_API_KEY",
	}, config.Mapping{ProviderName: "openai", ModelString: "x"}, []byte(`{}`))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context-deadline error")
	}
	// Pre-WriteHeader error from stream timeout must reach the dispatcher.
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "context canceled") {
		t.Errorf("error should reflect context deadline, got %q", err.Error())
	}
	// Should have bailed out near the timeout, NOT after 5s.
	if elapsed > 2*time.Second {
		t.Errorf("stream timeout not honored; elapsed=%v, want <2s", elapsed)
	}
}

func TestMixAdapter_RoutingDebugLog(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mix := NewMixAdapter(logger, false, 5*time.Minute)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")

	t.Run("anthropic-format base_url routes to anthropic", func(t *testing.T) {
		logBuf.Reset()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/messages",
			strings.NewReader(`{"stream":false}`),
		)
		mix.Handle(rec, req, config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/messages",
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		}, config.Mapping{ProviderName: "mix", ModelString: "x"}, []byte(`{}`))
		if !strings.Contains(logBuf.String(), `selected=anthropic`) {
			t.Errorf("expected 'selected=anthropic' log, got: %s", logBuf.String())
		}
	})

	t.Run("openai-format base_url routes to openai", func(t *testing.T) {
		logBuf.Reset()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
		mix.Handle(rec, req, config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		}, config.Mapping{ProviderName: "mix", ModelString: "x"}, []byte(`{}`))
		if !strings.Contains(logBuf.String(), `selected=openai`) {
			t.Errorf("expected 'selected=openai' log, got: %s", logBuf.String())
		}
	})
}

func TestDispatcher_ConfigError_Returns500(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "nim", ModelString: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{
		"openai": &preWriteHeaderErrProvider{
			err: &configError{err: errors.New("missing API key"), errType: "authentication_error"},
		},
	})
	d := NewDispatcher(cfg, registry, logger, false, 2, 5*time.Minute)
	handler := RequestIDMiddleware(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "error" {
		t.Errorf("type: got %v, want error", body["type"])
	}
	inner := body["error"].(map[string]any)
	if inner["type"] != "authentication_error" {
		t.Errorf("error.type: got %v, want authentication_error", inner["type"])
	}
	if !strings.Contains(inner["message"].(string), "missing API key") {
		t.Errorf("message should contain 'missing API key', got %v", inner["message"])
	}
	if got := rec.Header().Get("retry-after"); got != "" {
		t.Errorf("retry-after should be absent, got %q", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "" {
		t.Errorf("x-should-retry should be absent, got %q", got)
	}
}

func TestDispatcher_ConfigError_InvalidRequest(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "nim", ModelString: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{
		"openai": &preWriteHeaderErrProvider{
			err: &configError{err: errors.New("bad base_url"), errType: "invalid_request_error"},
		},
	})
	d := NewDispatcher(cfg, registry, logger, false, 2, 5*time.Minute)
	handler := RequestIDMiddleware(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
	if inner["type"] != "invalid_request_error" {
		t.Errorf("error.type: got %v, want invalid_request_error", inner["type"])
	}
	if got := rec.Header().Get("retry-after"); got != "" {
		t.Errorf("retry-after should be absent, got %q", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "" {
		t.Errorf("x-should-retry should be absent, got %q", got)
	}
}

func TestFreediusErrorHandler_DNSError_Returns502(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	dnsErr := &net.DNSError{Err: "no such host", Name: "nonexistent.example", IsNotFound: true}
	handler(rec, req, dnsErr)

	if rec.Code != 502 {
		t.Fatalf("status: got %d, want 502", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "error" {
		t.Errorf("type: got %v, want error", body["type"])
	}
	inner := body["error"].(map[string]any)
	if inner["type"] != "api_error" {
		t.Errorf("error.type: got %v, want api_error", inner["type"])
	}
	if got := rec.Header().Get("retry-after"); got != "" {
		t.Errorf("retry-after should be absent, got %q", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "" {
		t.Errorf("x-should-retry should be absent, got %q", got)
	}
}

func TestFreediusErrorHandler_ConnectionRefused_Returns529(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	handler(rec, req, errors.New("dial tcp: connection refused"))

	if rec.Code != 529 {
		t.Fatalf("status: got %d, want 529", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
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

func TestDispatcher_Upstream500_AnthropicErrorEnvelope(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal failure","detail":"database timeout"}`))
	}))
	defer upstream.Close()

	t.Setenv("TEST_API_KEY", "sk-test")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"test": {
				Behavior:         "openai",
				DefaultBaseURL:   upstream.URL,
				DefaultAPIKeyEnv: "TEST_API_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "test", ModelString: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(logger),
	})
	d := NewDispatcher(cfg, registry, logger, false, 2, 5*time.Minute)
	handler := RequestIDMiddleware(d)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["type"] != "error" {
		t.Errorf("type: got %v, want error", body["type"])
	}
	inner, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field is not a map: %v", body["error"])
	}
	if inner["type"] != "api_error" {
		t.Errorf("error.type: got %v, want api_error", inner["type"])
	}
	msg, ok := inner["message"].(string)
	if !ok {
		t.Fatalf("error.message is not a string: %v", inner["message"])
	}
	if !strings.Contains(msg, "internal failure") {
		t.Errorf("message should contain upstream snippet, got %q", msg)
	}
	if got := rec.Header().Get("retry-after"); got != "15" {
		t.Errorf("retry-after: got %q, want 15", got)
	}
	if got := rec.Header().Get("x-should-retry"); got != "true" {
		t.Errorf("x-should-retry: got %q, want true", got)
	}
}

func TestAdapter_ErrorResponse_NoAPIKeyInLog(t *testing.T) {
	const fakeKey = "sk-test-log-leak-1234567890abcdef1234567890"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Invalid API key provided: ` + fakeKey + `"}`))
	}))
	defer upstream.Close()

	t.Setenv("LEAK_TEST_KEY", "sk-real-key")
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"test": {
				Behavior:         "openai",
				DefaultBaseURL:   upstream.URL,
				DefaultAPIKeyEnv: "LEAK_TEST_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "test", ModelString: "x"},
		},
	}
	registry := NewRegistry(map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(logger),
	})
	d := NewDispatcher(cfg, registry, logger, true, 2, 5*time.Minute)
	handler := RequestIDMiddleware(d)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	logOutput := logBuf.String()
	if strings.Contains(logOutput, fakeKey) {
		t.Errorf("log output should NOT contain API key, got:\n%s", logOutput)
	}
}

// helper used by other tests in this file
var _ = json.Unmarshal
