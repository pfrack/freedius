package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

func newOpenAIAdapterForTest(t *testing.T) *OpenAICompatibleAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOpenAICompatibleAdapterWithTimeout(logger, 5*time.Minute)
}

func openAIAdapterCaptureUpstream(t *testing.T, capture *[]byte, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*capture = body
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestOpenAIAdapter_ConfigNoStreamUsageOmitsStreamOptions(t *testing.T) {
	tests := []struct {
		name   string
		openAI *config.OpenAIOptions
		apiEnv string
		apiKey string
	}{
		{
			name:   "google",
			openAI: &config.OpenAIOptions{NoStreamUsage: true},
			apiEnv: "GEMINI_API_KEY",
			apiKey: "sk-test",
		},
		{
			name:   "ollama",
			openAI: &config.OpenAIOptions{NoStreamUsage: true},
			apiEnv: "OLLAMA_API_KEY",
			apiKey: "sk-test",
		},
		{
			name:   "lmstudio",
			openAI: &config.OpenAIOptions{NoStreamUsage: true},
			apiEnv: "LMSTUDIO_API_KEY",
			apiKey: "sk-test",
		},
		{
			name:   "nim",
			openAI: &config.OpenAIOptions{NoStreamUsage: true, PreSendHook: "sanitizeNIMBody"},
			apiEnv: "NVIDIA_NIM_API_KEY",
			apiKey: "sk-test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.apiEnv, tt.apiKey)
			var capturedBody []byte
			var mu sync.Mutex
			upstream := openAIAdapterCaptureUpstream(t, &capturedBody, &mu)
			defer upstream.Close()

			a := newOpenAIAdapterForTest(t)
			rec := httptest.NewRecorder()
			body := []byte(
				`{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			err := a.Handle(
				rec,
				req,
				config.Provider{
					Behavior:         "openai",
					DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
					DefaultAPIKeyEnv: tt.apiEnv,
					OpenAI:           tt.openAI,
				},
				config.Mapping{ProviderName: tt.name, ModelString: "m"},
				body,
			)
			if err != nil {
				t.Fatalf("Handle returned err: %v", err)
			}

			mu.Lock()
			upstreamBody := append([]byte{}, capturedBody...)
			mu.Unlock()

			var got map[string]any
			if err := json.Unmarshal(upstreamBody, &got); err != nil {
				t.Fatalf("upstream body not JSON: %v\n%s", err, string(upstreamBody))
			}
			if _, ok := got["stream_options"]; ok {
				t.Errorf(
					"%s should not receive stream_options (NoStreamUsage=true), got %v",
					tt.name, got["stream_options"],
				)
			}
		})
	}
}

func TestOpenAIAdapter_ConfigStreamUsageIncludesStreamOptions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	var capturedBody []byte
	var mu sync.Mutex
	upstream := openAIAdapterCaptureUpstream(t, &capturedBody, &mu)
	defer upstream.Close()

	a := newOpenAIAdapterForTest(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	// OpenAI options omitted (nil) => default NoStreamUsage=false => stream_options sent.
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "m"},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}

	mu.Lock()
	upstreamBody := append([]byte{}, capturedBody...)
	mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal(upstreamBody, &got); err != nil {
		t.Fatalf("upstream body not JSON: %v\n%s", err, string(upstreamBody))
	}
	if _, ok := got["stream_options"]; !ok {
		t.Errorf("expected stream_options to be present when NoStreamUsage=false, got body:\n%s", string(upstreamBody))
	}
}

func TestOpenAIAdapter_UnknownPreSendHook_Errors(t *testing.T) {
	a := newOpenAIAdapterForTest(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: "https://example.com/v1/chat/completions",
			OpenAI:         &config.OpenAIOptions{PreSendHook: "does-not-exist"},
		},
		config.Mapping{ProviderName: "openai", ModelString: "m"},
		body,
	)
	if err == nil {
		t.Fatal("expected error for unknown pre_send_hook")
	}
	ce, ok := err.(*configError)
	if !ok {
		t.Fatalf("expected *configError, got %T: %v", err, err)
	}
	if ce.errType != "invalid_request_error" {
		t.Errorf("errType: got %q, want invalid_request_error", ce.errType)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention the unknown hook name: %v", err)
	}
}
