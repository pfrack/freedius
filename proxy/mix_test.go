package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newMixAdapter(t *testing.T) *MixAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewMixAdapter(logger)
}

func TestMixAdapter_AnthropicPassthrough(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
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
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"my-model"`) {
			t.Errorf("upstream should receive original body, got %q", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-model"}`)))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "my-model", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MIX_API_KEY"}, []byte(`{"model":"my-model"}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestMixAdapter_OpenAITranslation(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"my-model"`) {
			t.Errorf("upstream should see OpenAI format, got %q", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "my-model", BaseURL: upstream.URL + "/v1/chat/completions", APIKeyEnv: "MIX_API_KEY"}, body)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_start") {
		t.Errorf("body should contain Anthropic SSE message_start, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_stop") {
		t.Errorf("body should contain message_stop, got %q", rec.Body.String())
	}
}

func TestMixAdapter_Upstream401_AnthropicPath(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MIX_API_KEY"}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestMixAdapter_Upstream401_OpenAIPath(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "x", BaseURL: upstream.URL + "/v1/chat/completions", APIKeyEnv: "MIX_API_KEY"}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestMixAdapter_MissingEnvVar(t *testing.T) {
	t.Setenv("MIX_API_KEY", "")
	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "x", BaseURL: "https://example.com/v1/messages", APIKeyEnv: "MIX_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	if !strings.Contains(err.Error(), "MIX_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestMixAdapter_MissingBaseURL(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "mix", Model: "x", APIKeyEnv: "MIX_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}
