package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

func newTestMux() http.Handler {
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:     proxy.NewEventBus(10),
		LogSink: proxy.NewLogSink(10),
		Cfg:     cfg,
	}
	return SetupMux(h, logger)
}

// sink discards log output in tests.
type sink struct{}

func (sink) Write(p []byte) (int, error) { return len(p), nil }

func TestPageHandlers(t *testing.T) {
	mux := newTestMux()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantTitle  string
		wantActive string
	}{
		{"index", "/", 200, "Dashboard", "index"},
		{"logs", "/logs", 200, "Logs", "logs"},
		{"providers", "/providers", 200, "Providers", "providers"},
		{"mappings", "/mappings", 200, "Mappings", "mappings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<nav>") {
				t.Error("response should contain <nav>")
			}
			if !strings.Contains(body, "class=\"active\"") {
				t.Error("response should contain active nav class")
			}
			if !strings.Contains(body, tt.wantTitle) {
				t.Errorf("response should contain title %q", tt.wantTitle)
			}
		})
	}
}

func TestServeStatic(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "max-age=300") {
		t.Errorf("Cache-Control = %q, want max-age=300", cc)
	}
}

func TestHealthEndpoint(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Error("health response should contain status ok")
	}
}
