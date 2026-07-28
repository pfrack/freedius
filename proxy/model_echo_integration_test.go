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

func newModelEchoDispatcher(t *testing.T, cfg *config.Config, providers map[string]Provider) *Dispatcher {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewDispatcher(cfg, NewRegistry(providers), logger, false, 2, 5*time.Minute)
}

func modelEchoRequest() *http.Request {
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func writeOpenAIModelEchoStream(w http.ResponseWriter, upstreamModel string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	var stream strings.Builder
	stream.WriteString(
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"`,
	)
	stream.WriteString(upstreamModel)
	stream.WriteString(
		`","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` +
			"\n\n",
	)
	stream.WriteString(
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"`,
	)
	stream.WriteString(upstreamModel)
	stream.WriteString(
		`","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` +
			"\n\n",
	)
	stream.WriteString("data: [DONE]\n\n")
	_, _ = io.WriteString(w, stream.String())
}

func TestModelEcho_DispatcherOpenAIPath(t *testing.T) {
	const upstreamModel = "openai-upstream-model"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIModelEchoStream(w, upstreamModel)
	}))
	defer upstream.Close()

	t.Setenv("MODEL_ECHO_OPENAI_KEY", "sk-test")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"openai-provider": {
				Behavior:         "openai",
				DefaultBaseURL:   upstream.URL,
				DefaultAPIKeyEnv: "MODEL_ECHO_OPENAI_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "openai-provider", ModelString: upstreamModel},
		},
	}
	d := newModelEchoDispatcher(t, cfg, map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, modelEchoRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"model":"claude-opus-4"`) {
		t.Errorf("response did not echo client model: %s", rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != upstreamModel {
		t.Errorf("matched model: got %q, want %q", got, upstreamModel)
	}
}

func TestModelEcho_DispatcherAnthropicPath(t *testing.T) {
	const upstreamModel = "anthropic-upstream-model"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"model":"`+upstreamModel+`","id":"msg-test","content":[]}`)
	}))
	defer upstream.Close()

	t.Setenv("MODEL_ECHO_ANTHROPIC_KEY", "sk-test")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"anthropic-provider": {
				Behavior:         "anthropic",
				DefaultBaseURL:   upstream.URL,
				DefaultAPIKeyEnv: "MODEL_ECHO_ANTHROPIC_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "anthropic-provider", ModelString: upstreamModel},
		},
	}
	d := newModelEchoDispatcher(t, cfg, map[string]Provider{
		"anthropic": NewAnthropicCompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)), false),
	})

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, modelEchoRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"model":"claude-opus-4"`) {
		t.Errorf("response did not echo client model: %s", rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != upstreamModel {
		t.Errorf("matched model: got %q, want %q", got, upstreamModel)
	}
}

func TestModelEcho_DispatcherFallbackStability(t *testing.T) {
	const fallbackModel = "fallback-anthropic-model"
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"primary failed"}}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"model":"`+fallbackModel+`","id":"msg-fallback","content":[]}`)
	}))
	defer fallback.Close()

	t.Setenv("MODEL_ECHO_PRIMARY_KEY", "sk-primary")
	t.Setenv("MODEL_ECHO_FALLBACK_KEY", "sk-fallback")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"primary-provider": {
				Behavior:         "anthropic",
				DefaultBaseURL:   primary.URL,
				DefaultAPIKeyEnv: "MODEL_ECHO_PRIMARY_KEY",
			},
			"fallback-provider": {
				Behavior:         "anthropic",
				DefaultBaseURL:   fallback.URL,
				DefaultAPIKeyEnv: "MODEL_ECHO_FALLBACK_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {
				ProviderName: "primary-provider",
				ModelString:  "primary-anthropic-model",
				Fallback: []config.Mapping{
					{ProviderName: "fallback-provider", ModelString: fallbackModel},
				},
			},
		},
	}
	d := newModelEchoDispatcher(t, cfg, map[string]Provider{
		"anthropic": NewAnthropicCompatibleAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)), false),
	})

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, modelEchoRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"model":"claude-opus-4"`) {
		t.Errorf("fallback response did not echo client model: %s", rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != "fallback-provider" {
		t.Errorf("matched provider: got %q, want fallback-provider", got)
	}
	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != fallbackModel {
		t.Errorf("matched model: got %q, want %q", got, fallbackModel)
	}
}
