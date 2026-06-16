package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newCustomTestAdapter(t *testing.T) *CustomAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewCustomAdapter(logger)
}

func TestCustomAdapter_PassthroughText(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"my-shim"`) {
			t.Errorf("upstream did not receive original body, got %q", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{"model":"my-shim"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestCustomAdapter_Upstream401(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{"model":"my-shim"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestCustomAdapter_Upstream500(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`oops`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestCustomAdapter_MissingEnvVar(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "")
	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: "https://example.com", APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	if !strings.Contains(err.Error(), "CUSTOM_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestCustomAdapter_MissingBaseURL(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestAnthropicCompat_PassthroughText(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAnthropicCompatibleAdapter(logger)
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAnthropicCompatibleAdapter(logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", APIKeyEnv: "ANTHROPIC_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicCompat_MissingEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAnthropicCompatibleAdapter(logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "anthropic", Model: "x", BaseURL: "https://x", APIKeyEnv: "ANTHROPIC_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNIMAdapter_DispatchesToOpenAI(t *testing.T) {
	t.Setenv("NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"meta-llama"`) {
			t.Errorf("upstream should see OpenAI format, got %q", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewNIMAdapter(logger)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(rec, req, config.Model{Provider: "nim", Model: "meta-llama", BaseURL: upstream.URL + "/v1/chat/completions", APIKeyEnv: "NIM_API_KEY"}, body)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_start") {
		t.Errorf("body should contain Anthropic SSE message_start, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: content_block_delta") {
		t.Errorf("body should contain content_block_delta, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_stop") {
		t.Errorf("body should contain message_stop, got %q", rec.Body.String())
	}
}

func TestOpenAICompat_Upstream401(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer upstream.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewOpenAICompatibleAdapter(logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "openai", Model: "gpt-4", BaseURL: upstream.URL, APIKeyEnv: "OPENAI_API_KEY"}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestOpenAICompat_MissingEnvVar(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewOpenAICompatibleAdapter(logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "openai", Model: "gpt-4", BaseURL: "https://x", APIKeyEnv: "OPENAI_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAICompat_TranslationIncludesStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		if stream, ok := out["stream"].(bool); !ok || !stream {
			t.Errorf("upstream should see stream=true, got %v", out["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewOpenAICompatibleAdapter(logger)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(rec, req, config.Model{Provider: "openai", Model: "gpt-4", BaseURL: upstream.URL, APIKeyEnv: "OPENAI_API_KEY"}, body)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
}
