package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

const testConfigYAML = `providers:
  nim: {behavior: openai}
mappings:
  opus: {provider_name: nim, model_string: test}
`

func newWriteMux(t *testing.T) (http.Handler, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "freedius.yaml")
	if err := os.WriteFile(cfgPath, []byte(testConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:     proxy.NewEventBus(10),
		LogSink: proxy.NewLogSink(10),
		Cfg:     cfg,
		CfgPath: cfgPath,
	}
	return SetupMux(h, logger), cfg, cfgPath
}

func TestCreateProvider(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	body := "name=newprov&behavior=openai&default_base_url=https://api.example.com"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Errorf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if !cfg.HasProvider("newprov") {
		t.Error("provider should exist in config")
	}
}

func TestCreateProvider_ValidationError(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	body := "name=&behavior=openai"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if cfg.HasProvider("") {
		t.Error("empty-named provider should not exist")
	}
}

func TestCreateProvider_SaveFailure(t *testing.T) {
	dir := t.TempDir()
	// Create a file at the parent path so MkdirAll fails.
	parentFile := filepath.Join(dir, "blocked")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(parentFile, "config.yaml")
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{
		Bus:     proxy.NewEventBus(10),
		LogSink: proxy.NewLogSink(10),
		Cfg:     cfg,
		CfgPath: cfgPath,
	}
	mux := SetupMux(h, logger)

	body := "name=newprov&behavior=openai"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	// Rollback: provider should not be in config.
	if cfg.HasProvider("newprov") {
		t.Error("provider should be rolled back after save failure")
	}
}

func TestUpdateProvider(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	body := "name=nim&behavior=anthropic&default_base_url=https://api.anthropic.com"
	req := httptest.NewRequest(http.MethodPut, "/v1/providers/nim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	cfg.RLock()
	p := cfg.Providers["nim"]
	cfg.RUnlock()
	if p.Behavior != "anthropic" {
		t.Errorf("behavior = %q, want anthropic", p.Behavior)
	}
}

func TestUpdateProvider_NotFound(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	body := "name=nonexistent&behavior=openai"
	req := httptest.NewRequest(http.MethodPut, "/v1/providers/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestUpdateProvider_SaveFailure asserts that when SaveData fails after the
// in-memory mutation, the previous provider state is restored before the
// mutex is released (plan §3.7 rollback contract).
func TestUpdateProvider_SaveFailure(t *testing.T) {
	_, cfg, _ := newWriteMux(t)

	// Capture original behavior so we can assert rollback post-failure.
	cfg.RLock()
	original := cfg.Providers["nim"]
	cfg.RUnlock()

	// Force SaveData to fail by swapping in a read-only cfgPath.
	h := &eventstream.Handlers{
		Bus:     proxy.NewEventBus(10),
		LogSink: proxy.NewLogSink(10),
		Cfg:     cfg,
		CfgPath: "/dev/null/cannot-create-subdir/freedius.yaml",
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	body := "name=nim&behavior=anthropic&default_base_url=https://api.anthropic.com"
	req := httptest.NewRequest(http.MethodPut, "/v1/providers/nim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	cfg.RLock()
	rolled := cfg.Providers["nim"]
	cfg.RUnlock()
	if rolled.Behavior != original.Behavior {
		t.Errorf("behavior = %q, want %q (rollback should restore original)", rolled.Behavior, original.Behavior)
	}
	if rolled.DefaultBaseURL != original.DefaultBaseURL {
		t.Errorf("default_base_url = %q, want %q (rollback)", rolled.DefaultBaseURL, original.DefaultBaseURL)
	}
}

func TestDeleteProvider(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	// First remove the mapping that references nim.
	req := httptest.NewRequest(http.MethodDelete, "/v1/mappings/opus", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete mapping: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/providers/nim", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if cfg.HasProvider("nim") {
		t.Error("provider should be deleted")
	}
}

func TestDeleteProvider_InUse(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/providers/nim", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Errorf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteProvider_NotFound(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/providers/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestDeleteProvider_SaveFailure asserts that when SaveData fails after the
// in-memory delete, the provider is restored before the mutex is released
// (plan §3.7 rollback contract). The deleted provider must survive in
// cfg.Providers after the save error.
func TestDeleteProvider_SaveFailure(t *testing.T) {
	_, cfg, _ := newWriteMux(t)

	// Add a provider with no mappings so the in-use check passes.
	cfg.Lock()
	cfg.Providers["lonely"] = config.Provider{Behavior: "openai"}
	cfg.Unlock()

	h := &eventstream.Handlers{
		Bus:     proxy.NewEventBus(10),
		LogSink: proxy.NewLogSink(10),
		Cfg:     cfg,
		CfgPath: "/dev/null/cannot-create-subdir/freedius.yaml",
	}
	mux := SetupMux(h, slog.New(slog.NewTextHandler(sink{}, nil)))

	req := httptest.NewRequest(http.MethodDelete, "/v1/providers/lonely", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if !cfg.HasProvider("lonely") {
		t.Error("provider should be restored after save failure (rollback)")
	}
}

func TestCreateMapping(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	body := "name=haiku&provider_name=nim&model_string=claude-3-haiku"
	req := httptest.NewRequest(http.MethodPost, "/v1/mappings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Errorf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	cfg.RLock()
	_, ok := cfg.Mappings["haiku"]
	cfg.RUnlock()
	if !ok {
		t.Error("mapping should exist in config")
	}
}

func TestCreateMapping_NonExistentProvider(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	body := "name=test&provider_name=nonexistent&model_string=gpt-4"
	req := httptest.NewRequest(http.MethodPost, "/v1/mappings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteMapping(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/mappings/opus", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	cfg.RLock()
	_, ok := cfg.Mappings["opus"]
	cfg.RUnlock()
	if ok {
		t.Error("mapping should be deleted")
	}
}

func TestDeleteMapping_NotFound(t *testing.T) {
	mux, _, _ := newWriteMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/mappings/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCreateProvider_PersistsToDisk(t *testing.T) {
	mux, _, cfgPath := newWriteMux(t)

	body := "name=diskprov&behavior=openai&default_base_url=https://api.example.com"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Reload from disk and verify.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !cfg.HasProvider("diskprov") {
		t.Error("provider should persist to disk")
	}
}
