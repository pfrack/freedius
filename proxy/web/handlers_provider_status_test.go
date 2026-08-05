package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestProviderStatus_Unknown verifies that a provider with no traffic data
// renders an "Unknown" badge.
func TestProviderStatus_Unknown(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, "badge--unknown") {
		t.Errorf("expected Unknown badge for provider with no traffic; got: %s", body)
	}
	if !strings.Contains(body, "Unknown") {
		t.Errorf("expected 'Unknown' label in badge; got: %s", body)
	}
}

// TestProviderStatus_Healthy verifies that a provider with successful traffic
// renders a "Healthy" badge.
func TestProviderStatus_Healthy(t *testing.T) {
	// Emit events through a bus to populate stats.
	bus := proxy.NewEventBus(10)
	now := time.Now()
	sc := proxy.NewStatsCollector(bus)
	bus.Emit(proxy.RequestEvent{
		Model:           "test-map",
		MatchedProvider: "nim",
		Status:          200,
		Latency:         100 * time.Millisecond,
		Timestamp:       now,
	})
	// Give the collector goroutine time to process.
	time.Sleep(50 * time.Millisecond)

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{},
	}
	h := &eventstream.Handlers{
		Bus:   proxy.NewEventBus(1),
		Cfg:   cfg,
		Stats: sc,
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, "badge--healthy") {
		t.Errorf("expected Healthy badge for provider with successful traffic; got: %s", body)
	}
}

// TestProviderDetails_Expandable verifies that technical fields (Base URL,
// API Key Env, Protocol) are wrapped in a <details> element for expandable
// disclosure.
func TestProviderDetails_Expandable(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.example.com/v1",
				DefaultAPIKeyEnv: "EXAMPLE_API_KEY",
				Protocol:         "openai",
			},
		},
		Mappings: map[string]config.Mapping{},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, `<details class="provider-details">`) {
		t.Errorf("expected expandable <details> element for provider technical fields; got: %s", body)
	}
	if !strings.Contains(body, `<summary class="provider-details__summary">`) {
		t.Errorf("expected <summary> toggle for expandable details; got: %s", body)
	}
}

// TestProvider_TestConnectionButton verifies that each provider row includes
// a "Test" button that POSTs to the models refresh endpoint.
func TestProvider_TestConnectionButton(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, `hx-post="/v1/providers/nim/models/refresh"`) {
		t.Errorf("expected Test button to POST to models refresh endpoint; got: %s", body)
	}
}

// TestProviderStatus_Degraded verifies that a provider with >50% error rate
// renders a "Degraded" badge.
func TestProviderStatus_Degraded(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{},
	}

	bus := proxy.NewEventBus(20)
	sc := proxy.NewStatsCollector(bus)

	// Emit 6 error events out of last 10 → 60% error rate → degraded.
	now := time.Now()
	for i := 0; i < 6; i++ {
		bus.Emit(proxy.RequestEvent{
			Model:           "test-map",
			MatchedProvider: "nim",
			Status:          500,
			ErrorMessage:    "upstream error",
			Timestamp:       now.Add(time.Duration(i) * time.Second),
		})
	}
	for i := 6; i < 10; i++ {
		bus.Emit(proxy.RequestEvent{
			Model:           "test-map",
			MatchedProvider: "nim",
			Status:          200,
			Timestamp:       now.Add(time.Duration(i) * time.Second),
		})
	}
	time.Sleep(50 * time.Millisecond)

	h := &eventstream.Handlers{
		Bus:   proxy.NewEventBus(1),
		Cfg:   cfg,
		Stats: sc,
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, "badge--degraded") {
		t.Errorf("expected Degraded badge for provider with >50%% errors; got: %s", body)
	}
}
