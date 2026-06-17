package proxy

import (
	"bytes"
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
			t.Errorf("anthropic-version: got %q, want 2023-06-01", r.Header.Get("anthropic-version"))
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
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"x"}`)))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", BaseURL: upstream.URL, APIKeyEnv: "ANTHROPIC_API_KEY"}, []byte(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAnthropicCompat_MissingBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", APIKeyEnv: "ANTHROPIC_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicCompat_MissingEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	a := newAnthropicCompatAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", BaseURL: "https://x", APIKeyEnv: "ANTHROPIC_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
