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
  other: {behavior: openai}
mappings:
  opus: {provider_name: nim, model_string: test}
  default: {provider_name: other, model_string: catch-all}
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

func TestDeleteMapping_DefaultProtected(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	// The default catch-all must refuse deletion and remain present.
	req := httptest.NewRequest(http.MethodDelete, "/v1/mappings/default", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "protected_mapping") {
		t.Errorf("body missing protected_mapping error code: %s", rec.Body.String())
	}
	cfg.RLock()
	_, stillPresent := cfg.Mappings["default"]
	cfg.RUnlock()
	if !stillPresent {
		t.Error("default mapping should still be present after refused delete")
	}
}

func TestCreateMapping_RebuildsMatchers(t *testing.T) {
	mux, cfg, _ := newWriteMux(t)

	body := "name=mynewmap&provider_name=nim&model_string=claude-3-sonnet"
	req := httptest.NewRequest(http.MethodPost, "/v1/mappings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	// The runtime matcher list must include the newly added key so a model
	// can route to it without a restart.
	cfg.RLock()
	matchers := cfg.Matchers()
	cfg.RUnlock()
	found := false
	for _, mm := range matchers {
		if mm.Name == "mynewmap" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("matchers not rebuilt after create; got %d matchers", len(matchers))
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

// --- Provider writeback: generated-option preservation ---

const zenConfigYAML = `providers:
  zen:
    behavior: mix
    default_base_url: https://opencode.ai/zen/v1/messages
    default_api_key_env: OPENCODE_API_KEY
  nim: {behavior: openai}
mappings:
  opus: {provider_name: zen, model_string: glm-5.1}
  default: {provider_name: nim, model_string: catch-all}
`

// newWriteMuxWith writes the given YAML and returns the mux, config and path.
func newWriteMuxWith(t *testing.T, yaml string) (http.Handler, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "freedius.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
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

// putProvider issues a form-encoded PUT to the provider update endpoint.
func putProvider(t *testing.T, mux http.Handler, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/providers/"+name, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestUpdateProvider_ZenKeepsNoStreamUsage is the headline regression test for
// the data-loss bug. The form carries no openai: inputs, yet the persisted YAML
// must still declare no_stream_usage — otherwise zen upstreams that reject
// stream_options start returning 400s until restart.
func TestUpdateProvider_ZenKeepsNoStreamUsage(t *testing.T) {
	mux, _, cfgPath := newWriteMuxWith(t, zenConfigYAML)

	// Exactly what the dashboard Edit dialog submits: five fields, no openai:.
	body := "name=zen&behavior=mix" +
		"&default_base_url=https://opencode.ai/zen/v1/messages" +
		"&default_api_key_env=OPENCODE_API_KEY&protocol=anthropic"
	rec := putProvider(t, mux, "zen", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	onDisk, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "no_stream_usage: true") {
		t.Errorf("persisted YAML lost no_stream_usage:\n%s", onDisk)
	}

	// And the in-memory value the dispatcher actually reads.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cfg.RLock()
	p := cfg.Providers["zen"]
	cfg.RUnlock()
	if p.OpenAI == nil || !p.OpenAI.NoStreamUsage {
		t.Errorf("reloaded zen lost its openai block: %+v", p.OpenAI)
	}
	if !p.RequireBaseURL {
		t.Error("reloaded zen lost RequireBaseURL")
	}
}

// TestUpdateProvider_EmptyBaseURLRestoredFromDefaults covers the zeroing half:
// submitting a blank base URL must not erase the generated default.
func TestUpdateProvider_EmptyBaseURLRestoredFromDefaults(t *testing.T) {
	mux, _, cfgPath := newWriteMuxWith(t, zenConfigYAML)

	rec := putProvider(t, mux, "nim", "name=nim&behavior=openai&default_base_url=")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	onDisk, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "integrate.api.nvidia.com") {
		t.Errorf("blank base_url erased nim's generated default:\n%s", onDisk)
	}
}

// TestUpdateProvider_InvalidConfigReturns400 asserts the validation gate: a

// TestUpdateProvider_DoesNotInjectOtherProviders pins the scoped-reapply
// contract end to end. applyDefaults injects every ready-to-use provider into
// the map, so naively calling it on the write path would persist providers the
// user never configured. The scoped ReapplyProviderDefaults must not add any.
//
// Note the baseline: load-time applyDefaults already injects ~18 known
// providers into any config, so the invariant is a *delta*, not an absolute
// count. A write must not increase the provider set.
func TestUpdateProvider_DoesNotInjectOtherProviders(t *testing.T) {
	mux, cfg, _ := newWriteMuxWith(t, zenConfigYAML)

	cfg.RLock()
	before := len(cfg.Providers)
	beforeNames := make(map[string]bool, before)
	for n := range cfg.Providers {
		beforeNames[n] = true
	}
	cfg.RUnlock()

	rec := putProvider(t, mux, "zen",
		"name=zen&behavior=mix&default_base_url=https://opencode.ai/zen/v1/messages")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	cfg.RLock()
	after := len(cfg.Providers)
	var added []string
	for n := range cfg.Providers {
		if !beforeNames[n] {
			added = append(added, n)
		}
	}
	cfg.RUnlock()
	if len(added) > 0 {
		t.Errorf("write path injected providers: %v (before=%d after=%d)", added, before, after)
	}
	if after != before {
		t.Errorf("provider count changed: %d -> %d", before, after)
	}
}

// TestUpdateProvider_UserDefinedProviderKeepsSettings covers the case where
// neither D1 nor D2 should touch anything: a provider with no generated entry.
func TestUpdateProvider_UserDefinedProviderKeepsSettings(t *testing.T) {
	yaml := `providers:
  mygateway:
    behavior: openai
    default_base_url: https://my.gateway/v1/chat/completions
    anthropic_version: "2023-06-01"
mappings:
  opus: {provider_name: mygateway, model_string: my-model}
  default: {provider_name: mygateway, model_string: catch-all}
`
	mux, _, cfgPath := newWriteMuxWith(t, yaml)

	// Submit without a base_url — for a user-defined provider there is no
	// generated default to restore it from, so the user's value must be the one
	// that survives.
	rec := putProvider(t, mux, "mygateway",
		"name=mygateway&behavior=openai&default_base_url=https://my.gateway/v1/chat/completions")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	onDisk, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "https://my.gateway/v1/chat/completions") {
		t.Errorf("user-defined base_url lost:\n%s", onDisk)
	}
	// Assert on the mygateway stanza specifically, not the whole file: the
	// pre-existing load-time applyDefaults injects known providers such as nim
	// (which legitimately carries an openai: block) into any config.
	if stanza := providerStanza(string(onDisk), "mygateway"); strings.Contains(stanza, "no_stream_usage") {
		t.Errorf("user-defined provider gained a generated openai block:\n%s", stanza)
	}
	if stanza := providerStanza(string(onDisk), "mygateway"); !strings.Contains(stanza, "2023-06-01") {
		t.Errorf("user-defined anthropic_version lost:\n%s", stanza)
	}
}

// providerStanza returns the indented YAML block for a named top-level provider
// key, so assertions can target one provider without tripping over entries that
// load-time applyDefaults legitimately injects.
func providerStanza(yaml, name string) string {
	lines := strings.Split(yaml, "\n")
	var out []string
	in := false
	for _, ln := range lines {
		key := "  " + name + ":"
		if strings.HasPrefix(ln, key) {
			in = true
			out = append(out, ln)
			continue
		}
		if in {
			// A line at the same indent level starts the next provider.
			if strings.HasPrefix(ln, "  ") && !strings.HasPrefix(ln, "    ") {
				break
			}
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

// write that would leave the config unloadable is rejected with 400 and the
// file on disk is left untouched.
func TestUpdateProvider_InvalidConfigReturns400(t *testing.T) {
	mux, _, cfgPath := newWriteMuxWith(t, zenConfigYAML)

	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// zen requires a base_url (require_base_url: true, no generated default),
	// so clearing it makes the config invalid.
	rec := putProvider(t, mux, "zen", "name=zen&behavior=mix&default_base_url=")
	if rec.Code != 400 {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Errorf("expected validation_failed error code, got: %s", rec.Body.String())
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("config file was modified by a rejected write:\n%s", after)
	}
}

// TestUpdateProvider_HTMXStillRendersTable guards the htmx branch — the
// dashboard expects an HTML fragment, not JSON.
func TestUpdateProvider_HTMXStillRendersTable(t *testing.T) {
	mux, _, _ := newWriteMuxWith(t, zenConfigYAML)

	req := httptest.NewRequest(http.MethodPut, "/v1/providers/zen", strings.NewReader(
		"name=zen&behavior=mix&default_base_url=https://opencode.ai/zen/v1/messages",
	))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	// The contract that matters for htmx is the body shape: an HTML fragment
	// that can be swapped into #providers, not a JSON status object.
	// (Content-Type is deliberately not asserted — renderProvidersTable never
	// sets it, so Go sniffs text/plain; that is pre-existing and htmx does not
	// gate on it.)
	if !strings.Contains(rec.Body.String(), "<table") {
		t.Errorf("expected an HTML table fragment, got: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"status"`) {
		t.Errorf("htmx request should not return JSON, got: %s", rec.Body.String())
	}
}
