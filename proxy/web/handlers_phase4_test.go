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

// newPhase4Handlers wraps a config with the dev-leaning fields Phase 4
// tests need (Stats nil-safe, no LastResponder needed for most tests).
func newPhase4Handlers(cfg *config.Config) *eventstream.Handlers {
	return &eventstream.Handlers{
		Bus:           proxy.NewEventBus(1),
		LogSink:       proxy.NewLogSink(1),
		Cfg:           cfg,
		LastResponder: proxy.NewLastResponder(),
	}
}

func TestMappingsPhase4_Filters(t *testing.T) {
	// Use a fixed env var so the "active" status filter is deterministic.
	t.Setenv("PHASE4_TEST_KEY", "present")

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":         {Behavior: "openai", DefaultAPIKeyEnv: "PHASE4_TEST_KEY"},
			"groq":        {Behavior: "openai", DefaultAPIKeyEnv: "PHASE4_TEST_KEY"},
			"missing-key": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"alpha": {ProviderName: "nim", ModelString: "m1"},
			"beta": {
				ProviderName: "groq",
				ModelString:  "m2",
				Fallback:     []config.Mapping{{ProviderName: "nim", ModelString: "fb1"}},
			},
			"gamma": {ProviderName: "missing-key", ModelString: "m3"},
			"aleph": {ProviderName: "nim", ModelString: "m4"},
		},
	}

	tests := []struct {
		name        string
		query       string
		mustContain []string
		mustExclude []string
	}{
		{
			name:        "search substring match",
			query:       "?search=alph",
			mustContain: []string{"alpha"},
			mustExclude: []string{"beta", "gamma", "aleph"},
		},
		{
			name:        "search case-insensitive",
			query:       "?search=ALPH",
			mustContain: []string{"alpha"},
			mustExclude: []string{"beta", "gamma", "aleph"},
		},
		{
			name:        "search broad prefix matches both alpha and aleph",
			query:       "?search=al",
			mustContain: []string{"alpha", "aleph"},
			mustExclude: []string{"beta", "gamma"},
		},
		{
			name:        "status active filters by env presence",
			query:       "?status=active",
			mustContain: []string{"alpha", "beta", "aleph"},
			mustExclude: []string{"gamma"},
		},
		{
			name:        "status inactive filters by env absence",
			query:       "?status=inactive",
			mustContain: []string{"gamma"},
			mustExclude: []string{"alpha", "beta", "aleph"},
		},
		{
			name:        "has_fallback true keeps only mappings with fallback",
			query:       "?has_fallback=true",
			mustContain: []string{"beta"},
			mustExclude: []string{"alpha", "gamma", "aleph"},
		},
		{
			name:        "has_fallback false keeps only mappings without fallback",
			query:       "?has_fallback=false",
			mustContain: []string{"alpha", "gamma", "aleph"},
			mustExclude: []string{"beta"},
		},
		{
			name:        "combined search + status",
			query:       "?search=al&status=active",
			mustContain: []string{"alpha", "aleph"},
			mustExclude: []string{"beta", "gamma"},
		},
		{
			name:        "combined all filters",
			query:       "?provider=nim&status=active&has_fallback=false",
			mustContain: []string{"alpha", "aleph"},
			mustExclude: []string{"beta", "gamma"},
		},
		{
			name:        "no filters shows all",
			query:       "",
			mustContain: []string{"alpha", "beta", "gamma", "aleph"},
			mustExclude: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newPhase4Handlers(cfg)
			req := httptest.NewRequest(http.MethodGet, "/mappings"+tt.query, nil)
			rec := httptest.NewRecorder()
			renderMappingsTable(rec, req, h)
			body := rec.Body.String()

			for _, want := range tt.mustContain {
				if !strings.Contains(body, "<strong>"+want+"</strong>") {
					t.Errorf("filter %q should include %q; body: %s", tt.query, want, body)
				}
			}
			for _, dont := range tt.mustExclude {
				if strings.Contains(body, "<strong>"+dont+"</strong>") {
					t.Errorf("filter %q should exclude %q; body: %s", tt.query, dont, body)
				}
			}
		})
	}
}

func TestMappingsPhase4_HTMXFragmentEndpoint(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newPhase4Handlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings?provider=nim", nil)
	// Even without HX-Request, the renderMappingsTable path returns the
	// fragment directly (it's the same code path used by handleMappings
	// when HX-Request is true). Verify it does NOT include the layout.
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, `id="mappings-table-container"`) {
		t.Errorf("HTMX fragment endpoint must return the table container; got: %s", body)
	}
	if strings.Contains(body, "<html") {
		t.Errorf("HTMX fragment endpoint must NOT include layout; got: %s", body)
	}
	if strings.Contains(body, "freedius") {
		t.Errorf("HTMX fragment endpoint must NOT include layout chrome; got: %s", body)
	}
}

func TestMappingsPhase4_FilterInputsPrefilled(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newPhase4Handlers(cfg)

	// Build a recorder via the full page handler so the filter values
	// round-trip into the rendered HTML pre-fills.
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
	req := httptest.NewRequest(http.MethodGet,
		"/mappings?search=q&provider=nim&status=active&has_fallback=true", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{
		`value="q"`,                    // search input
		`<option value="nim" selected`, // provider select
		`value="active" selected`,      // status select
		`checked`,                      // has_fallback checkbox
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in pre-filled filter inputs; body: %s", want, body)
		}
	}
}

func TestMappingsPhase4_DeleteConfirmationDialog(t *testing.T) {
	// The new Phase 4 template must drive a confirmation dialog instead
	// of the legacy hx-confirm. The Delete button carries the mapping
	// name; the dialog is rendered by mappings.html.
	tmpl, err := loadPageTemplate("mappings.html", "mappings-routing-table.html")
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf strings.Builder
	err = tmpl.ExecuteTemplate(&buf, "layout", mappingsData{
		pageData:  pageData{Active: "mappings"},
		Providers: []providerRow{{Name: "nim"}},
		Mappings: []mappingRow{
			{Name: "alpha", ProviderName: "nim", Model: "m1"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()

	// The table must NOT use the legacy hx-confirm path.
	if strings.Contains(body, `hx-confirm="Delete mapping`) {
		t.Errorf("Phase 4 must remove hx-confirm; got: %s", body)
	}
	// The Delete button must carry data-mapping-name for the dialog JS.
	if !strings.Contains(body, `data-mapping-name="alpha"`) {
		t.Errorf("Delete button must carry data-mapping-name; got: %s", body)
	}
	// The confirmation dialog must exist with the expected structure.
	for _, want := range []string{
		`<dialog id="delete-confirm-dialog"`,
		`hx-delete="/v1/mappings/__pending__"`,
		`id="delete-mapping-name"`,
		`function confirmDeleteMapping`,
		`function closeDeleteDialog`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("delete dialog must contain %q; got: %s", want, body)
		}
	}
}

func TestMappingsPhase4_EmptyStateDistinguishesNoMappingsVsFilters(t *testing.T) {
	// Configuration: some mappings exist, but a filter excludes everything.
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newPhase4Handlers(cfg)

	t.Run("filters exclude everything → 'Clear filters' CTA", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mappings?provider=nonexistent", nil)
		rec := httptest.NewRecorder()
		renderMappingsTable(rec, req, h)
		body := rec.Body.String()

		if !strings.Contains(body, "No mappings match the current filters") {
			t.Errorf("expected filter-excluded empty state copy; got: %s", body)
		}
		if !strings.Contains(body, "clearMappingsFilters") {
			t.Errorf("expected Clear filters button; got: %s", body)
		}
		if strings.Contains(body, "Add your first mapping") {
			t.Errorf("must NOT show 'Add your first mapping' when mappings exist; got: %s", body)
		}
	})

	t.Run("zero mappings → 'Add your first mapping' CTA", func(t *testing.T) {
		emptyCfg := &config.Config{
			Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
			Mappings:  map[string]config.Mapping{},
		}
		emptyH := newPhase4Handlers(emptyCfg)
		req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
		rec := httptest.NewRecorder()
		renderMappingsTable(rec, req, emptyH)
		body := rec.Body.String()

		if !strings.Contains(body, "Add your first mapping") {
			t.Errorf("expected zero-mappings CTA; got: %s", body)
		}
		if !strings.Contains(body, "openAddMapping") {
			t.Errorf("expected openAddMapping CTA handler; got: %s", body)
		}
		if strings.Contains(body, "Clear filters") {
			t.Errorf("must NOT show Clear filters when no mappings exist; got: %s", body)
		}
	})
}
