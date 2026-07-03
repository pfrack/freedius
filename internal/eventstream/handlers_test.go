package eventstream

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func newTestHandlers() *Handlers {
	return &Handlers{
		Bus:       proxy.NewEventBus(100),
		LogSink:   proxy.NewLogSink(100),
		Cfg:       &config.Config{Providers: map[string]config.Provider{}, Mappings: map[string]config.Mapping{}},
		StartTime: time.Now(),
	}
}

func TestHandleEvents_Replay(t *testing.T) {
	h := newTestHandlers()

	// Emit some events.
	for i := 0; i < 5; i++ {
		h.Bus.Emit(proxy.RequestEvent{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "POST",
			Path:      "/v1/messages",
		})
	}

	// Use a real server to test SSE streaming.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleEvents(w, r)
	}))
	defer srv.Close()

	// Use a context with timeout so the handler doesn't block forever.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events?since=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteString("\n")
	}

	out := body.String()

	// Should contain replay complete frame.
	if !strings.Contains(out, `"complete":true`) {
		t.Error("expected replay complete frame")
	}

	// Should contain event frames.
	if !strings.Contains(out, "event: event") {
		t.Error("expected event frames")
	}

	// Verify SSE framing: no triple newline (json.NewEncoder bug per lessons.md §1).
	re := regexp.MustCompile(`\n\n\n`)
	if re.MatchString(out) {
		t.Error("SSE framing has triple newline (json.NewEncoder bug)")
	}
}

func TestHandleEvents_Eviction(t *testing.T) {
	h := newTestHandlers()

	// Emit one event.
	h.Bus.Emit(proxy.RequestEvent{RequestID: "req-0", Method: "POST", Path: "/"})
	currentSeq := h.Bus.EventCount()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleEvents(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events?since=1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteString("\n")
	}

	out := body.String()
	if !strings.Contains(out, fmt.Sprintf(`"current_seq":%d`, currentSeq)) {
		t.Errorf("expected current_seq in response, got:\n%s", out)
	}
}

func TestHandleLogs_Replay(t *testing.T) {
	h := newTestHandlers()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleLogs(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/logs?since=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteString("\n")
	}

	out := body.String()
	if !strings.Contains(out, `"complete":true`) {
		t.Error("expected replay complete frame")
	}
	if !strings.Contains(out, "event: replay") {
		t.Error("expected replay event type")
	}
}

func TestHandleEvents_Live(t *testing.T) {
	h := newTestHandlers()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleEvents(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events?since=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	// Emit an event while the handler is streaming.
	time.Sleep(50 * time.Millisecond)
	h.Bus.Emit(proxy.RequestEvent{RequestID: "live-1", Method: "GET", Path: "/"})

	// Read until we find the live event or timeout.
	found := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "live-1") {
				found = true
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
	}

	cancel()

	if !found {
		t.Error("expected live event in response")
	}
}

func TestHandleStats(t *testing.T) {
	h := newTestHandlers()

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "uptime") {
		t.Error("stats response should contain uptime")
	}
}

func TestHandleConfig(t *testing.T) {
	h := newTestHandlers()

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestWriteSSE_Format(t *testing.T) {
	h := newTestHandlers()

	rec := httptest.NewRecorder()
	h.writeSSE(rec, "test", map[string]string{"key": "value"})

	body := rec.Body.String()
	expected := "event: test\ndata: {\"key\":\"value\"}\n\n"
	if body != expected {
		t.Errorf("SSE format = %q, want %q", body, expected)
	}
}

func TestRequireAuth_NoToken(t *testing.T) {
	h := &Handlers{AuthToken: ""}

	called := false
	handler := h.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should be called when no token configured")
	}
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequireAuth_CorrectToken(t *testing.T) {
	h := &Handlers{AuthToken: "secret123"}

	called := false
	handler := h.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should be called with correct token")
	}
}

func TestRequireAuth_WrongToken(t *testing.T) {
	h := &Handlers{AuthToken: "secret123"}

	handler := h.requireAuth(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called with wrong token")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Error("response should contain unauthorized error")
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	h := &Handlers{AuthToken: "secret123"}

	handler := h.requireAuth(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called without auth header")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestSSE_NoTripleNewline(t *testing.T) {
	h := newTestHandlers()

	h.Bus.Emit(proxy.RequestEvent{RequestID: "a", Method: "POST", Path: "/"})
	h.Bus.Emit(proxy.RequestEvent{RequestID: "b", Method: "GET", Path: "/test"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleEvents(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events?since=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteString("\n")
	}

	out := body.String()

	re := regexp.MustCompile(`\n\n\n`)
	if re.MatchString(out) {
		t.Error("SSE output contains triple newline — json.NewEncoder was used instead of json.Marshal")
	}
}

func TestEventsReplay_BeforeLive(t *testing.T) {
	h := newTestHandlers()

	h.Bus.Emit(proxy.RequestEvent{RequestID: "before-1", Method: "POST", Path: "/"})
	h.Bus.Emit(proxy.RequestEvent{RequestID: "before-2", Method: "GET", Path: "/"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleEvents(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events?since=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	lines := make(chan string, 100)
	go func() {
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	// Wait for replay, then emit a live event.
	time.Sleep(100 * time.Millisecond)
	h.Bus.Emit(proxy.RequestEvent{RequestID: "after-1", Method: "PUT", Path: "/"})

	// Collect output.
	var body []string
	timeout := time.After(1 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				goto done
			}
			body = append(body, line)
		case <-timeout:
			goto done
		}
	}
done:
	cancel()

	// Verify ordering: before events come before after event.
	out := strings.Join(body, "\n")
	beforeIdx := strings.Index(out, "before-1")
	afterIdx := strings.Index(out, "after-1")
	if beforeIdx < 0 || afterIdx < 0 {
		t.Fatalf("expected both before-1 and after-1 in output, got:\n%s", out)
	}
	if beforeIdx > afterIdx {
		t.Error("before-1 should appear before after-1")
	}
}

func TestHandlers_Register_AllRoutes(t *testing.T) {
	h := newTestHandlers()
	mux := http.NewServeMux()
	h.Register(mux)

	// Non-SSE routes: use httptest.NewRecorder (no blocking).
	for _, route := range []string{"/v1/stats", "/v1/config"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("GET %s: status = %d, want 200", route, rec.Code)
		}
	}

	// SSE routes: use httptest.NewServer with context timeout.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, route := range []string{"/v1/events", "/v1/logs"} {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+route, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("GET %s: %v", route, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: status = %d, want 200", route, resp.StatusCode)
		}
	}
}
