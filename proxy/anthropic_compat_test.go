package proxy

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newAnthropicCompatAdapter(t *testing.T) *AnthropicCompatibleAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAnthropicCompatibleAdapter(logger, false)
}

func TestAnthropicCompat_PassthroughText(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key: got %q, want sk-test", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf(
				"anthropic-version: got %q, want 2023-06-01",
				r.Header.Get("anthropic-version"),
			)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization should be empty, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"x"}`)),
	)
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAnthropicCompat_Upstream401_ForwardsBody(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer upstream.Close()

	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"x"}`)),
	)
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err == nil {
		t.Fatal("expected upstreamError on 401")
	}
	var ue *upstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *upstreamError, got %T: %v", err, err)
	}
	if ue.status != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", ue.status, http.StatusUnauthorized)
	}
	if ue.errType != "authentication_error" {
		t.Errorf("errType: got %q, want authentication_error", ue.errType)
	}
	if rec.Body.Len() > 0 {
		t.Errorf("expected no bytes written, got body=%q", rec.Body.String())
	}
}

func TestAnthropicCompat_MissingBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{Behavior: "anthropic", DefaultAPIKeyEnv: "ANTHROPIC_API_KEY"},
		config.Mapping{ProviderName: "anthropic", ModelString: "x"},
		[]byte(`{}`),
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicCompat_MissingEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   "https://x",
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "x"},
		[]byte(`{}`),
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicCompat_ModelOverride_NonStreaming(t *testing.T) {
	// Verify model field is rewritten in non-streaming JSON responses
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Response contains upstream model name
		_, _ = w.Write([]byte(`{"model":"deepseek-v3","id":"msg_test","content":[]}`))
	}))
	defer upstream.Close()

	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"claude-opus-4"}`)),
	)
	// Store original model in context as the dispatcher would
	req = req.WithContext(WithRequestModel(req.Context(), "claude-opus-4"))

	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "deepseek-v3"},
		[]byte(`{"model":"claude-opus-4"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	// Verify rewritten body contains original model
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"model":"claude-opus-4"`)) {
		t.Errorf("expected rewritten model in body, got: %q", rec.Body.String())
	}
	// Verify upstream model is NOT in the output
	if bytes.Contains(rec.Body.Bytes(), []byte(`"model":"deepseek-v3"`)) {
		t.Errorf("upstream model should not appear in output, got: %q", rec.Body.String())
	}
}

func TestAnthropicCompat_ModelOverride_Streaming(t *testing.T) {
	// Verify model field is rewritten in streaming SSE responses
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Stream with message_start containing upstream model
		_, _ = w.Write([]byte("event: message_start\n"))
		msgStart := `{"type":"message_start","message":{"id":"msg_test","model":"gpt-4-turbo",` +
			`"type":"message","role":"assistant","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}`
		_, _ = w.Write([]byte("data: " + msgStart))
		_, _ = w.Write([]byte("\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()

	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"claude-opus-4"}`)),
	)
	// Store original model in context
	req = req.WithContext(WithRequestModel(req.Context(), "claude-opus-4"))

	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "gpt-4-turbo"},
		[]byte(`{"model":"claude-opus-4"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	// Verify stream contains rewritten model in message_start
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"model":"claude-opus-4"`)) {
		t.Errorf("expected rewritten model in stream, got: %q", rec.Body.String())
	}
	// Verify upstream model is NOT in the output
	if bytes.Contains(rec.Body.Bytes(), []byte(`"model":"gpt-4-turbo"`)) {
		t.Errorf("upstream model should not appear in stream, got: %q", rec.Body.String())
	}
}

func TestAnthropicCompat_NoModelOverride_Passthrough(t *testing.T) {
	// When no model override in context, upstream model passes through unchanged
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"zen-model","id":"msg_test","content":[]}`))
	}))
	defer upstream.Close()

	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"zen-model"}`)),
	)
	// NO model stored in context (simulating old path)
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "anthropic",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
		},
		config.Mapping{ProviderName: "anthropic", ModelString: "zen-model"},
		[]byte(`{"model":"zen-model"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	// Without override, upstream model passes through
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"model":"zen-model"`)) {
		t.Errorf("expected upstream model passthrough, got: %q", rec.Body.String())
	}
}
