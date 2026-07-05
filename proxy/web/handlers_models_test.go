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

func TestFetchModels_ColdCache(t *testing.T) {
	mux, _, _ := newModelsWriteMux(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No models fetched yet") {
		t.Errorf("expected 'No models fetched yet' in cold cache body, got: %s", body)
	}
}

func TestFetchModels_NamedNonexistent(t *testing.T) {
	mux, _, _ := newModelsWriteMux(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/providers/nonexistent/models", nil)
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

	// Refresh
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d; body: %s", rec.Code, rec.Body.String())
	}

	// GET should return cached data now
	req2 := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/models", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("GET: status = %d, want 200", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "cached-model") {
		t.Errorf("expected cached models in GET, got: %s", rec2.Body.String())
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

func TestFetchModels_CachedAfterFailedRefresh(t *testing.T) {
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

	// First refresh succeeds.
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh: status = %d", rec.Code)
	}

	// Now break the upstream by changing the provider's base URL to a dead port.
	cfg.Lock()
	cfg.Providers["nim"] = config.Provider{
		Behavior:       "openai",
		DefaultBaseURL: "http://127.0.0.1:1/v1/chat/completions",
	}
	cfg.Unlock()

	// Refresh again — this fails, but cache keeps previous data.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/providers/nim/models/refresh", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("second refresh: status = %d, want 200", rec2.Code)
	}

	// GET should still return cached models.
	req3 := httptest.NewRequest(http.MethodGet, "/v1/providers/nim/models", nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Errorf("GET after failed refresh: status = %d, want 200", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), "persistent-model") {
		t.Errorf("expected persistent models from cache, got: %s", rec3.Body.String())
	}
}
