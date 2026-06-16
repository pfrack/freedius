package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

func newTestCustomAdapter(t *testing.T) *CustomAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewCustomAdapter(logger)
}

func TestCustomPassthroughTextRequest(t *testing.T) {
	var gotAuth string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Source", "mock")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_x","type":"message","role":"assistant","content":[]}`))
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[]}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "my-sonnet-shim", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}
	body := []byte(`{"model":"claude-sonnet-4","messages":[]}`)

	if err := adapter.Handle(w, req, m, body); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if gotAuth != "Bearer sk-test-custom" {
		t.Errorf("upstream Authorization: got %q", gotAuth)
	}
	if string(gotBody) != `{"model":"claude-sonnet-4","messages":[]}` {
		t.Errorf("upstream body: got %q", gotBody)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if w.Header().Get("X-Upstream-Source") != "mock" {
		t.Errorf("X-Upstream-Source: got %q", w.Header().Get("X-Upstream-Source"))
	}
	if !strings.Contains(w.Body.String(), `"id":"msg_x"`) {
		t.Errorf("body: got %q", w.Body.String())
	}
}

func TestCustomPassthroughStreamingSSE(t *testing.T) {
	chunks := []string{
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}

	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
	for _, c := range chunks {
		if !strings.Contains(w.Body.String(), c) {
			t.Errorf("body missing chunk %q", c)
		}
	}
}

func TestCustomUpstreamError401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}

	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"unauthorized"`) {
		t.Errorf("body: got %q", w.Body.String())
	}
}

func TestCustomUpstreamError500(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}

	if err := adapter.Handle(w, req, m, []byte(`{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"upstream down"`) {
		t.Errorf("body: got %q", w.Body.String())
	}
}

func TestCustomMissingEnvVar(t *testing.T) {
	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "x", BaseURL: "http://localhost:1", APIKeyEnv: "MY_SHIM_API_KEY"}
	err := adapter.Handle(w, req, m, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "MY_SHIM_API_KEY") {
		t.Errorf("error does not mention env var: %v", err)
	}
}

func TestCustomMissingBaseURL(t *testing.T) {
	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	m := config.Model{Provider: "custom", Model: "x", BaseURL: "", APIKeyEnv: "MY_SHIM_API_KEY"}
	err := adapter.Handle(w, req, m, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("error does not mention base_url: %v", err)
	}
}

func TestCustomClientDisconnect(t *testing.T) {
	var upstreamHit int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&upstreamHit, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`)).WithContext(ctx)
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	m := config.Model{Provider: "custom", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}
	_ = adapter.Handle(w, req, m, []byte(`{}`))

	if atomic.LoadInt32(&upstreamHit) != 1 {
		t.Error("upstream was not reached")
	}
	if w.Code == http.StatusBadGateway {
		t.Errorf("expected no 502 on client disconnect, got %d (body: %q)", w.Code, w.Body.String())
	}
}

func TestCustomClientDisconnectNoBodyWritten(t *testing.T) {
	var upstreamHit int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&upstreamHit, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	adapter := newTestCustomAdapter(t)
	t.Setenv("MY_SHIM_API_KEY", "sk-test-custom")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`)).WithContext(ctx)
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	m := config.Model{Provider: "custom", Model: "x", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"}
	err := adapter.Handle(w, req, m, []byte(`{}`))

	if err != nil {
		t.Logf("Handle returned error (acceptable): %v", err)
	}
	if atomic.LoadInt32(&upstreamHit) != 1 {
		t.Error("upstream was not reached")
	}
	if w.Code == http.StatusBadGateway {
		t.Errorf("expected no 502 on client disconnect, got %d (body: %q)", w.Code, w.Body.String())
	}
}

func TestForwardUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Id", "abc-123")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad input"}`))
	}))
	defer upstream.Close()

	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	if err := forwardUpstreamError(w, resp); err != nil {
		t.Fatalf("forwardUpstreamError: %v", err)
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	if w.Header().Get("X-Upstream-Id") != "abc-123" {
		t.Errorf("X-Upstream-Id: got %q", w.Header().Get("X-Upstream-Id"))
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "bad input" {
		t.Errorf("body.error: got %q", body["error"])
	}
}

func TestFreediusErrorHandlerClientCancel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	w := httptest.NewRecorder()
	handler(w, req, context.Canceled)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 0 (no write on cancel)", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body: got %q, want empty (no write on cancel)", w.Body.String())
	}
}

func TestFreediusErrorHandlerTransportFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	w := httptest.NewRecorder()
	handler(w, req, &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true})

	if w.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	if !strings.Contains(w.Body.String(), `"error":"upstream_unreachable"`) {
		t.Errorf("body: got %q, want upstream_unreachable", w.Body.String())
	}
}
