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

func newModelsWriteMux(t *testing.T) (http.Handler, *config.Config, *proxy.ModelsCache) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.example.com/v1/chat/completions",
				DefaultAPIKeyEnv: "",
			},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "test"},
		},
	}
	mc := proxy.NewModelsCache()
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		CfgPath:     dir + "/freedius.yaml",
		ModelsCache: mc,
	}
	return SetupMux(h, logger), cfg, mc
}

func TestRefreshModels_NamedNonexistent(t *testing.T) {
	mux, _, _ := newModelsWriteMux(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nonexistent/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRefreshModels_WithUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o"}, {"id": "gpt-4o-mini"}]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   srv.URL + "/v1/chat/completions",
				DefaultAPIKeyEnv: "",
			},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "test"},
		},
	}
	mc := proxy.NewModelsCache()
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, logger)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gpt-4o") {
		t.Errorf("expected model gpt-4o in body, got: %s", body)
	}
	if !strings.Contains(body, "gpt-4o-mini") {
		t.Errorf("expected model gpt-4o-mini in body, got: %s", body)
	}
}

func TestRefreshModels_AfterRefreshGetCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": "cached-model"}]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   srv.URL + "/v1/chat/completions",
				DefaultAPIKeyEnv: "",
			},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "test"},
		},
	}
	mc := proxy.NewModelsCache()
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, logger)

	// First refresh populates cache.
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh: status = %d; body: %s", rec.Code, rec.Body.String())
	}

	// Second refresh hits same idempotent upstream — response still contains models.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("second refresh: status = %d, want 200", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "cached-model") {
		t.Errorf("expected cached models in second POST, got: %s", rec2.Body.String())
	}
}

func TestRefreshModels_UpstreamError(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   "http://127.0.0.1:1/v1/chat/completions",
				DefaultAPIKeyEnv: "",
			},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "test"},
		},
	}
	mc := proxy.NewModelsCache()
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, logger)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (graceful error); body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "fetch models") && !strings.Contains(body, "error") &&
		!strings.Contains(body, "Error") {
		t.Errorf("expected error message in body, got: %s", body)
	}
}

func TestRefreshModels_CachedAfterFailedRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": "persistent-model"}]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   srv.URL + "/v1/chat/completions",
				DefaultAPIKeyEnv: "",
			},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "test"},
		},
	}
	mc := proxy.NewModelsCache()
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:         proxy.NewEventBus(10),
		LogSink:     proxy.NewLogSink(10),
		Cfg:         cfg,
		ModelsCache: mc,
	}
	mux := SetupMux(h, logger)

	// First refresh succeeds and populates cache.
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh: status = %d", rec.Code)
	}

	// Break the upstream.
	cfg.Lock()
	cfg.Providers["nim"] = config.Provider{
		Behavior:       "openai",
		DefaultBaseURL: "http://127.0.0.1:1/v1/chat/completions",
	}
	cfg.Unlock()

	// Second refresh fails; response shows error with stale FetchedAt timestamp.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second refresh: status = %d, want 200", rec2.Code)
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, "form-error") {
		t.Errorf("expected error markup in second POST response, got: %s", body2)
	}
	if !strings.Contains(body2, "Fetched") {
		t.Errorf("expected FetchedAt timestamp (cache preserved), got: %s", body2)
	}

	// Restore upstream and re-fetch — cached models should still be available.
	cfg.Lock()
	cfg.Providers["nim"] = config.Provider{
		Behavior:         "openai",
		DefaultBaseURL:   srv.URL + "/v1/chat/completions",
		DefaultAPIKeyEnv: "",
	}
	cfg.Unlock()
	req3 := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("third refresh: status = %d", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), "persistent-model") {
		t.Errorf("expected cached models after upstream restored, got: %s", rec3.Body.String())
	}
}
