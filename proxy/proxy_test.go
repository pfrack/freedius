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
	return NewDispatcher(cfg, logger)
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
			wantStatus:  http.StatusNotImplemented,
			wantBodyHas: []string{`"matched_provider":"nim"`, `"matched_model":"meta/llama-3.1-70b-instruct"`, `"status":"not_implemented"`},
			wantHeader: map[string]string{
				"X-Freedius-Matched-Provider": "nim",
				"X-Freedius-Matched-Model":    "meta/llama-3.1-70b-instruct",
				"Content-Type":                "application/json",
			},
		},
		{
			name:          "POST unknown model",
			method:        http.MethodPost,
			body:          `{"model":"unknown"}`,
			wantStatus:    http.StatusNotFound,
			wantBodyHas:   []string{`"status":"no_match"`},
			wantBodyLacks: []string{"matched_provider", "matched_model"},
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
