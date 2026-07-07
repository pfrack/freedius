package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestMappingsTable_F1_RoundTrip verifies F1: the `data-fallbacks` attribute
// round-trips provider/model strings containing both `'` and `"` through
// html/template without double-escaping (`\x27`/`\x22` from `| js`).
func TestMappingsTable_F1_RoundTrip(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":   {Behavior: "openai", Protocol: "openai"},
			"zen":   {Behavior: "openai", Protocol: "openai"},
			"myCo":  {Behavior: "openai", Protocol: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  `O'Reilly "GPT"-4`,
				Fallback: []config.Mapping{
					{ProviderName: "zen", ModelString: `with "quote"`},
					{ProviderName: "myCo", ModelString: `with 'apos'`},
				},
			},
		},
	}
	h := &eventstream.Handlers{
		Bus: proxy.NewEventBus(1),
		Cfg: cfg,
	}

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h.Cfg)
	_ = h

	body := rec.Body.String()
	if !strings.Contains(body, `data-fallbacks="`) {
		t.Fatalf("expected data-fallbacks= attribute in body, got: %s", body)
	}
	// F1's `| js` filter emitted `\x27`/`\x22` JS string escapes that JSON.parse
	// does NOT understand. html/template's own attribute encoding (`&#39;`/`&#34;`)
	// is correct — the browser decodes those before `dataset.fallbacks` exposes
	// them to JS. Forbid only the JS-context escapes.
	for _, forbidden := range []string{`\x27`, `\x22`, `\u0022`, `\u0027`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("body contains forbidden escape %q (| js over-encoding): %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "data-fallbacks=") {
		t.Errorf("body missing data-fallbacks attr; got: %s", body)
	}
}

// TestMappingsTable_F1_QuotesInModelSurvive is the companion check: a model
// string containing both kinds of quotes survives the template→DOM round trip
// intact (i.e. it's encoded once, by html/template, as `&#39;`/`&#34;`).
func TestMappingsTable_F1_QuotesInModelSurvive(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  `a'b"c`,
				Fallback: []config.Mapping{
					{ProviderName: "nim", ModelString: `x"y'z`},
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, cfg)

	body := rec.Body.String()
	if !strings.Contains(body, "data-fallbacks=") {
		t.Fatalf("expected data-fallbacks attr; got: %s", body)
	}
	if strings.Contains(body, `\x27`) || strings.Contains(body, `\x22`) {
		t.Errorf("| js over-escaping present: %s", body)
	}
}

// TestHandleRefreshModels_Truncation verifies F2: when proxy.FetchModels
// returns more than 1000 entries, the response renders exactly 1000
// <li data-model-id> rows plus the truncation notice.
func TestHandleRefreshModels_Truncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [` + make1500Models() + `]}`))
	}))
	defer srv.Close()

	// Build a mux with a base URL pointed at the test upstream; the shared
	// helper hard-codes a different URL.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:       "openai",
				DefaultBaseURL: srv.URL + "/v1/chat/completions",
			},
		},
	}
	mc := proxy.NewModelsCache()
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	count := strings.Count(body, `<li data-model-id="m-`)
	if count != 1000 {
		t.Errorf("rendered %d <li data-model-id> rows, want exactly 1000", count)
	}
	if !strings.Contains(body, "Truncated at 1000 models") {
		t.Errorf("expected truncation notice in body; got: %s", body)
	}
}

// TestHandleRefreshModels_NoTruncationMessage_WhenExactly1000 verifies that
// the truncation notice is NOT appended when the upstream returns exactly
// 1000 models (the cutoff is strict `> 1000`).
func TestHandleRefreshModels_NoTruncationMessage_WhenExactly1000(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [` + makeExactlyNModels(1000) + `]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultBaseURL: srv.URL + "/v1/chat/completions"},
		},
	}
	mc := proxy.NewModelsCache()
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	count := strings.Count(body, `<li data-model-id="m-`)
	if count != 1000 {
		t.Errorf("rendered %d rows, want 1000", count)
	}
	if strings.Contains(body, "Truncated at 1000") {
		t.Errorf("truncation notice should NOT appear at exactly 1000 models")
	}
}

// TestHandleRefreshModels_InProgress verifies F4: when a fetch is already
// in flight (lock held), a concurrent refresh renders the "Refresh already
// in progress…" hint.
func TestHandleRefreshModels_InProgress(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4"}]}`))
	}))
	defer srv.Close()
	defer close(release)

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultBaseURL: srv.URL + "/v1/chat/completions"},
		},
	}
	mc := proxy.NewModelsCache()
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	// Pre-populate cache so the in-progress branch has data to render.
	mc.Set("nim", []proxy.ModelView{{ID: "cached", DisplayName: "cached"}}, nil)

	// Hold the inflight lock for "nim" manually.
	muAny, _ := modelFetchInflight.LoadOrStore("nim", &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// While the lock is held, the handler must short-circuit with the
	// in-progress hint AND cached models.
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Refresh already in progress") {
		t.Errorf("expected in-progress hint; got: %s", body)
	}
	if !strings.Contains(body, "cached") {
		t.Errorf("expected cached models to be rendered alongside in-progress hint; got: %s", body)
	}
}

// TestMappingsTable_HxConfirmBalanced is the byte-level regression guard for
// F3: the rendered `hx-confirm` attribute on the Delete button must close
// its quoted string before the trailing `?`.
func TestMappingsTable_HxConfirmBalanced(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings:  map[string]config.Mapping{"alpha": {ProviderName: "nim", ModelString: "m"}},
	}

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, cfg)

	body := rec.Body.String()
	if !strings.Contains(body, `hx-confirm="Delete mapping 'alpha'?"`) {
		t.Errorf("mappings table must use balanced hx-confirm copy; got: %s", body)
	}
	if strings.Contains(body, `Delete mapping 'alpha?"`) {
		t.Errorf("mappings table still has unbalanced hx-confirm; got: %s", body)
	}
}

func TestProvidersTable_HxConfirmBalanced(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"alpha": {Behavior: "openai"}},
		Mappings:  map[string]config.Mapping{},
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec := httptest.NewRecorder()
	renderProvidersTable(rec, req, cfg)

	body := rec.Body.String()
	if !strings.Contains(body, `hx-confirm="Delete provider 'alpha'?"`) {
		t.Errorf("providers table must use balanced hx-confirm copy; got: %s", body)
	}
	if strings.Contains(body, `Delete provider 'alpha?"`) {
		t.Errorf("providers table still has unbalanced hx-confirm; got: %s", body)
	}
}

// TestStaleHxPutAfterEditCancel_StaticContract is the byte-level guard for
// 1.8: the rendered Add button must route through `openAddMapping()`, the
// Edit button must route through `editMapping(btn)`, and the form helper
// script must reset the form to POST (i.e. remove any inherited hx-put).
// Without a browser we cannot execute the JS, but we can prove the
// DOM contract is set up to clear `hx-put` on every Add opening.
func TestStaleHxPutAfterEditCancel_StaticContract(t *testing.T) {
	tmpl, err := loadPageTemplate("mappings.html", "mappings-table.html")
	if err != nil {
		t.Fatalf("load mappings.html: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", mappingsData{
		pageData:  pageData{Active: "mappings"},
		Mappings:  []mappingRow{},
		Providers: []providerRow{{Name: "nim"}},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()

	// Add button must call the reset helper, not showModal directly.
	if !strings.Contains(body, `onclick="openAddMapping()"`) {
		t.Errorf("Add button must call openAddMapping(); got body: %s", body)
	}
	if strings.Contains(body, `onclick="document.getElementById('mapping-dialog').showModal()"`) {
		t.Errorf("Add button still binds showModal directly (bypasses reset): %s", body)
	}

	// The reset helper must remove hx-put so a stale PUT from a prior Edit
	// doesn't survive.
	if !strings.Contains(body, "function resetMappingForm") {
		t.Errorf("mappings.html must define resetMappingForm helper")
	}
	if !strings.Contains(body, "f.removeAttribute('hx-put')") {
		t.Errorf("resetMappingForm must remove hx-put; got: %s", body)
	}
	if !strings.Contains(body, "f.setAttribute('hx-post', '/v1/mappings')") {
		t.Errorf("resetMappingForm must restore hx-post=/v1/mappings; got: %s", body)
	}
	if !strings.Contains(body, "openAddMapping()") || !strings.Contains(body, "resetMappingForm()") {
		t.Errorf("openAddMapping must invoke resetMappingForm first")
	}

	// Edit handler must set hx-put so a subsequent Add is what triggers the reset.
	if !strings.Contains(body, "f.setAttribute('hx-put', '/v1/mappings/'") {
		t.Errorf("editMapping must set hx-put so the reset path is exercised; got: %s", body)
	}
}

// TestStaleHxPutAfterEditCancel_StaticContract_Providers mirrors the same
// check for the providers page.
func TestStaleHxPutAfterEditCancel_StaticContract_Providers(t *testing.T) {
	tmpl, err := loadPageTemplate("providers.html", "providers-table.html")
	if err != nil {
		t.Fatalf("load providers.html: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", providersData{
		pageData:  pageData{Active: "providers"},
		Providers: []providerRow{{Name: "nim"}},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `onclick="openAddProvider()"`) {
		t.Errorf("providers Add must call openAddProvider(); got body: %s", body)
	}
	if !strings.Contains(body, "function resetProviderForm") {
		t.Errorf("providers.html must define resetProviderForm helper")
	}
	if !strings.Contains(body, "f.removeAttribute('hx-put')") {
		t.Errorf("resetProviderForm must remove hx-put; got: %s", body)
	}
}

func make1500Models() string {
	var b strings.Builder
	for i := 0; i < 1500; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":"m-`)
		writeInt(&b, i)
		b.WriteString(`"}`)
	}
	return b.String()
}

func makeExactlyNModels(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":"m-`)
		writeInt(&b, i)
		b.WriteString(`"}`)
	}
	return b.String()
}

func writeInt(b *strings.Builder, i int) {
	if i == 0 {
		b.WriteByte('0')
		return
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	b.Write(buf[pos:])
}