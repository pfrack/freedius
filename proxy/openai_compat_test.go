package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newOpenAICompatibleAdapter(t *testing.T) *OpenAICompatibleAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOpenAICompatibleAdapter(logger)
}

func TestOpenAICompat_Upstream401(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer upstream.Close()

	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "openai",
			Model:     "gpt-4",
			BaseURL:   upstream.URL,
			APIKeyEnv: "OPENAI_API_KEY",
		},
		[]byte(`{}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestOpenAICompat_MissingEnvVar(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "openai",
			Model:     "gpt-4",
			BaseURL:   "https://x",
			APIKeyEnv: "OPENAI_API_KEY",
		},
		[]byte(`{}`),
	)
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
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "openai",
			Model:     "gpt-4",
			BaseURL:   upstream.URL,
			APIKeyEnv: "OPENAI_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
}
