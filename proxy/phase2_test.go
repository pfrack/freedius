package proxy

import (
	"bytes"
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

// preWriteHeaderErrProvider returns a fixed error from Handle() WITHOUT writing
// any response headers — simulates pre-WriteHeader adapter failure so the
// dispatcher forwards the error in the unified error JSON body.
type preWriteHeaderErrProvider struct {
	err error
}

func (s *preWriteHeaderErrProvider) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	return s.err
}

func TestDispatcher_AdapterError_ForwardedAsUpstreamError(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"claude-opus-4": {Provider: "nim", Model: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("without verbose-errors detail is omitted", func(t *testing.T) {
		registry := NewRegistry(map[string]Provider{
			"nim": &preWriteHeaderErrProvider{err: errors.New("upstream connection refused")},
		})
		d := NewDispatcher(cfg, registry, logger, false)
		handler := RequestIDMiddleware(d)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude-opus-4"}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status: got %d, want 502", rec.Code)
		}
		body := decodeErrorBody(t, rec)
		if body["error"] != "upstream_error" {
			t.Errorf("error code: got %q, want upstream_error", body["error"])
		}
		if body["message"] == "" {
			t.Error("expected message")
		}
		if _, has := body["detail"]; has {
			t.Errorf("detail must be omitted when verboseErrors=false; body=%v", body)
		}
	})

	t.Run("with verbose-errors detail is included", func(t *testing.T) {
		registry := NewRegistry(map[string]Provider{
			"nim": &preWriteHeaderErrProvider{err: errors.New("upstream connection refused")},
		})
		d := NewDispatcher(cfg, registry, logger, true)
		handler := RequestIDMiddleware(d)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude-opus-4"}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status: got %d, want 502", rec.Code)
		}
		body := decodeErrorBody(t, rec)
		if body["detail"] != "upstream connection refused" {
			t.Errorf("detail: got %q, want upstream connection refused", body["detail"])
		}
	})
}

func TestFreediusErrorHandler_UnifiedShape(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	// Inject a request_id so we can verify the body field is populated.
	ctx := context.WithValue(req.Context(), requestIDKey, "abc123")
	handler(rec, req.WithContext(ctx), errors.New("dial tcp: connection refused"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want 502", rec.Code)
	}
	body := decodeErrorBody(t, rec)
	if body["error"] != "upstream_unreachable" {
		t.Errorf("error: got %q, want upstream_unreachable", body["error"])
	}
	if body["message"] != "upstream not reachable" {
		t.Errorf("message: got %q, want upstream not reachable", body["message"])
	}
	if body["detail"] != "dial tcp: connection refused" {
		t.Errorf("detail: got %q, want dial tcp: connection refused", body["detail"])
	}
	if body["request_id"] != "abc123" {
		t.Errorf("request_id: got %q, want abc123", body["request_id"])
	}
}

func TestAdapter_ErrorTemplate_UsesOriginalProvider(t *testing.T) {
	tests := []struct {
		name         string
		model        config.Model
		apiKeyEnv    string
		envValue     string
		adapterCtor  func(logger *slog.Logger) Provider
		wantContains []string
	}{
		{
			name:      "nim via openai-compat names provider nim not openai",
			model:     config.Model{Provider: "openai", Model: "x", BaseURL: "https://x/v1/chat/completions", APIKeyEnv: "NIM_API_KEY", OriginalProvider: "nim"},
			apiKeyEnv: "NIM_API_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewOpenAICompatibleAdapter(l)
			},
			wantContains: []string{"nim adapter (openai-compat)", "NIM_API_KEY"},
		},
		{
			name:      "custom via anthropic-compat names provider custom not anthropic",
			model:     config.Model{Provider: "anthropic", Model: "x", BaseURL: "https://x", APIKeyEnv: "CUSTOM_KEY", OriginalProvider: "custom"},
			apiKeyEnv: "CUSTOM_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewAnthropicCompatibleAdapter(l)
			},
			wantContains: []string{"custom adapter (anthropic-compat)", "CUSTOM_KEY"},
		},
		{
			name:      "zen post-rewrite names provider zen not mix",
			model:     config.Model{Provider: "mix", Model: "x", BaseURL: "https://x/v1/messages", APIKeyEnv: "OPENCODE_API_KEY", OriginalProvider: "zen"},
			apiKeyEnv: "OPENCODE_API_KEY",
			envValue:  "",
			adapterCtor: func(l *slog.Logger) Provider {
				return NewMixAdapter(l)
			},
			wantContains: []string{"zen adapter (anthropic-compat)", "OPENCODE_API_KEY"},
		},
		{
			name:      "go via openai-compat names provider go",
			model:     config.Model{Provider: "openai", Model: "x", BaseURL: "https://x/v1/chat/completions", APIKeyEnv: "OPENAI_API_KEY", OriginalProvider: "go"},
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
			err := a.Handle(rec, req, tt.model, []byte("{}"))
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

func TestOpenAICompat_MissingBaseURL_UsesOriginalProvider(t *testing.T) {
	a := NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	err := a.Handle(rec, req, config.Model{Provider: "openai", Model: "x", OriginalProvider: "go"}, []byte("{}"))
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

func TestAnthropicCompat_MissingBaseURL_UsesOriginalProvider(t *testing.T) {
	a := NewAnthropicCompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", OriginalProvider: "custom"}, []byte("{}"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "custom adapter (anthropic-compat)") {
		t.Errorf("missing base_url error should name provider custom, got %q", err.Error())
	}
}

func TestOpenAICompat_StreamTimeout_Honored(t *testing.T) {
	// Stub upstream that hangs for 5 seconds.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	err := a.Handle(rec, req, config.Model{
		Provider: "openai", Model: "x", BaseURL: upstream.URL, APIKeyEnv: "OPENAI_API_KEY",
	}, []byte(`{}`))
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
	mix := NewMixAdapter(logger)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")

	t.Run("anthropic-format base_url routes to anthropic", func(t *testing.T) {
		logBuf.Reset()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":false}`))
		mix.Handle(rec, req, config.Model{
			Provider: "mix", Model: "x",
			BaseURL: upstream.URL + "/v1/messages",
			APIKeyEnv: "OPENAI_API_KEY",
		}, []byte(`{}`))
		if !strings.Contains(logBuf.String(), `selected=anthropic`) {
			t.Errorf("expected 'selected=anthropic' log, got: %s", logBuf.String())
		}
	})

	t.Run("openai-format base_url routes to openai", func(t *testing.T) {
		logBuf.Reset()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
		mix.Handle(rec, req, config.Model{
			Provider: "mix", Model: "x",
			BaseURL: upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "OPENAI_API_KEY",
		}, []byte(`{}`))
		if !strings.Contains(logBuf.String(), `selected=openai`) {
			t.Errorf("expected 'selected=openai' log, got: %s", logBuf.String())
		}
	})
}

// helper used by other tests in this file
var _ = json.Unmarshal
