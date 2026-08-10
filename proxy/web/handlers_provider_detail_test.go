package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
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

	// noEnvCfg declares no DefaultAPIKeyEnv, so the drawer reports the key as
	// not required rather than missing.
	noEnvCfg := &config.Config{
		Providers: map[string]config.Provider{
			"ollama": {Behavior: "openai", Protocol: "openai", DefaultBaseURL: "http://localhost:11434"},
		},
		Mappings: map[string]config.Mapping{},
	}

	get := func(t *testing.T, c *config.Config, path string) *httptest.ResponseRecorder {
		t.Helper()
		mux := SetupMux(newRenderHandlers(c), slog.New(slog.NewTextHandler(sink{}, nil)))
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("known provider returns drawer fragment with identity + config", func(t *testing.T) {
		rec := get(t, cfg, "/v1/providers/nim/detail")

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

	// The API-key env line has three states driven by DefaultAPIKeyEnv and the
	// process environment. t.Setenv("", …) is used for the missing case so the
	// value is restored automatically instead of leaking into sibling tests.
	envCases := []struct {
		name     string
		cfg      *config.Config
		path     string
		envKey   string
		envValue string
		want     string
	}{
		{
			name:     "declared + set",
			cfg:      cfg,
			path:     "/v1/providers/nim/detail",
			envKey:   "NIM_API_KEY_FOR_TEST",
			envValue: "secret",
			want:     "Declared · Set",
		},
		{
			name:     "declared + missing",
			cfg:      cfg,
			path:     "/v1/providers/nim/detail",
			envKey:   "NIM_API_KEY_FOR_TEST",
			envValue: "",
			want:     "Declared · Missing",
		},
		{
			name: "not declared",
			cfg:  noEnvCfg,
			path: "/v1/providers/ollama/detail",
			want: "Not required",
		},
	}

	for _, tc := range envCases {
		t.Run("api key env "+tc.name, func(t *testing.T) {
			if tc.envKey != "" {
				t.Setenv(tc.envKey, tc.envValue)
			}
			body := get(t, tc.cfg, tc.path).Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("expected %q; body: %s", tc.want, body)
			}
		})
	}

	t.Run("unknown provider returns 404 JSON and no fragment", func(t *testing.T) {
		rec := get(t, cfg, "/v1/providers/does-not-exist/detail")

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
		// drawer__content is what the rendered fragment actually emits;
		// "provider-drawer" is only the {{define}} name and never appears
		// in output, so asserting on it would be tautological.
		if strings.Contains(body, "drawer__content") {
			t.Errorf("unknown provider must not render drawer fragment; body: %s", body)
		}
	})
}
