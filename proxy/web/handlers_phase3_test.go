package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

// TestEmptyState_Providers verifies §3.4: an empty providers list renders
// an `.empty-state` block with the CTA copy.
func TestEmptyState_Providers(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, cfg)
	body := rec.Body.String()

	if !strings.Contains(body, `class="empty-state"`) {
		t.Errorf("empty providers list must render empty-state; got: %s", body)
	}
	if !strings.Contains(body, "Add your first provider") {
		t.Errorf("empty-state CTA copy missing; got: %s", body)
	}
	if !strings.Contains(body, "openAddProvider") {
		t.Errorf("empty-state CTA must invoke openAddProvider(); got: %s", body)
	}
}

// TestEmptyState_Mappings verifies §3.5: empty mappings list renders an
// empty-state with a CTA referencing `mapping-dialog`.
func TestEmptyState_Mappings(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings:  map[string]config.Mapping{},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, `class="empty-state"`) {
		t.Errorf("empty mappings list must render empty-state; got: %s", body)
	}
	if !strings.Contains(body, "Add your first mapping") {
		t.Errorf("empty-state CTA copy missing; got: %s", body)
	}
	if !strings.Contains(body, "openAddMapping") {
		t.Errorf("empty-state CTA must invoke openAddMapping(); got: %s", body)
	}
}

// TestNonEmptyState_NoEmptyCTA verifies the inverse: when there are entries,
// the empty-state block is NOT rendered.
func TestNonEmptyState_NoEmptyCTA(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings:  map[string]config.Mapping{"q": {ProviderName: "nim", ModelString: "m"}},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if strings.Contains(body, `class="empty-state"`) {
		t.Errorf("non-empty mappings list must NOT render empty-state; got: %s", body)
	}
	if !strings.Contains(body, `class="route-card"`) {
		t.Errorf("non-empty mappings list must render cards; got: %s", body)
	}
}

// TestErrorMessageInResponse verifies §3.6: a malformed provider form
// returns a JSON ValidationError whose body has fields.name and
// fields.behavior keys. (This is the contract the global JS listener
// will consume to render inline errors.)
func TestErrorMessageInResponse(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	body := "name=&behavior=invalid"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"fields":{`) {
		t.Errorf("response missing fields map; got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"required"`) {
		t.Errorf("response missing fields.name=required; got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"behavior"`) {
		t.Errorf("response missing fields.behavior; got: %s", rec.Body.String())
	}
}

// TestSaveButtonHasDisabledEltAndIndicator verifies §3.7: each Save button
// carries hx-disabled-elt="this" and a sibling .htmx-indicator span.
func TestSaveButtonHasDisabledEltAndIndicator(t *testing.T) {
	for _, page := range []string{"mappings.html", "providers.html"} {
		t.Run(page, func(t *testing.T) {
			tmpl, err := loadPageTemplate(page, strings.TrimSuffix(page, ".html")+"-table.html")
			if err != nil {
				t.Fatalf("load %s: %v", page, err)
			}
			var buf strings.Builder
			if err := tmpl.ExecuteTemplate(&buf, "layout", mappingsData{
				pageData: pageData{Active: strings.TrimSuffix(page, ".html")},
				Providers: []providerRow{
					{Name: "nim"},
				},
				Mappings: []mappingRow{},
			}); err != nil {
				t.Fatalf("render: %v", err)
			}
			body := buf.String()
			if !strings.Contains(body, `hx-disabled-elt="this"`) {
				t.Errorf("%s Save button must carry hx-disabled-elt=\"this\"; got: %s", page, body)
			}
			if !strings.Contains(body, `class="htmx-indicator"`) {
				t.Errorf("%s Save button must carry sibling .htmx-indicator; got: %s", page, body)
			}
		})
	}
}

// TestLogsEmptyStateCopy verifies §3.4 (logs side): empty logs render the
// "Waiting for log events…" hint.
func TestLogsEmptyStateCopy(t *testing.T) {
	tmpl, err := loadPageTemplate("logs.html")
	if err != nil {
		t.Fatalf("load logs.html: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", logsData{
		pageData: pageData{Active: "logs"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "Waiting for log events") {
		t.Errorf("empty logs must show 'Waiting for log events' hint; got: %s", body)
	}
}

// TestToastRegionInLayout verifies §3.1: layout.html has a #toast-region
// element ready to receive toasts.
func TestToastRegionInLayout(t *testing.T) {
	tmpl, err := loadPageTemplate("index.html")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", indexData{
		pageData: pageData{Active: "index"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `id="toast-region"`) {
		t.Errorf("layout must include #toast-region; got: %s", body)
	}
	if !regexp.MustCompile(`aria-live="polite"`).MatchString(body) {
		t.Errorf("toast-region must declare aria-live=polite for screen readers; got: %s", body)
	}
}

// TestGlobalAfterRequestListenerWired verifies §3.2: the layout includes
// an `htmx:afterRequest` listener on document.body.
func TestGlobalAfterRequestListenerWired(t *testing.T) {
	tmpl, err := loadPageTemplate("index.html")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", indexData{
		pageData: pageData{Active: "index"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "htmx:afterRequest") {
		t.Errorf("layout must include global htmx:afterRequest listener; got: %s", body)
	}
	if !strings.Contains(body, "showToast") {
		t.Errorf("layout script must define showToast; got: %s", body)
	}
}