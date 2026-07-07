package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

// TestProvidersTable_MappingCountLink verifies that the MappingCount column
// shows a link to /mappings?provider=<name> when the count is > 0.
func TestProvidersTable_MappingCountLink(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {ProviderName: "nim", ModelString: "m2"},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, cfg)
	body := rec.Body.String()

	// Should contain a link to /mappings?provider=nim with text "2".
	if !strings.Contains(body, `href="/mappings?provider=nim"`) {
		t.Errorf("expected link to /mappings?provider=nim; got: %s", body)
	}
	if !strings.Contains(body, `>2</a>`) {
		t.Errorf("expected mapping count '2' as link text; got: %s", body)
	}
}

// TestProvidersTable_ZeroMappingCount verifies that when a provider has 0
// mappings, the MappingCount column shows a muted "0" without a link.
func TestProvidersTable_ZeroMappingCount(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{},
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, cfg)
	body := rec.Body.String()

	// Should contain a muted "0" span, not a link.
	if !strings.Contains(body, `class="text-muted">0</span>`) {
		t.Errorf("expected muted '0' for zero mapping count; got: %s", body)
	}
	// Should NOT contain a link to /mappings.
	if strings.Contains(body, `href="/mappings?provider=nim"`) {
		t.Errorf("expected no link for zero mapping count; got: %s", body)
	}
}
