package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestIndexHandler_ReturnsMappings verifies that the dashboard handler returns
// a routing table with mapping names and routes.
func TestIndexHandler_ReturnsMappings(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {ProviderName: "nim", ModelString: "m2"},
		},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Should contain routing table.
	if !strings.Contains(body, `class="routing-table"`) {
		t.Errorf("expected routing-table in body; got first 500 chars: %s", body[:min(500, len(body))])
	}
	// Should contain mapping names in the table.
	if !strings.Contains(body, "nim / m1") && !strings.Contains(body, "nim / m2") {
		t.Errorf("expected mapping routes in body")
	}
}

// TestIndexHandler_ReturnsProviders verifies that the dashboard handler returns
// provider health badges.
func TestIndexHandler_ReturnsProviders(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", Protocol: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {ProviderName: "nim", ModelString: "m2"},
		},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Should contain provider badge with name.
	if !strings.Contains(body, `class="provider-badge`) {
		t.Errorf("expected provider-badge in body")
	}
	if !strings.Contains(body, "nim") {
		t.Errorf("expected provider 'nim' in body")
	}
}

// TestIndexHandler_EmptyState verifies that the dashboard handler returns
// empty state when no mappings are configured.
func TestIndexHandler_EmptyState(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Should contain empty state for mappings.
	if !strings.Contains(body, `class="empty-state"`) {
		t.Errorf("expected empty-state in body")
	}
	if !strings.Contains(body, "No mappings configured") {
		t.Errorf("expected 'No mappings configured' in body; got first 500: %s", body[:min(500, len(body))])
	}
}

// TestIndexHandler_StatsPreserved verifies that the dashboard handler
// contains the health strip with Uptime and Endpoint.
func TestIndexHandler_StatsPreserved(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Should contain health strip.
	if !strings.Contains(body, `class="health-strip"`) {
		t.Errorf("expected health-strip in body")
	}
	// Should contain Uptime.
	if !strings.Contains(body, "Uptime") {
		t.Errorf("expected Uptime in body")
	}
	// Should contain Endpoint.
	if !strings.Contains(body, "Endpoint") {
		t.Errorf("expected Endpoint in body")
	}
	// Should contain health state.
	if !strings.Contains(body, "Healthy") {
		t.Errorf("expected Healthy state in body")
	}
}

// TestDashboard_AttentionPanel verifies the attention panel appears when
// there are config issues (missing API key).
func TestDashboard_AttentionPanel(t *testing.T) {
	// Provider requires an env var that doesn't exist.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NONEXISTENT_KEY_FOR_TEST_XYZ"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "model-1"},
		},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, `class="attention-panel"`) {
		t.Errorf("expected attention-panel when API key is missing")
	}
	if !strings.Contains(body, "missing API key") {
		t.Errorf("expected 'missing API key' alert message in body")
	}
}

// TestDashboard_NoAttentionPanel verifies the attention panel is absent when
// there are no issues.
func TestDashboard_NoAttentionPanel(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "model-1"},
		},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	if strings.Contains(body, `class="attention-panel"`) {
		t.Errorf("expected NO attention-panel when everything is configured correctly")
	}
}

// TestMappingDrawer covers the GET /v1/mappings/{name}/detail endpoint:
//   - happy path returns 200 + an HTML fragment with mapping details,
//   - unknown mapping returns a 404 JSON error,
//   - the rendered fragment includes the route chain (primary + fallbacks),
//   - stats counters from StatsCollector are rendered when present.
func TestMappingDrawer(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":  {Behavior: "openai", Protocol: "openai"},
			"groq": {Behavior: "openai", Protocol: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {
				ProviderName: "nim",
				ModelString:  "anthropic/claude-3-5-haiku",
				Fallback: []config.Mapping{
					{ProviderName: "groq", ModelString: "llama-3.1-70b"},
				},
			},
		},
	}

	t.Run("returns fragment with route chain for known mapping", func(t *testing.T) {
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

		req := httptest.NewRequest(http.MethodGet, "/v1/mappings/haiku/detail", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
		body := rec.Body.String()

		// Mapping name + primary route must be rendered.
		for _, want := range []string{
			"haiku",
			"anthropic/claude-3-5-haiku",
			"nim",
			"groq",
			"llama-3.1-70b",
			"route-step--primary",
			"route-step--fallback",
			"Edit on Mappings page",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("expected %q in drawer fragment; body: %s", want, body)
			}
		}
	})

	t.Run("returns 404 JSON for unknown mapping", func(t *testing.T) {
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

		req := httptest.NewRequest(http.MethodGet, "/v1/mappings/does-not-exist/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if !strings.Contains(rec.Body.String(), "not_found") {
			t.Errorf("expected not_found in JSON body; got %s", rec.Body.String())
		}
	})

	t.Run("reflects stats counters from StatsCollector", func(t *testing.T) {
		bus := proxy.NewEventBus(10)
		stats := proxy.NewStatsCollector(bus)
		// Rebuild handlers using the bus the collector subscribes to. The
		// shared bus pattern is established in Phase 1; here we just need the
		// collector's snapshot to surface non-zero counters in the fragment.
		h := &eventstream.Handlers{
			Bus:           bus,
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
			Stats:         stats,
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

		// Inject three events so counters and timestamps are non-zero. The
		// collector subscribes asynchronously; a short sleep lets the
		// internal goroutine drain the buffered channel before we snapshot.
		now := time.Now()
		for i := 0; i < 3; i++ {
			bus.Emit(proxy.RequestEvent{
				Model:           "haiku",
				Provider:        "nim",
				Status:          200,
				MatchedProvider: "nim",
				MatchedModel:    "anthropic/claude-3-5-haiku",
				Timestamp:       now.Add(time.Duration(i) * time.Second),
				Latency:         42 * time.Millisecond,
			})
		}
		time.Sleep(50 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/v1/mappings/haiku/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()

		if !strings.Contains(body, ">3<") {
			t.Errorf("expected RequestCount=3 in drawer stats; body: %s", body)
		}
		if strings.Contains(body, "No traffic") {
			t.Errorf("expected last activity to reflect recent traffic; body: %s", body)
		}
	})
}
