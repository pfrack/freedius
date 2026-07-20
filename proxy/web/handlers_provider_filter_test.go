package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

// TestMappingsProviderFilter_SubstringMatch verifies that the provider filter
// does a case-insensitive substring match against provider names.
func TestMappingsProviderFilter_SubstringMatch(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":        {Behavior: "openai"},
			"openai-nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {ProviderName: "openai-nim", ModelString: "m2"},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings?provider=nim", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// Both mappings should appear because "nim" is a substring of "nim" and "openai-nim".
	if !strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected mapping 'q' in body; got: %s", body)
	}
	if !strings.Contains(body, `class="route-card__name">r</h3>`) {
		t.Errorf("expected mapping 'r' in body; got: %s", body)
	}
}

// TestMappingsProviderFilter_FallbackMatch verifies that the provider filter
// matches fallback providers, not just primary providers.
func TestMappingsProviderFilter_FallbackMatch(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"alpha": {Behavior: "openai"},
			"beta":  {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "alpha",
				ModelString:  "m1",
				Fallback: []config.Mapping{
					{ProviderName: "beta", ModelString: "fb1"},
				},
			},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings?provider=beta", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// The mapping should appear because "beta" is a fallback provider.
	if !strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected mapping 'q' in body (fallback match); got: %s", body)
	}
}

// TestMappingsProviderFilter_CaseInsensitive verifies that the provider filter
// is case-insensitive.
func TestMappingsProviderFilter_CaseInsensitive(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings?provider=NIM", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// The mapping should appear because the filter is case-insensitive.
	if !strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected mapping 'q' in body (case-insensitive match); got: %s", body)
	}
}

// TestMappingsProviderFilter_EmptyShowsAll verifies that an empty provider filter
// shows all mappings.
func TestMappingsProviderFilter_EmptyShowsAll(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
			"zen": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {ProviderName: "zen", ModelString: "m2"},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// Both mappings should appear when no filter is set.
	if !strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected mapping 'q' in body; got: %s", body)
	}
	if !strings.Contains(body, `class="route-card__name">r</h3>`) {
		t.Errorf("expected mapping 'r' in body; got: %s", body)
	}
}

// TestMappingsProviderFilter_NoMatchShowsEmpty verifies that a non-matching
// provider filter shows an empty state.
func TestMappingsProviderFilter_NoMatchShowsEmpty(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings?provider=nonexistent", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// Should show empty state because no mappings match the filter.
	if !strings.Contains(body, `class="empty-state"`) {
		t.Errorf("expected empty-state for non-matching filter; got: %s", body)
	}
	if strings.Contains(body, `class="route-card__name">q</h3>`) {
		t.Errorf("expected no mappings for non-matching filter; got: %s", body)
	}
}
