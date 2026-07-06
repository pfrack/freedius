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
