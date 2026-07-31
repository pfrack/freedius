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
