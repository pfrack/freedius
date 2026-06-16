package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

func newTestNIMAdapter(t *testing.T, baseURL, apiKey string) *NIMAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewNIMAdapter(NIMAdapterConfig{BaseURL: baseURL, APIKey: apiKey}, logger)
}

func TestNIMStreamingTextResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth := r.Header.Get("Authorization")
		if gotAuth != "Bearer sk-test-nim" {
			t.Errorf("upstream auth: got %q", gotAuth)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("upstream content-type: got %q", ct)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("upstream accept: got %q", r.Header.Get("Accept"))
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream body not OpenAI format: %v", err)
		}
		if req["model"] == nil {
			t.Errorf("upstream body missing model: %s", body)
		}
		if stream, _ := req["stream"].(bool); !stream {
			t.Errorf("upstream body stream: got false, want true")
		}
		so, _ := req["stream_options"].(map[string]any)
		if so == nil || so["include_usage"] != true {
			t.Errorf("upstream body stream_options.include_usage: not set")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test-nim")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "meta/llama"}

	if err := adapter.Handle(w, req, m, []byte(`{"model":"claude-opus-4","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Errorf("missing message_start: %s", body)
	}
	if !strings.Contains(body, `"text_delta","text":"hello"`) {
		t.Errorf("missing text_delta: %s", body)
	}
	if !strings.Contains(body, `"stop_reason":"end_turn"`) {
		t.Errorf("missing stop_reason: %s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Errorf("missing message_stop: %s", body)
	}
}

func TestNIMStreamingToolUse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test-nim")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"c","max_tokens":10,"messages":[{"role":"user","content":"weather?"}]}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	body := []byte(`{"model":"c","max_tokens":10,"messages":[{"role":"user","content":"weather?"}]}`)
	if err := adapter.Handle(w, req, m, body); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, `"type":"tool_use"`) {
		t.Errorf("missing tool_use: %s", respBody)
	}
	if !strings.Contains(respBody, `"name":"get_weather"`) {
		t.Errorf("missing tool name: %s", respBody)
	}
	if !strings.Contains(respBody, `"partial_json":"{\"city\":\"Paris\"}"`) {
		t.Errorf("missing partial_json: %s", respBody)
	}
	if !strings.Contains(respBody, `"stop_reason":"tool_use"`) {
		t.Errorf("missing tool_use stop_reason: %s", respBody)
	}
}

func TestNIMParallelToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"a","arguments":""}},{"index":1,"id":"call_2","type":"function","function":{"name":"b","arguments":""}}]},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test-nim")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"name":"a"`) {
		t.Errorf("missing tool a: %s", body)
	}
	if !strings.Contains(body, `"name":"b"`) {
		t.Errorf("missing tool b: %s", body)
	}
	if strings.Count(body, "event: content_block_start") != 2 {
		t.Errorf("expected 2 content_block_start events, got %d in: %s", strings.Count(body, "event: content_block_start"), body)
	}
}

func TestNIMUpstream401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-wrong")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"invalid api key"`) {
		t.Errorf("body: got %q", w.Body.String())
	}
}

func TestNIMUpstream429(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After: got %q", w.Header().Get("Retry-After"))
	}
	if !strings.Contains(w.Body.String(), `"rate limited"`) {
		t.Errorf("body: got %q", w.Body.String())
	}
}

func TestNIMNonStreamingTextResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}

	if err := adapter.Handle(w, req, m, []byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json (upstream-passthrough)", got)
	}
	if !strings.Contains(w.Body.String(), `"content":"hi"`) {
		t.Errorf("body: got %q, want upstream body verbatim", w.Body.String())
	}
}

func TestNIMTransportError(t *testing.T) {
	adapter := newTestNIMAdapter(t, "http://127.0.0.1:1", "sk-test")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	err := adapter.Handle(w, req, m, []byte(`{}`))
	if err != nil {
		t.Fatalf("expected nil (adapter writes 502 envelope directly), got %v", err)
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", w.Code)
	}
	if !strings.Contains(w.Body.String(), "upstream_unreachable") {
		t.Errorf("body: got %q, want upstream_unreachable envelope", w.Body.String())
	}
}

func TestNIMClientDisconnectMidStream(t *testing.T) {
	var upstreamStarted int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&upstreamStarted, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test")
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = adapter.Handle(w, req, m, []byte(`{}`))

	if atomic.LoadInt32(&upstreamStarted) != 1 {
		t.Error("upstream was not reached")
	}
}

func TestNIMStreamErrorAfterWriteHeaderReturnsNil(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {this is not valid json\n\n"))
	}))
	defer upstream.Close()

	adapter := newTestNIMAdapter(t, upstream.URL, "sk-test")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}

	err := adapter.Handle(w, req, m, []byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("expected nil after WriteHeader (Provider contract), got %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Errorf("expected streamed event: message_start in body, got %q", body)
	}
	if strings.Contains(body, `"error"`) {
		t.Errorf("response body was corrupted by dispatcher writing a 502 JSON envelope after the stream started; body: %q", body)
	}
}

func TestNIMTranslateRequestError(t *testing.T) {
	adapter := newTestNIMAdapter(t, "http://localhost:1", "sk-test")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "nim", Model: "m"}
	err := adapter.Handle(w, req, m, []byte(`not valid json`))
	if err == nil {
		t.Error("expected error on invalid request body, got nil")
	}
	if !strings.Contains(fmt.Sprint(err), "translate request") {
		t.Errorf("error does not mention translation: %v", err)
	}
}
