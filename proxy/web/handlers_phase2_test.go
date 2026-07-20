package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestMappingRow_PopulatesProtocol verifies §2.4: when a provider has a
// Protocol set, the mapping row carries that Protocol into the rendered HTML
// as a `.badge--protocol` element with the protocol text.
func TestMappingRow_PopulatesProtocol(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", Protocol: "openai"},
			"zen": {Behavior: "openai", Protocol: "anthropic"},
		},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
			"r": {
				ProviderName: "zen",
				ModelString:  "m2",
				Fallback: []config.Mapping{
					{ProviderName: "nim", ModelString: "fb1"},
				},
			},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// Primary of "q" uses nim (openai).
	openaiCount := strings.Count(body, `class="badge badge--protocol route-step__protocol">openai</span>`)
	anthropicCount := strings.Count(body, `class="badge badge--protocol route-step__protocol">anthropic</span>`)
	if openaiCount == 0 {
		t.Errorf("expected openai protocol badge in body; got: %s", body)
	}
	if anthropicCount == 0 {
		t.Errorf("expected anthropic protocol badge in body; got: %s", body)
	}
	if openaiCount+anthropicCount < 3 {
		// Two primaries + at least one fallback badge across q and r.
		t.Errorf("expected >=3 protocol badges total, got %d (openai=%d, anthropic=%d)",
			openaiCount+anthropicCount, openaiCount, anthropicCount)
	}
}

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
func TestRouteStepHasAriaAndRole(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", Protocol: "openai"},
			"zen": {Behavior: "openai", Protocol: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  "m1",
				Fallback: []config.Mapping{
					{ProviderName: "zen", ModelString: "fb1"},
				},
			},
			"r": {ProviderName: "nim", ModelString: "m2"},
		},
	}
	h := newRenderHandlers(cfg)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	// Count .route-step elements (both primary and fallback).
	steps := regexp.MustCompile(`class="route-step[^"]*"`).FindAllString(body, -1)
	if len(steps) < 3 {
		t.Fatalf("expected >=3 .route-step elements (primary+1fb for q, primary for r), got %d", len(steps))
	}
	// Each must have aria-label and role="listitem". The <a> tag may span
	// multiple lines (whitespace between attributes), so capture up to the
	// first ">" after the class="route-step" attribute.
	for _, step := range steps {
		idx := strings.Index(body, step)
		if idx < 0 {
			continue
		}
		start := strings.LastIndex(body[:idx], "<a")
		if start < 0 {
			t.Errorf("could not find <a for step: %s", step)
			continue
		}
		end := strings.Index(body[start:], ">")
		if end < 0 {
			continue
		}
		tag := body[start : start+end+1]
		if !strings.Contains(tag, `aria-label="`) {
			t.Errorf("step missing aria-label: %s", tag)
		}
		if !strings.Contains(tag, `role="listitem"`) {
			t.Errorf("step missing role=listitem: %s", tag)
		}
	}
}

// TestNoResponderClassWhenEmpty verifies §2.9: with no recorded responders,
// no `.route-step--responder` class appears in the rendered output.
func TestNoResponderClassWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings: map[string]config.Mapping{
			"q": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	h := newRenderHandlers(cfg) // LastResponder is empty

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if strings.Contains(body, "route-step--responder") {
		t.Errorf("empty aggregator must not produce --responder class; got: %s", body)
	}
}

// TestResponderClassAppearsWhenRecorded verifies the symmetric case: when a
// responder is recorded for the matching index, the rendered step carries
// the `--responder` class.
func TestResponderClassAppearsWhenRecorded(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
			"zen": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  "m1",
				Fallback: []config.Mapping{
					{ProviderName: "zen", ModelString: "fb1"},
				},
			},
		},
	}
	h := newRenderHandlers(cfg)
	h.LastResponder.Record("q", 1) // fallback index 1 (the second step)

	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, "route-step--responder") {
		t.Errorf("expected --responder class for recorded responder; got: %s", body)
	}
	// The first step (primary) must NOT have --responder; only the second.
	if strings.Contains(body, `class="route-step route-step--primary route-step--responder"`) {
		t.Errorf("primary step must NOT carry --responder when index is 1; got: %s", body)
	}
}

// TestRouteStepClickableHref verifies §2.6: each `.route-step` is an `<a>`
// whose href includes provider= and mapping= query params.
func TestRouteStepClickableHref(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
			"zen": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  "m1",
				Fallback: []config.Mapping{
					{ProviderName: "zen", ModelString: "fb1"},
				},
			},
		},
	}
	h := newRenderHandlers(cfg)
	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	for _, want := range []string{
		`href="/logs?provider=nim&amp;mapping=q"`,
		`href="/logs?provider=zen&amp;mapping=q"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s; got: %s", want, body)
		}
	}
}

// TestDepthPillRendered verifies §2.3: the card header shows a depth pill
// when fallbacks > 0.
func TestDepthPillRendered(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings: map[string]config.Mapping{
			"q": {
				ProviderName: "nim",
				ModelString:  "m1",
				Fallback: []config.Mapping{
					{ProviderName: "nim", ModelString: "fb1"},
					{ProviderName: "nim", ModelString: "fb2"},
				},
			},
			"r": {ProviderName: "nim", ModelString: "m2"},
		},
	}
	h := newRenderHandlers(cfg)
	req := httptest.NewRequest(http.MethodGet, "/mappings", nil)
	rec := httptest.NewRecorder()
	renderMappingsTable(rec, req, h)
	body := rec.Body.String()

	if !strings.Contains(body, `class="route-card__depth"`) {
		t.Errorf("expected depth pill; got: %s", body)
	}
	if !strings.Contains(body, "2 fallbacks") {
		t.Errorf("expected '2 fallbacks' plural; got: %s", body)
	}
}
