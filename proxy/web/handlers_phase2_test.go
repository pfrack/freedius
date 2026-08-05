package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestHandleLogs_ProviderFilter verifies §2.5: ?provider= applies a
// case-insensitive substring filter on the rendered log lines.
func TestHandleLogs_ProviderFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)
	// Push fixtures directly via the sink's underlying handle path.
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	ringHandler := proxy.NewRingHandler(logger.Handler(), logSink)
	logger = slog.New(ringHandler)
	logger.Info("dispatch alpha", "provider", "alpha", "model", "m1")
	logger.Info("dispatch beta", "provider", "beta", "model", "m2")
	logger.Info("dispatch alpha retry", "provider", "alpha", "model", "m3")

	tests := []struct {
		name        string
		query       string
		wantCount   int
		mustContain string
		mustExclude string
	}{
		{"alpha only", "?provider=alpha", 2, "alpha", "dispatch beta"},
		{"beta only", "?provider=beta", 1, "beta", "alpha retry"},
		{"combined min=info&provider=alpha", "?min=info&provider=alpha", 2, "alpha", "beta"},
		{"no match", "?provider=nonexistent", 0, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/logs"+tt.query, nil)
			rec := httptest.NewRecorder()
			handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if tt.mustContain != "" && !strings.Contains(body, tt.mustContain) {
				t.Errorf("body missing %q; got: %s", tt.mustContain, body)
			}
			if tt.mustExclude != "" && strings.Contains(body, tt.mustExclude) {
				t.Errorf("body should NOT contain %q; got: %s", tt.mustExclude, body)
			}
			if tt.wantCount == 0 {
				if strings.Contains(body, "<pre") {
					t.Errorf("expected 0 entries but body contains <pre>: %s", body)
				}
			}
		})
	}
}

// TestHandleLogs_MappingFilter verifies the parallel ?mapping= filter.
func TestHandleLogs_MappingFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	ringHandler := proxy.NewRingHandler(logger.Handler(), logSink)
	logger = slog.New(ringHandler)
	logger.Info("dispatch request", "mapping", "opus", "provider", "alpha")
	logger.Info("dispatch request", "mapping", "haiku", "provider", "beta")

	req := httptest.NewRequest(http.MethodGet, "/logs?mapping=opus", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "opus") {
		t.Errorf("expected opus mapping in body; got: %s", body)
	}
	if strings.Contains(body, "haiku") {
		t.Errorf("body should NOT contain haiku mapping; got: %s", body)
	}
}

// TestLastResponderEndpoint verifies the GET /v1/mappings/last-responders
// route returns a JSON map of mapping-name → responder-index.
func TestLastResponderEndpoint(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings:  map[string]config.Mapping{"q": {ProviderName: "nim", ModelString: "m"}},
	}
	lr := proxy.NewLastResponder()
	lr.Record("q", 2)

	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		Cfg:           cfg,
		LastResponder: lr,
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/v1/mappings/last-responders", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rec.Body.String())
	}
	if resp["q"] != 2 {
		t.Errorf("resp[q] = %d, want 2", resp["q"])
	}
}

// TestRouteStepHasAriaAndRole verifies §2.8: every rendered `.route-step`
// carries both an `aria-label` attribute and `role="listitem"`.
// REMOVED in Phase 4: the compact routing table replaces the old card UI
// that carried route-step elements. The dashboard drawer still renders
// route steps (see mapping-drawer.html); coverage there is owned by other
// tests.
