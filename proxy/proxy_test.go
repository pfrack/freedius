package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newTestDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	cfg := &config.Config{
		Models: map[string]config.Model{
			"claude-opus-4": {Provider: "nim", Model: "meta/llama-3.1-70b-instruct"},
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
			name:       "POST known model no registered adapter",
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
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"opus": {Provider: "nim", Model: "x", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{"ok":true}`},
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
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"opus": {Provider: "nim", Model: "x", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{}`},
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
		Models: map[string]config.Model{
			"shared-key": {Provider: "nim", Model: "from-models", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Model{
			"shared-key": {
				Provider:  "nim",
				Model:     "from-mappings",
				APIKeyEnv: "NVIDIA_NIM_API_KEY",
			},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &recordingProvider{},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"shared-key"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "from-models" {
		t.Errorf("models should win over mappings; got model %q, want from-models", got)
	}
}

func TestServeHTTPFamilyMatch(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"opus": {
				Provider:  "nim",
				Model:     "meta/llama-3.1-70b-instruct",
				APIKeyEnv: "NVIDIA_NIM_API_KEY",
			},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{"ok":true,"matched":"family"}`},
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
		Models:   map[string]config.Model{},
		Mappings: map[string]config.Model{},
	}
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{}`},
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
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"default": {Provider: "nim", Model: "catch-all", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{"ok":true}`},
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
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"auto":    {Provider: "nim", Model: "from-auto", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
			"opus":    {Provider: "nim", Model: "from-opus", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
			"default": {Provider: "nim", Model: "from-default", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{}`},
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

func TestServeHTTPModelsWinsOverFamilyMatch(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"claude-opus-4-1": {
				Provider:  "nim",
				Model:     "exact-match",
				APIKeyEnv: "NVIDIA_NIM_API_KEY",
			},
		},
		Mappings: map[string]config.Model{
			"opus": {Provider: "nim", Model: "family-match", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	d := newTestDispatcherWithAdapter(t, cfg, map[string]Provider{
		"nim": &mockProvider{status: 200, body: `{}`},
	})

	// Exact model match should win over family pattern
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Freedius-Matched-Model"); got != "exact-match" {
		t.Errorf("models exact match should win over family; got model %q, want exact-match", got)
	}

	// A different opus version not in models: should use the family mapping
	req2 := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-5"}`),
	)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	d.ServeHTTP(rec2, req2)

	if got2 := rec2.Header().Get("X-Freedius-Matched-Model"); got2 != "family-match" {
		t.Errorf("unmatched opus version should use family; got model %q, want family-match", got2)
	}
}

type mockProvider struct {
	status int
	body   string
}

func (m *mockProvider) Handle(
	w http.ResponseWriter,
	_ *http.Request,
	_ config.Model,
	_ []byte,
) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(m.status)
	_, _ = w.Write([]byte(m.body))
	return nil
}

type recordingProvider struct {
	called bool
	model  string
}

func (r *recordingProvider) Handle(
	w http.ResponseWriter,
	_ *http.Request,
	cfg config.Model,
	_ []byte,
) error {
	r.called = true
	r.model = cfg.Model
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
	return nil
}

// TestServeHTTPCountTokens exercises the path-aware capability check that
// gates /v1/messages/count_tokens. Success cases verify the dispatcher does
// NOT reject (the actual adapter behavior is covered by anthropic_compat_test
// and mix_test); rejection cases verify the freedius error envelope shape.
func TestServeHTTPCountTokens(t *testing.T) {
	const (
		anthropicProvider = "anthropic"
		nimProvider       = "nim"
		mixProvider       = "mix"
	)
	mockOK := &mockProvider{status: http.StatusOK, body: `{"input_tokens":1}`}
	mockOKMix := &mockProvider{status: http.StatusOK, body: `{"input_tokens":1}`}

	tests := []struct {
		name            string
		path            string
		model           config.Model
		registeredMocks map[string]Provider
		wantStatus      int
		wantBodyHas     []string
		wantBodyLacks   []string
		wantHeader      map[string]string
		wantHeaderMiss  []string
	}{
		{
			name:  "anthropic provider + count_tokens path -> success (pass-through)",
			path:  "/v1/messages/count_tokens",
			model: config.Model{Provider: anthropicProvider, Model: "claude-opus-4"},
			registeredMocks: map[string]Provider{
				anthropicProvider: mockOK,
			},
			wantStatus: http.StatusOK,
			wantBodyHas: []string{
				`"input_tokens":1`,
			},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": anthropicProvider,
				"X-Freedius-Matched-Model":    "claude-opus-4",
			},
		},
		{
			name:  "nim provider + count_tokens path -> 501 not_supported",
			path:  "/v1/messages/count_tokens",
			model: config.Model{Provider: nimProvider, Model: "x"},
			registeredMocks: map[string]Provider{
				nimProvider: mockOK, // adapter should NEVER be invoked
			},
			wantStatus: http.StatusNotImplemented,
			wantBodyHas: []string{
				`"error":"not_supported"`,
				`/v1/messages/count_tokens is not supported for provider \"nim\"`,
			},
			wantHeaderMiss: []string{
				"X-Freedius-Matched-Provider",
				"X-Freedius-Matched-Model",
			},
		},
		{
			name:  "mix + Protocol anthropic + count_tokens -> success",
			path:  "/v1/messages/count_tokens",
			model: config.Model{Provider: mixProvider, Protocol: "anthropic", Model: "x"},
			registeredMocks: map[string]Provider{
				mixProvider: mockOKMix, // mix→anthropic delegation covered by mix_test
			},
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": mixProvider,
				"X-Freedius-Matched-Model":    "x",
			},
		},
		{
			name:  "mix + Protocol openai + count_tokens -> 501 not_supported",
			path:  "/v1/messages/count_tokens",
			model: config.Model{Provider: mixProvider, Protocol: "openai", Model: "x"},
			registeredMocks: map[string]Provider{
				mixProvider: mockOK,
			},
			wantStatus: http.StatusNotImplemented,
			wantBodyHas: []string{
				`"error":"not_supported"`,
				`/v1/messages/count_tokens is not supported`,
				`\"mix\"`,
			},
			wantHeaderMiss: []string{
				"X-Freedius-Matched-Provider",
				"X-Freedius-Matched-Model",
			},
		},
		{
			name: "mix + no protocol + /v1/messages BaseURL + count_tokens -> success (URL sniff)",
			path: "/v1/messages/count_tokens",
			model: config.Model{
				Provider: mixProvider,
				Model:    "x",
				BaseURL:  "https://api.minimax.io/anthropic/v1/messages",
			},
			registeredMocks: map[string]Provider{
				mixProvider: mockOKMix,
			},
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": mixProvider,
				"X-Freedius-Matched-Model":    "x",
			},
		},
		{
			name:  "regular /v1/messages + nim -> success (regression)",
			path:  "/v1/messages",
			model: config.Model{Provider: nimProvider, Model: "x"},
			registeredMocks: map[string]Provider{
				nimProvider: mockOK,
			},
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": nimProvider,
				"X-Freedius-Matched-Model":    "x",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Models: map[string]config.Model{
					"claude-opus-4": tt.model,
				},
			}
			d := newTestDispatcherWithAdapter(t, cfg, tt.registeredMocks)

			req := httptest.NewRequest(
				http.MethodPost,
				tt.path,
				strings.NewReader(`{"model":"claude-opus-4"}`),
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
