package proxy

import (
	"bytes"
	"errors"
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
	registry := NewRegistry(nil)
	return NewDispatcher(cfg, registry, logger)
}

func newTestDispatcherWithRegistry(t *testing.T, providers map[string]Provider) *Dispatcher {
	t.Helper()
	cfg := &config.Config{
		Models: map[string]config.Model{
			"claude-opus-4":   {Provider: "nim", Model: "meta/llama-3.1-70b-instruct"},
			"claude-sonnet-4": {Provider: "custom", Model: "my-sonnet-shim"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(providers)
	return NewDispatcher(cfg, registry, logger)
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
			name:        "POST known model",
			method:      http.MethodPost,
			body:        `{"model":"claude-opus-4"}`,
			wantStatus:  http.StatusInternalServerError,
			wantBodyHas: []string{`"error":"provider not registered: nim"`},
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
			wantBodyHas:    []string{`"status":"no_match"`},
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
				t.Errorf("status: got %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
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

type stubProvider struct {
	called bool
	body   []byte
	model  config.Model
}

func (s *stubProvider) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	s.called = true
	s.body = body
	s.model = m
	w.Header().Set("X-Stub-Provider", "called")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"stub":"ok"}`))
	return nil
}

func TestServeHTTPWithRegisteredProvider(t *testing.T) {
	stub := &stubProvider{}
	d := newTestDispatcherWithRegistry(t, map[string]Provider{"nim": stub})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !stub.called {
		t.Error("provider Handle was not called")
	}
	if got := rec.Header().Get("X-Stub-Provider"); got != "called" {
		t.Errorf("X-Stub-Provider header: got %q, want \"called\"", got)
	}
	if got := rec.Header().Get("X-Freedius-Matched-Provider"); got != "nim" {
		t.Errorf("X-Freedius-Matched-Provider: got %q, want nim (preserved by adapter)", got)
	}
	if string(stub.body) != `{"model":"claude-opus-4","messages":[]}` {
		t.Errorf("adapter received body: got %q", stub.body)
	}
}

type erroringProvider struct{}

func (e *erroringProvider) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	return errors.New("transport-level failure")
}

func TestServeHTTPAdapterError(t *testing.T) {
	d := newTestDispatcherWithRegistry(t, map[string]Provider{"nim": &erroringProvider{}})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "upstream error") {
		t.Errorf("body %q does not contain \"upstream error\"", rec.Body.String())
	}
}

func TestServeHTTPCustomAdapterEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Id", "e2e")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_e2e","type":"message","role":"assistant","content":[],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	t.Setenv("MY_SHIM_API_KEY", "sk-test-e2e")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	customAdapter := NewCustomAdapter(logger)
	cfg := &config.Config{
		Models: map[string]config.Model{
			"claude-sonnet-4": {Provider: "custom", Model: "my-sonnet-shim", BaseURL: upstream.URL + "/v1/messages", APIKeyEnv: "MY_SHIM_API_KEY"},
		},
	}
	registry := NewRegistry(map[string]Provider{"custom": customAdapter})
	d := NewDispatcher(cfg, registry, logger)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Upstream-Id") != "e2e" {
		t.Errorf("X-Upstream-Id: got %q, want e2e", rec.Header().Get("X-Upstream-Id"))
	}
	if rec.Header().Get("X-Freedius-Matched-Provider") != "custom" {
		t.Errorf("X-Freedius-Matched-Provider: got %q, want custom (preserved through adapter)", rec.Header().Get("X-Freedius-Matched-Provider"))
	}
	if !strings.Contains(rec.Body.String(), `"id":"msg_e2e"`) {
		t.Errorf("body: got %q", rec.Body.String())
	}
}
