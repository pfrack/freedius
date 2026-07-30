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
// mapping cards when mappings are configured.
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

	// Should contain mapping cards.
	if !strings.Contains(body, `class="route-card"`) {
		t.Errorf("expected route-card in body; got: %s", body)
	}
	// Should contain mapping names.
	if !strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected mapping 'q' in body; got: %s", body)
	}
	if !strings.Contains(body, `class="route-card__name">r</h3>`) {
		t.Errorf("expected mapping 'r' in body; got: %s", body)
	}
}

// TestIndexHandler_ReturnsProviders verifies that the dashboard handler returns
// provider summary with mapping-count links.
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

	// Should contain provider name in providers-summary chip.
	if !strings.Contains(body, `providers-summary__chip`) {
		t.Errorf("expected providers-summary__chip in body; got: %s", body)
	}
	if !strings.Contains(body, "nim") {
		t.Errorf("expected provider 'nim' in body; got: %s", body)
	}
	// Should contain protocol badge.
	if !strings.Contains(body, `class="badge badge--protocol">openai</span>`) {
		t.Errorf("expected protocol badge 'openai' in body; got: %s", body)
	}
	// Should contain mapping-count link.
	if !strings.Contains(body, `href="/mappings?provider=nim"`) {
		t.Errorf("expected link to /mappings?provider=nim; got: %s", body)
	}
	// Should contain mapping count.
	if !strings.Contains(body, `providers-summary__count">2</span>`) {
		t.Errorf("expected mapping count '2' in body; got: %s", body)
	}
}

// TestIndexHandler_EmptyState verifies that the dashboard handler returns
// empty state when no providers or mappings are configured.
func TestIndexHandler_EmptyState(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
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
		t.Errorf("expected empty-state in body; got: %s", body)
	}
	if !strings.Contains(body, "No mappings yet") {
		t.Errorf("expected 'No mappings yet' in body; got: %s", body)
	}
	// Providers section should not be rendered when no providers exist.
	if strings.Contains(body, `providers-summary`) {
		t.Errorf("expected no providers-summary when empty; got: %s", body)
	}
	// Stats grid should still show zeros.
	if !strings.Contains(body, `class="stats-grid"`) {
		t.Errorf("expected stats-grid in body; got: %s", body)
	}
}

// TestIndexHandler_StatsPreserved verifies that the dashboard handler still
// contains the stats strip with Uptime and Listening On.
func TestIndexHandler_StatsPreserved(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
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

	// Should contain stats strip.
	if !strings.Contains(body, `class="stats-strip"`) {
		t.Errorf("expected stats-strip in body; got: %s", body)
	}
	// Should contain Uptime label.
	if !strings.Contains(body, `class="stats-strip__label">Uptime</span>`) {
		t.Errorf("expected Uptime label in body; got: %s", body)
	}
	// Should contain Listening On label.
	if !strings.Contains(body, `class="stats-strip__label">Listening On</span>`) {
		t.Errorf("expected Listening On label in body; got: %s", body)
	}
}
