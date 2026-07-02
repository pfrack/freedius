package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func newTestDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "nim", ModelString: "meta/llama-3.1-70b-instruct"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{})
	return NewDispatcher(cfg, registry, logger, false)
}

func newTestDispatcherWithAdapter(
	t *testing.T,
	cfg *config.Config,
	providers map[string]Provider,
) *Dispatcher {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(providers)
	return NewDispatcher(cfg, registry, logger, false)
}

func TestServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		body           string
		contentType    string
		wantStatus     int
		wantBodyHas    []string
		wantBodyLacks  []string
		wantHeader     map[string]string
		wantHeaderMiss []string
	}{
		{
			name:       "POST known mapping no registered adapter",
			method:     http.MethodPost,
			body:       `{"model":"claude-opus-4"}`,
			wantStatus: http.StatusInternalServerError,
			wantBodyHas: []string{
				`"error":"provider_not_registered"`,
				`is not registered in this freedius build`,
			},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "nim",
				"X-Freedius-Matched-Model":    "meta/llama-3.1-70b-instruct",
				"Content-Type":                "application/json",
			},
		},
		{
			name:           "POST unknown model",
			method:         http.MethodPost,
			body:           `{"model":"unknown"}`,
			wantStatus:     http.StatusNotFound,
			wantBodyHas:    []string{`"error":"no_match"`, `no configured mapping for model`},
			wantBodyLacks:  []string{"matched_provider", "matched_model"},
			wantHeaderMiss: []string{"X-Freedius-Matched-Provider", "X-Freedius-Matched-Model"},
		},
		{
			name:        "POST malformed JSON",
			method:      http.MethodPost,
			body:        `{not json`,
			wantStatus:  http.StatusBadRequest,
			wantBodyHas: []string{"invalid request body:"},
		},
		{
			name:        "POST missing model field",
			method:      http.MethodPost,
			body:        `{"other":"value"}`,
			wantStatus:  http.StatusBadRequest,
			wantBodyHas: []string{"missing or empty"},
		},
		{
			name:        "POST empty body",
			method:      http.MethodPost,
			body:        ``,
			wantStatus:  http.StatusBadRequest,
			wantBodyHas: []string{"empty"},
		},
		{
			name:        "POST non-JSON content type",
			method:      http.MethodPost,
			body:        `{"model":"claude-opus-4"}`,
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantBodyHas: []string{"unsupported content type"},
		},
		{
			name:       "GET method not allowed",
			method:     http.MethodGet,
			body:       ``,
			wantStatus: http.StatusMethodNotAllowed,
			wantHeader: map[string]string{"Allow": http.MethodPost},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDispatcher(t)
			req := httptest.NewRequest(tt.method, "/v1/messages", strings.NewReader(tt.body))
			ct := tt.contentType
			if ct == "" {
				ct = "application/json"
			}
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			d.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf(
					"status: got %d, want %d (body: %s)",
					rec.Code,
					tt.wantStatus,
					rec.Body.String(),
				)
			}

			for k, v := range tt.wantHeader {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("header %s: got %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.wantHeaderMiss {
				if got := rec.Header().Get(k); got != "" {
					t.Errorf("header %s: got %q, want missing", k, got)
				}
			}

			body := rec.Body.String()
			for _, s := range tt.wantBodyHas {
				if !strings.Contains(body, s) {
					t.Errorf("body %q does not contain %q", body, s)
				}
			}
			for _, s := range tt.wantBodyLacks {
				if strings.Contains(body, s) {
					t.Errorf("body %q should not contain %q", body, s)
				}
			}
		})
	}
}

func TestServeHTTPMultiProviderRouting(t *testing.T) {
	var providerACalled, providerBCalled bool

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerACalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"provider":"A"}`))
	}))
	defer upstreamA.Close()

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerBCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"provider":"B"}`))
	}))
	defer upstreamB.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"providerA": {
				Behavior:         "openai",
				DefaultBaseURL:   upstreamA.URL,
				DefaultAPIKeyEnv: "PROVIDER_A_KEY",
			},
			"providerB": {
				Behavior:         "openai",
				DefaultBaseURL:   upstreamB.URL,
				DefaultAPIKeyEnv: "PROVIDER_B_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"model-a": {ProviderName: "providerA", ModelString: "model-a"},
			"model-b": {ProviderName: "providerB", ModelString: "model-b"},
		},
	}
	t.Setenv("PROVIDER_A_KEY", "sk-a")
	t.Setenv("PROVIDER_B_KEY", "sk-b")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{
		"openai": NewOpenAICompatibleAdapter(logger),
	})
	d := NewDispatcher(cfg, registry, logger, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"model-a","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !providerACalled {
		t.Error("provider A was NOT called; expected it to receive the request")
	}
	if providerBCalled {
		t.Error("provider B was called; expected it to be untouched")
	}
	if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != "providerA" {
		t.Errorf("X-Freedius-Matched-Provider: got %q, want providerA", got)
	}
}

func TestServeHTTPOversizeBody(t *testing.T) {
	d := newTestDispatcher(t)

	oversize := bytes.Repeat([]byte("a"), MaxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(oversize))
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Errorf("body %q does not contain 'request body too large'", rec.Body.String())
	}
}

func TestServeHTTPMappingsLookup(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"from":"upstream"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "x"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{"ok":true}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"opus"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != "nim" {
		t.Errorf("X-Freedius-Matched-Provider: got %q, want nim", got)
	}
	_ = upstream
}

func TestServeHTTPNeitherMatch(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "x"},
		},
	}
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"unknown"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "no_match") {
		t.Errorf("body: got %q, want no_match", rec.Body.String())
	}
}

func TestServeHTTPModelsWinsOverMappings(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"shared-key":      {ProviderName: "nim", ModelString: "from-mappings"},
			"shared-key-name": {ProviderName: "nim", ModelString: "from-models"},
		},
	}
	_ = cfg
	cfg = &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"shared-key": {ProviderName: "nim", ModelString: "from-mappings"},
		},
	}
	// Add a Models map that wins for a key that's also in Mappings.
	// Note: under the new schema, Models map is removed at the dispatcher level —
	// dispatcher only consults Mappings, so this regression test no longer
	// applies. Kept here as a placeholder documenting the behavior change.
	t.Skip("Models map removed in providers-section-refactor; dispatcher consults Mappings only")

	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &recordingProvider{},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"shared-key"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "from-mappings" {
		t.Errorf("matched model: got %q, want from-mappings", got)
	}
}

func TestServeHTTPFamilyMatch(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "meta/llama-3.1-70b-instruct"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{"ok":true,"matched":"family"}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != "nim" {
		t.Errorf("provider: got %q, want nim", got)
	}
	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "meta/llama-3.1-70b-instruct" {
		t.Errorf("model: got %q, want meta/llama-3.1-70b-instruct", got)
	}
}

func TestServeHTTPFamilyNoDefault(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestServeHTTPFamilyDefaultCatchAll(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"default": {ProviderName: "nim", ModelString: "catch-all"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{"ok":true}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-unknown-2026"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "catch-all" {
		t.Errorf("model: got %q, want catch-all", got)
	}
}

func TestServeHTTPFamilyPriorityIndependentOfYAMLOrder(t *testing.T) {
	// Mappings list auto before opus — but opus has higher priority in knownFamilies
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"auto":    {ProviderName: "nim", ModelString: "from-auto"},
			"opus":    {ProviderName: "nim", ModelString: "from-opus"},
			"default": {ProviderName: "nim", ModelString: "from-default"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{}`},
	})

	// claude-opus-4-1 should match opus even though auto is listed first in the map
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "from-opus" {
		t.Errorf("expected opus family priority, got model %q", got)
	}
}

func TestServeHTTPFamilyMatchWinsOverUnrelatedExact(t *testing.T) {
	// Exact match on a different family should not preempt family matching
	// for the requested model.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"sonnet": {ProviderName: "nim", ModelString: "exact-match"},
			"opus":   {ProviderName: "nim", ModelString: "family-match"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"openai": &mockProvider{status: 200, body: `{}`},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-5"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "family-match" {
		t.Errorf("unmatched opus version should use family; got model %q, want family-match", got)
	}
}

type mockProvider struct {
	status int
	body   string
}

func (m *mockProvider) Handle(
	w http.ResponseWriter,
	_ *http.Request,
	_ config.Provider,
	_ config.Mapping,
	_ []byte,
) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(m.status)
	_, _ = w.Write([]byte(m.body))
	return nil
}

type recordingProvider struct {
	called   bool
	modelStr string
}

func (r *recordingProvider) Handle(
	w http.ResponseWriter,
	_ *http.Request,
	_ config.Provider,
	mapping config.Mapping,
	_ []byte,
) error {
	r.called = true
	r.modelStr = mapping.ModelString
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
	return nil
}

func TestServeHTTPMappingEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		body         string
		wantStatus   int
		wantBody     []string
		wantModel    string
		wantProvider string
	}{
		{
			name: "model matches no family and no exact mapping",
			cfg: &config.Config{
				Providers: map[string]config.Provider{
					"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
				},
				Mappings: map[string]config.Mapping{
					"opus": {ProviderName: "nim", ModelString: "x"},
				},
			},
			body:       `{"model":"gpt-4-turbo"}`,
			wantStatus: http.StatusNotFound,
			wantBody:   []string{"no_match", "no configured mapping"},
		},
		{
			name: "family priority: opus wins over auto",
			cfg: &config.Config{
				Providers: map[string]config.Provider{
					"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
				},
				Mappings: map[string]config.Mapping{
					"auto": {ProviderName: "nim", ModelString: "from-auto"},
					"opus": {ProviderName: "nim", ModelString: "from-opus"},
				},
			},
			body:         `{"model":"claude-opus-4-2025"}`,
			wantStatus:   http.StatusOK,
			wantModel:    "from-opus",
			wantProvider: "nim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDispatcherWithAdapter(t, tt.cfg, map[string]Provider{
				"openai": &mockProvider{status: 200, body: `{"ok":true}`},
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/messages",
				strings.NewReader(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			d.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := rec.Body.String()
			for _, s := range tt.wantBody {
				if !strings.Contains(body, s) {
					t.Errorf("body %q does not contain %q", body, s)
				}
			}
			if tt.wantModel != "" {
				if got := rec.Header().Get("X-Freedius-Matched-Model"); got != tt.wantModel {
					t.Errorf("X-Freedius-Matched-Model: got %q, want %q", got, tt.wantModel)
				}
			}
			if tt.wantProvider != "" {
				if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != tt.wantProvider {
					t.Errorf("X-Freedius-Matched-Provider: got %q, want %q", got, tt.wantProvider)
				}
			}
		})
	}
}

// TestServeHTTPCountTokens exercises the path-aware capability check that
// gates /v1/messages/count_tokens. Success cases verify the dispatcher does
// NOT reject (the actual adapter behavior is covered by anthropic_compat_test
// and mix_test); the OpenAI-protocol local-counter cases verify the
// dispatcher computes the count locally and does NOT invoke the adapter.
func TestServeHTTPCountTokens(t *testing.T) {
	const (
		anthropicBehavior = "anthropic"
		nimBehavior       = "openai"
		mixBehavior       = "mix"
	)
	mockOK := &mockProvider{status: http.StatusOK, body: `{"input_tokens":1}`}
	mockOKMix := &mockProvider{status: http.StatusOK, body: `{"input_tokens":1}`}

	tests := []struct {
		name                  string
		path                  string
		provider              config.Provider
		mapping               config.Mapping
		registeredMocks       map[string]Provider
		wantStatus            int
		wantBodyHas           []string
		wantBodyLacks         []string
		wantHeader            map[string]string
		wantHeaderMiss        []string
		wantAdapterNotCalled  bool
		wantInputTokensGTZero bool
	}{
		{
			name: "anthropic provider + count_tokens path -> success (pass-through)",
			path: "/v1/messages/count_tokens",
			provider: config.Provider{
				Behavior:            anthropicBehavior,
				SupportsCountTokens: true,
			},
			mapping: config.Mapping{ProviderName: "anthropic", ModelString: "claude-opus-4"},
			registeredMocks: map[string]Provider{
				anthropicBehavior: mockOK,
			},
			wantStatus: http.StatusOK,
			wantBodyHas: []string{
				`"input_tokens":1`,
			},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "anthropic",
				"X-Freedius-Matched-Model":    "claude-opus-4",
			},
		},
		{
			name: "nim provider + count_tokens path -> local counter (200)",
			path: "/v1/messages/count_tokens",
			provider: config.Provider{
				Behavior:            nimBehavior,
				SupportsCountTokens: false,
			},
			mapping: config.Mapping{ProviderName: "nim", ModelString: "x"},
			registeredMocks: map[string]Provider{
				nimBehavior: &recordingProvider{},
			},
			wantStatus: http.StatusOK,
			wantBodyHas: []string{
				`"input_tokens":`,
				`"context_management":`,
				`"original_input_tokens":`,
			},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "nim",
				"X-Freedius-Matched-Model":    "x",
			},
			wantAdapterNotCalled:  true,
			wantInputTokensGTZero: true,
		},
		{
			name: "mix with /v1/messages base_url + count_tokens -> success (path sniff)",
			path: "/v1/messages/count_tokens",
			provider: config.Provider{
				Behavior:            mixBehavior,
				DefaultBaseURL:      "https://api.minimax.io/anthropic/v1/messages",
				SupportsCountTokens: true,
			},
			mapping: config.Mapping{ProviderName: "mix", ModelString: "x"},
			registeredMocks: map[string]Provider{
				mixBehavior: mockOKMix,
			},
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "mix",
				"X-Freedius-Matched-Model":    "x",
			},
		},
		{
			name: "mix with /v1/chat/completions base_url + count_tokens -> local counter",
			path: "/v1/messages/count_tokens",
			provider: config.Provider{
				Behavior:            mixBehavior,
				DefaultBaseURL:      "https://api.minimax.io/v1/chat/completions",
				SupportsCountTokens: false,
			},
			mapping: config.Mapping{ProviderName: "mix", ModelString: "x"},
			registeredMocks: map[string]Provider{
				mixBehavior: &recordingProvider{},
			},
			wantStatus: http.StatusOK,
			wantBodyHas: []string{
				`"input_tokens":`,
				`"context_management":`,
				`"original_input_tokens":`,
			},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "mix",
				"X-Freedius-Matched-Model":    "x",
			},
			wantAdapterNotCalled:  true,
			wantInputTokensGTZero: true,
		},
		{
			name: "regular /v1/messages + nim -> success (regression)",
			path: "/v1/messages",
			provider: config.Provider{
				Behavior:            nimBehavior,
				SupportsCountTokens: false,
			},
			mapping: config.Mapping{ProviderName: "nim", ModelString: "x"},
			registeredMocks: map[string]Provider{
				nimBehavior: mockOK,
			},
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "nim",
				"X-Freedius-Matched-Model":    "x",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Providers: map[string]config.Provider{
					tt.mapping.ProviderName: tt.provider,
				},
				Mappings: map[string]config.Mapping{
					"claude-opus-4": tt.mapping,
				},
			}
			d := newTestDispatcherWithAdapter(t, cfg, tt.registeredMocks)

			body := `{"model":"claude-opus-4","messages":[{"role":"user","content":"hello world"}]}`
			req := httptest.NewRequest(
				http.MethodPost,
				tt.path,
				strings.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			d.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf(
					"status: got %d, want %d (body: %s)",
					rec.Code,
					tt.wantStatus,
					rec.Body.String(),
				)
			}

			for k, v := range tt.wantHeader {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("header %s: got %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.wantHeaderMiss {
				if got := rec.Header().Get(k); got != "" {
					t.Errorf("header %s: got %q, want missing", k, got)
				}
			}

			respBody := rec.Body.String()
			for _, s := range tt.wantBodyHas {
				if !strings.Contains(respBody, s) {
					t.Errorf("body %q does not contain %q", respBody, s)
				}
			}
			for _, s := range tt.wantBodyLacks {
				if strings.Contains(respBody, s) {
					t.Errorf("body %q should not contain %q", respBody, s)
				}
			}

			if tt.wantInputTokensGTZero {
				var resp struct {
					InputTokens int `json:"input_tokens"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Errorf("parse count_tokens response: %v (body: %s)", err, respBody)
				}
				if resp.InputTokens <= 0 {
					t.Errorf("input_tokens: got %d, want > 0", resp.InputTokens)
				}
			}

			if tt.wantAdapterNotCalled {
				for providerName, p := range tt.registeredMocks {
					if rp, ok := p.(*recordingProvider); ok && rp.called {
						t.Errorf(
							"adapter %q was invoked; the local counter must short-circuit dispatch",
							providerName,
						)
					}
				}
			}
		})
	}
}

// TestDispatcher_ConcurrentMapAccess is a regression test for the F1
// impl-review finding. The dispatcher reads config.Providers/Mappings on
// HTTP goroutines while the TUI mutates them under write lock. With the
// sync.RWMutex in config.Config, the race detector must stay clean under
// sustained concurrent load.
func TestDispatcher_ConcurrentMapAccess(t *testing.T) {
	cfg, err := config.LoadFromBytes([]byte(`providers:
  nim: {behavior: openai}
mappings:
  opus: {provider_name: nim, model_string: m-opus}
  sonnet: {provider_name: nim, model_string: m-sonnet}
  haiku: {provider_name: nim, model_string: m-haiku}
`))
	if err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{
		Cfg:      cfg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Registry: NewRegistry(nil),
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: constantly mutate the config maps under write lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			cfg.Lock()
			cfg.Mappings["opus"] = config.Mapping{
				ProviderName: "nim",
				ModelString:  fmt.Sprintf("m-%d", i),
			}
			cfg.Unlock()
		}
	}()

	// Readers: invoke resolveMapping concurrently.
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _, _ = d.resolveMapping("opus")
			}
		}()
	}

	// Let it run briefly, then stop everything.
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
