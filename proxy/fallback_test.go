package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
)

func newFallbackTestDispatcher(t *testing.T, cfg *config.Config, providers map[string]Provider) *Dispatcher {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(providers)
	return NewDispatcher(cfg, registry, logger, false, 2, 5*time.Minute)
}

func TestFallback_MissingAPIKey_PrimaryFailsFallbackSucceeds(t *testing.T) {
	t.Setenv("FB_KEY_B", "sk-b")
	calledB := false
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calledB = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: "http://localhost:1", DefaultAPIKeyEnv: "FB_KEY_MISSING"},
			"provB": {Behavior: "openai", DefaultBaseURL: upstreamB.URL, DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !calledB {
		t.Error("fallback provider B was NOT called")
	}
}

func TestFallback_TransportFailure_PrimaryFailsFallbackSucceeds(t *testing.T) {
	t.Setenv("FB_KEY_B", "sk-b")
	calledB := false
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calledB = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: "http://127.0.0.1:1", DefaultAPIKeyEnv: "FB_KEY_B"},
			"provB": {Behavior: "openai", DefaultBaseURL: upstreamB.URL, DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !calledB {
		t.Error("fallback provider B was NOT called")
	}
}

func TestFallback_Upstream429_PrimaryFailsFallbackSucceeds(t *testing.T) {
	t.Setenv("FB_KEY_A", "sk-a")
	t.Setenv("FB_KEY_B", "sk-b")
	calledB := false
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("retry-after", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calledB = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: upstreamA.URL, DefaultAPIKeyEnv: "FB_KEY_A"},
			"provB": {Behavior: "openai", DefaultBaseURL: upstreamB.URL, DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !calledB {
		t.Error("fallback provider B was NOT called")
	}
}

func TestFallback_Upstream500_PrimaryFailsFallbackSucceeds(t *testing.T) {
	t.Setenv("FB_KEY_A", "sk-a")
	t.Setenv("FB_KEY_B", "sk-b")
	calledB := false
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal failure"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calledB = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: upstreamA.URL, DefaultAPIKeyEnv: "FB_KEY_A"},
			"provB": {Behavior: "openai", DefaultBaseURL: upstreamB.URL, DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !calledB {
		t.Error("fallback provider B was NOT called")
	}
}

func TestFallback_AllFail_AggregatedError(t *testing.T) {
	t.Setenv("FB_KEY_A", "")
	t.Setenv("FB_KEY_B", "")

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: "http://127.0.0.1:1", DefaultAPIKeyEnv: "FB_KEY_A"},
			"provB": {Behavior: "openai", DefaultBaseURL: "http://127.0.0.1:2", DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "all providers failed") {
		t.Errorf("expected aggregated error, got: %s", body)
	}
	if !strings.Contains(body, "provA") {
		t.Errorf("error should mention provA, got: %s", body)
	}
	if !strings.Contains(body, "provB") {
		t.Errorf("error should mention provB, got: %s", body)
	}
}

func TestFallback_MixedFailures_AggregatedError(t *testing.T) {
	t.Setenv("FB_KEY_A", "sk-a")
	t.Setenv("FB_KEY_B", "sk-b")
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"provA": {Behavior: "openai", DefaultBaseURL: upstreamA.URL, DefaultAPIKeyEnv: "FB_KEY_A"},
			"provB": {Behavior: "openai", DefaultBaseURL: upstreamB.URL, DefaultAPIKeyEnv: "FB_KEY_B"},
		},
		Mappings: map[string]config.Mapping{
			"test-model": {
				ProviderName: "provA",
				ModelString:  "gpt-4",
				Fallback: []config.Mapping{
					{ProviderName: "provB", ModelString: "gpt-4"},
				},
			},
		},
	}
	d := newFallbackTestDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "all providers failed") {
		t.Errorf("expected aggregated error, got: %s", body)
	}
	if !strings.Contains(body, "provA/gpt-4") {
		t.Errorf("error should mention provA/gpt-4, got: %s", body)
	}
	if !strings.Contains(body, "provB/gpt-4") {
		t.Errorf("error should mention provB/gpt-4, got: %s", body)
	}
}
