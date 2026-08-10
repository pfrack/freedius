package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// TestProviderDetail_Drawer covers GET /v1/providers/{name}/detail:
//   - known provider returns 200 + an HTML fragment with identity/config,
//   - API-key env line reflects Declared·Set / Declared·Missing / Not required,
//   - unknown provider returns 404 JSON and renders no drawer fragment.
func TestProviderDetail_Drawer(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				Protocol:         "openai",
				DefaultBaseURL:   "https://api.nim.test/v1",
				DefaultAPIKeyEnv: "NIM_API_KEY_FOR_TEST",
			},
		},
		Mappings: map[string]config.Mapping{},
	}

	t.Run("known provider returns drawer fragment with identity + config", func(t *testing.T) {
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

		req := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/detail", nil)
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
		for _, want := range []string{
			"drawer__content",
			"nim",
			"badge--status-unknown",
			"Unknown",
			"openai",
			"https://api.nim.test/v1",
			"/providers?provider=nim",
			"Edit on Providers page",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("expected %q in provider drawer fragment; body: %s", want, body)
			}
		}
	})

	t.Run("env declared + set shows Declared · Set", func(t *testing.T) {
		t.Setenv("NIM_API_KEY_FOR_TEST", "secret")
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "Declared · Set") {
			t.Errorf("expected 'Declared · Set'; body: %s", rec.Body.String())
		}
	})

	t.Run("env declared + missing shows Declared · Missing", func(t *testing.T) {
		os.Unsetenv("NIM_API_KEY_FOR_TEST")
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "Declared · Missing") {
			t.Errorf("expected 'Declared · Missing'; body: %s", rec.Body.String())
		}
	})

	t.Run("provider without env shows Not required", func(t *testing.T) {
		cfg2 := &config.Config{
			Providers: map[string]config.Provider{
				"ollama": {Behavior: "openai", Protocol: "openai", DefaultBaseURL: "http://localhost:11434"},
			},
			Mappings: map[string]config.Mapping{},
		}
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg2,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/ollama/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "Not required") {
			t.Errorf("expected 'Not required'; body: %s", rec.Body.String())
		}
	})

	t.Run("unknown provider returns 404 JSON and no fragment", func(t *testing.T) {
		h := &eventstream.Handlers{
			Bus:           proxy.NewEventBus(1),
			LogSink:       proxy.NewLogSink(1),
			Cfg:           cfg,
			LastResponder: proxy.NewLastResponder(),
		}
		mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/does-not-exist/detail", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "not_found") {
			t.Errorf("expected not_found code; body: %s", body)
		}
		if strings.Contains(body, "provider-drawer") {
			t.Errorf("unknown provider must not render drawer fragment; body: %s", body)
		}
	})
}
