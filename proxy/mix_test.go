package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
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

func newMixAdapter(t *testing.T) *MixAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewMixAdapter(logger, false, 5*time.Minute)
}

type failingRecorder struct {
	*httptest.ResponseRecorder
	failed bool
}

func (fr *failingRecorder) Write(data []byte) (int, error) {
	if !fr.failed {
		fr.failed = true
		return 0, io.ErrShortWrite
	}
	return fr.ResponseRecorder.Write(data)
}

func TestMixAdapter_AnthropicPassthrough(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
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
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"my-model"}`)),
	)
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/messages",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "my-model"},
		[]byte(`{"model":"my-model"}`),
	)
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
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "my-model"},
		body,
	)
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

func TestMixAdapter_OpenAIPathOmitsStreamOptions(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")

	var capturedBody []byte
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		capturedBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"my-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "my-model"},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
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
			"MixAdapter OpenAI path should not send stream_options (NoStreamUsage=true), got %v",
			got["stream_options"],
		)
	}
}

func TestMixAdapter_Upstream401_AnthropicPath(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/messages",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{}`),
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
	if rec.Body.Len() > 0 {
		t.Errorf("expected no bytes written, got body=%q", rec.Body.String())
	}
}

func TestMixAdapter_Upstream401_OpenAIPath(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{}`),
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
	if rec.Body.Len() > 0 {
		t.Errorf("expected no bytes written, got body=%q", rec.Body.String())
	}
}

func TestMixAdapter_MissingEnvVar(t *testing.T) {
	t.Setenv("MIX_API_KEY", "")
	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   "https://example.com/v1/messages",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{}`),
	)
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
	err := a.Handle(
		rec,
		req,
		config.Provider{Behavior: "mix", DefaultAPIKeyEnv: "MIX_API_KEY"},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{}`),
	)
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestMixAdapter_URLAuthPathSniff(t *testing.T) {
	// Provider-level behavior is "mix"; routing is decided by base_url path
	// suffix ("/v1/messages" → anthropic). This exercises the sniff that
	// replaces the old per-mapping Protocol override.
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key: got %q, want sk-test", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"x"}`)),
	)
	err := a.Handle(rec, req, config.Provider{
		Behavior:         "mix",
		DefaultBaseURL:   upstream.URL + "/v1/messages",
		DefaultAPIKeyEnv: "MIX_API_KEY",
	}, config.Mapping{ProviderName: "mix", ModelString: "x"}, []byte(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMixAdapter_URLOpenAIPathSniff(t *testing.T) {
	// URL path ending in /v1/chat/completions routes to OpenAI sub-adapter,
	// even when the upstream happens to live at a /v1/messages-style host.
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(rec, req, config.Provider{
		Behavior:         "mix",
		DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
		DefaultAPIKeyEnv: "MIX_API_KEY",
	}, config.Mapping{ProviderName: "mix", ModelString: "x"}, body)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "event: message_start") {
		t.Errorf(
			"body should contain Anthropic SSE (translated from OpenAI), got %q",
			rec.Body.String(),
		)
	}
}

func TestMixAdapter_AnthropicPathClientDisconnect(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	rec := &failingRecorder{ResponseRecorder: httptest.NewRecorder(), failed: false}

	a := newMixAdapter(t)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		bytes.NewReader([]byte(`{"model":"x"}`)),
	)
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/messages",
			DefaultAPIKeyEnv: "MIX_API_KEY",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned %v, want nil", err)
	}
}

func TestMixAdapter_ProtocolAnthropicOverridesURL(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key: got %q, want sk-test", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
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
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1",
			DefaultAPIKeyEnv: "MIX_API_KEY",
			Protocol:         "anthropic",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMixAdapter_ProtocolOpenAIOverridesURL(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"x","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1",
			DefaultAPIKeyEnv: "MIX_API_KEY",
			Protocol:         "openai",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "event: message_start") {
		t.Errorf("body should contain Anthropic SSE, got %q", rec.Body.String())
	}
}

func TestMixAdapter_ProtocolAnthropic_URLAlreadyCorrect(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key: got %q, want sk-test", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
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
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/messages",
			DefaultAPIKeyEnv: "MIX_API_KEY",
			Protocol:         "anthropic",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMixAdapter_ProtocolAnthropic_SwapsWrongSuffix(t *testing.T) {
	t.Setenv("MIX_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key: got %q, want sk-test", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newMixAdapter(t)
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
			Behavior:         "mix",
			DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
			DefaultAPIKeyEnv: "MIX_API_KEY",
			Protocol:         "anthropic",
		},
		config.Mapping{ProviderName: "mix", ModelString: "x"},
		[]byte(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	a := newMixAdapter(t)
	tests := []struct {
		name        string
		baseURL     string
		wantSuffix  string
		otherSuffix string
		want        string
	}{
		{
			name:        "appends /messages to /v1",
			baseURL:     "https://api.example.com/v1",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com/v1/messages",
		},
		{
			name:        "appends /chat/completions to /v1",
			baseURL:     "https://api.example.com/v1",
			wantSuffix:  "/chat/completions",
			otherSuffix: "/messages",
			want:        "https://api.example.com/v1/chat/completions",
		},
		{
			name:        "preserves correct suffix",
			baseURL:     "https://api.example.com/v1/messages",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com/v1/messages",
		},
		{
			name:        "swaps wrong suffix",
			baseURL:     "https://api.example.com/v1/chat/completions",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com/v1/messages",
		},
		{
			name:        "appends to bare host",
			baseURL:     "https://api.example.com",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com/messages",
		},
		{
			name:        "handles trailing slash",
			baseURL:     "https://api.example.com/v1/",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com/v1/messages",
		},
		{
			name:        "preserves query and port",
			baseURL:     "https://api.example.com:8080/v1?key=val",
			wantSuffix:  "/messages",
			otherSuffix: "/chat/completions",
			want:        "https://api.example.com:8080/v1/messages?key=val",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.normalizeBaseURL(tt.baseURL, tt.wantSuffix, tt.otherSuffix)
			if got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
