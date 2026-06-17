package proxy

import (
	"bytes"
	"context"
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

func newNIMAdapter(t *testing.T) *NIMAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewNIMAdapter(logger, 5*time.Minute)
}

func TestNIMAdapter_DispatchesToOpenAI(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"meta-llama"`) {
			t.Errorf("upstream should see OpenAI format, got %q", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
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
	if !strings.Contains(rec.Body.String(), "event: content_block_delta") {
		t.Errorf("body should contain content_block_delta, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_stop") {
		t.Errorf("body should contain message_stop, got %q", rec.Body.String())
	}
}

func TestNIMAdapter_OmitsStreamOptionsAndStripsBooleanSchema(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")

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
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"do_thing","description":"x","input_schema":{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":true}}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
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
			"NIM should not receive stream_options (NoStreamUsage=true), got %v",
			got["stream_options"],
		)
	}

	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools: got %v", got["tools"])
	}
	params := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if _, ok := params["additionalProperties"]; ok {
		t.Errorf(
			"expected additionalProperties stripped by sanitize hook, got %v",
			params["additionalProperties"],
		)
	}
}

func TestNIMAdapter_Upstream401_ForwardsVerbatim(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key","detail":"key not found"}`))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "invalid api key") {
		t.Errorf("body should contain upstream error verbatim, got %q", rec.Body.String())
	}
}

func TestNIMAdapter_Upstream429_ForwardsVerbatim(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestNIMAdapter_StreamingToolUse_EmitsContentBlockStart(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"do_thing\",\"arguments\":\"{\\\"x\\\":\"}}]},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"1\\\"}\"}}]},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"do_thing","description":"x","input_schema":{"type":"object","properties":{"x":{"type":"string"}}}}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: content_block_start") {
		t.Errorf("body should contain content_block_start, got %q", out)
	}
	if !strings.Contains(out, "tool_use") {
		t.Errorf("body should contain tool_use content type, got %q", out)
	}
	if !strings.Contains(out, "input_json_delta") {
		t.Errorf("body should contain input_json_delta, got %q", out)
	}
}

func TestNIMAdapter_ParallelToolCalls_EmitsMultipleIndices(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"a\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"b\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"a","description":"x","input_schema":{"type":"object"}},{"name":"b","description":"x","input_schema":{"type":"object"}}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	out := rec.Body.String()
	count := strings.Count(out, "event: content_block_start")
	if count != 2 {
		t.Errorf("expected 2 content_block_start (one per parallel tool), got %d in %q", count, out)
	}
	if !strings.Contains(out, `"name":"a"`) || !strings.Contains(out, `"name":"b"`) {
		t.Errorf("body should contain both tool names, got %q", out)
	}
}

func TestNIMAdapter_NonStreamingResponse_NoOutput(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				`{"id":"x","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`,
			),
		)
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNIMAdapter_ClientCancel_ReturnsError(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"meta-llama\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer upstream.Close()

	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).
		WithContext(ctx)
	go func() {
		<-started
		cancel()
	}()
	_ = a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   upstream.URL + "/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
}

func TestNIMAdapter_TransportError_Returns502(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "sk-test")
	a := newNIMAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Model{
			Provider:  "nim",
			Model:     "meta-llama",
			BaseURL:   "http://127.0.0.1:1/v1/chat/completions",
			APIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		body,
	)
	if err == nil {
		t.Fatal("expected transport error to be returned to dispatcher")
	}
}
