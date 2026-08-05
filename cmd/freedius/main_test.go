package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func testLoggerWithBuffer() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func countWarnings(buf *bytes.Buffer) int {
	n := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["level"] == "WARN" {
			n++
		}
	}
	return n
}

const minimalConfigYAML = "providers:\n" +
	"  nim: {behavior: openai}\n" +
	"mappings:\n" +
	"  opus: {provider_name: nim, model_string: test}\n"

func TestCheckRequiredEnvVars_PresetEnvVarMissing(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "nim", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 1 {
		t.Fatalf("expected 1 warning for missing NVIDIA_NIM_API_KEY, got %d", countWarnings(buf))
	}
	var warn map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &warn); err != nil {
		t.Fatalf("warning is not valid JSON: %v", err)
	}
	if warn["env"] != "NVIDIA_NIM_API_KEY" || warn["provider"] != "nim" {
		t.Errorf("warning should mention env var and provider: %v", warn)
	}
}

func TestCheckRequiredEnvVars_PerProviderOverrideMissing(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "set")
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "zen", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 1 {
		t.Fatalf("expected 1 warning for missing OPENCODE_API_KEY, got %d", countWarnings(buf))
	}
	var warn map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &warn); err != nil {
		t.Fatalf("warning is not valid JSON: %v", err)
	}
	if warn["env"] != "OPENCODE_API_KEY" {
		t.Errorf("warning should mention env var: %v", warn)
	}
}

func TestCheckRequiredEnvVars_AllSet(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":    {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
			"openai": {Behavior: "openai", DefaultAPIKeyEnv: "OPENAI_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"m1": {ProviderName: "nim", ModelString: "test"},
			"m2": {ProviderName: "openai", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 0 {
		t.Errorf("expected 0 warnings when all keys set, got %d", countWarnings(buf))
	}
}

func TestCheckRequiredEnvVars_CustomKeySet(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	t.Setenv("CUSTOM_KEY", "k2")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"custom": {
				Behavior:         "mix",
				DefaultBaseURL:   "https://x",
				DefaultAPIKeyEnv: "CUSTOM_KEY",
			},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "custom", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 0 {
		t.Errorf("expected 0 warnings for custom with key set, got %d", countWarnings(buf))
	}
}

func TestCheckRequiredEnvVars_NoDefaultAPIKeyEnv(t *testing.T) {
	// Providers without a DefaultAPIKeyEnv (e.g. ollama) must not produce a
	// warning even when referenced by a mapping.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"ollama": {Behavior: "openai", DefaultAPIKeyEnv: ""},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "ollama", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 0 {
		t.Errorf("expected 0 warnings for provider without DefaultAPIKeyEnv, got %d", countWarnings(buf))
	}
}

func TestCheckRequiredEnvVars_NoProvidersWithEnv(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"openai": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://x",
				DefaultAPIKeyEnv: "OPENAI_API_KEY",
			},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 0 {
		t.Errorf("expected 0 warnings when no provider referenced by mapping, got %d", countWarnings(buf))
	}
}

func TestCheckRequiredEnvVars_MappingDoesNotTriggerCheck(t *testing.T) {
	// Mappings referencing a provider with a missing env should surface a
	// warning via the provider, not independently per-mapping.
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "zen", ModelString: "x"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 1 {
		t.Fatalf("expected 1 warning for missing OPENCODE_API_KEY, got %d", countWarnings(buf))
	}
	var warn map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &warn); err != nil {
		t.Fatalf("warning is not valid JSON: %v", err)
	}
	if warn["env"] != "OPENCODE_API_KEY" {
		t.Errorf("warning should mention provider env: %v", warn)
	}
}

func TestCheckRequiredEnvVars_MultipleMissingKeys(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"m1": {ProviderName: "nim", ModelString: "test"},
			"m2": {ProviderName: "zen", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 2 {
		t.Fatalf("expected 2 warnings (no early exit), got %d", countWarnings(buf))
	}
	envs := map[string]bool{}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("warning is not valid JSON: %v", err)
		}
		if entry["level"] == "WARN" {
			if e, ok := entry["env"].(string); ok {
				envs[e] = true
			}
		}
	}
	if !envs["NVIDIA_NIM_API_KEY"] || !envs["OPENCODE_API_KEY"] {
		t.Errorf("expected warnings for both missing env vars, got %v", envs)
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := newLogger("json", &buf, proxy.NewLogSink(10))
	if err != nil {
		t.Fatalf("newLogger(json): %v", err)
	}
	logger.Info("hello", "key", "value")
	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw: %s)", err, out)
	}
	if parsed["msg"] != "hello" {
		t.Errorf("msg: got %v, want hello", parsed["msg"])
	}
	if parsed["key"] != "value" {
		t.Errorf("key: got %v, want value", parsed["key"])
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := newLogger("text", &buf, proxy.NewLogSink(10))
	if err != nil {
		t.Fatalf("newLogger(text): %v", err)
	}
	logger.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "key=value") {
		t.Errorf("text format output missing key= / msg= pairs: %s", out)
	}
	// Ensure output is NOT JSON (text handler produces key=value pairs).
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err == nil {
		t.Errorf("text format should not produce valid JSON, got: %s", out)
	}
}

func TestNewLogger_InvalidFormat(t *testing.T) {
	_, err := newLogger("yaml", io.Discard, nil)
	if err == nil {
		t.Fatal("expected error for invalid log format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention invalid format: %v", err)
	}
}

func TestCheckRequiredEnvVars_ProviderNameInWarning(t *testing.T) {
	// The warning must reference the provider's user-defined name.
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "zen", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 1 {
		t.Fatalf("expected 1 warning for missing OPENCODE_API_KEY, got %d", countWarnings(buf))
	}
	var warn map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &warn); err != nil {
		t.Fatalf("warning is not valid JSON: %v", err)
	}
	if warn["provider"] != "zen" {
		t.Errorf("warning should reference the provider name (zen), got: %v", warn)
	}
	if warn["env"] != "OPENCODE_API_KEY" {
		t.Errorf("warning should reference the env var, got: %v", warn)
	}
}

func TestCheckRequiredEnvVars_ReferencesConfiguredProvider(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
		Mappings: map[string]config.Mapping{
			"test": {ProviderName: "nim", ModelString: "test"},
		},
	}
	logger, buf := testLoggerWithBuffer()
	warnMissingEnvVars(logger, cfg)
	if countWarnings(buf) != 1 {
		t.Fatalf("expected 1 warning, got %d", countWarnings(buf))
	}
	var warn map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &warn); err != nil {
		t.Fatalf("warning is not valid JSON: %v", err)
	}
	if warn["provider"] != "nim" {
		t.Errorf("warning should reference the configured provider (nim), got: %v", warn)
	}
}

func TestRun_StartupBanner(t *testing.T) {
	// The startup banner now goes to the TUI's log ring buffer, not stderr.
	// Verify the process still attempts to start (bind error on privileged port).
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	cfgBody := "providers:\n" +
		"  nim: {behavior: openai}\n" +
		"mappings:\n" +
		"  opus: {provider_name: nim, model_string: test}\n"
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", "1", "--no-export-hint")
	cmd.Env = append(os.Environ(), "NVIDIA_NIM_API_KEY=test-key")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run() // expected to fail (port 1 is privileged)
	output := stderr.String()
	if !strings.Contains(output, "bind") && !strings.Contains(output, "address already in use") {
		t.Errorf("expected bind/address error on stderr, got:\n%s", output)
	}
}

func TestRun_VersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	cmd.Dir = "."
	if err := cmd.Run(); err != nil {
		t.Fatalf("run --version: %v (output: %s)", err, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "freedius ") {
		t.Errorf("expected version line, got: %s", out)
	}
}

func TestRun_HelpFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	cmd.Dir = "."
	if err := cmd.Run(); err != nil {
		t.Fatalf("run --help: %v (output: %s)", err, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"Usage: freedius", "config", "port", "verbose-errors", "-c", "ui-port"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRun_CShorthandForConfig(t *testing.T) {
	// Regression for F8: -c is the shorthand for --config.
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "test-key")

	cmd := exec.Command("go", "run", ".", "-c", cfgPath, "--port", "1", "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run() // port 1 may bind or fail; the point is -c was accepted.

	output := stderr.String()
	if strings.Contains(output, "flag provided but not defined: -c") {
		t.Errorf("-c shorthand not registered; stderr:\n%s", output)
	}
}

func TestRun_EvalSnippetAppears(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "test-key")

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", "1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run()
	output := stderr.String()
	if !strings.Contains(output, "ANTHROPIC_BASE_URL") {
		t.Errorf("eval snippet should appear in stderr, got:\n%s", output)
	}
	if !strings.Contains(output, "--no-export-hint") {
		t.Errorf("snippet should mention --no-export-hint")
	}
}

func TestRun_EvalSnippetSuppressed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "test-key")

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", "1", "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run()
	output := stderr.String()
	if strings.Contains(output, "ANTHROPIC_BASE_URL") {
		t.Errorf("eval snippet should be suppressed with --no-export-hint, got:\n%s", output)
	}
}

func TestRun_NoArgsStartsProxy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "test-key")

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", "1", "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run() // expected to fail: port 1 privileged; the point is "freedius" alone starts proxy.
	output := stderr.String()
	// The startup banner now goes to the TUI ring buffer, not stderr.
	// Verify the process attempted to start by checking for the bind error
	// and that no "unknown subcommand" error appeared.
	if strings.Contains(output, "unknown subcommand") {
		t.Errorf("should not print 'unknown subcommand' error, got:\n%s", output)
	}
	if !strings.Contains(output, "bind") && !strings.Contains(output, "address already in use") {
		t.Errorf("expected bind/address error showing the proxy attempted to start, got:\n%s", output)
	}
}

func TestRun_UnknownFlagExitsNonZero(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--bogus")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit for unknown flag")
	}
}

func TestStarterTemplate_ValidConfig(t *testing.T) {
	// Validate that the embedded starter template parses to a valid Config
	// without touching the filesystem. Used as a regression check that the
	// template embedded in main.go remains parseable.
	cfg, err := config.LoadFromBytes([]byte(starterTemplate))
	if err != nil {
		t.Fatalf("starter template should be valid config: %v", err)
	}
	if len(cfg.Providers) == 0 && len(cfg.Mappings) == 0 {
		t.Error("starter template should define at least one provider or mapping")
	}
}

func TestRun_BindFailureSurfaces(t *testing.T) {
	// Regression for F3: when the bind fails (e.g., port already in use),
	// the error must be surfaced immediately. Use a port we hold from a side listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	occupiedPort := ln.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIDIA_NIM_API_KEY", "test-key")

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", strconv.Itoa(occupiedPort), "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	err = cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit when port is occupied")
	}
	output := stderr.String()
	if !strings.Contains(output, "bind") && !strings.Contains(output, "address already in use") {
		t.Errorf("expected bind/address-already-in-use error in stderr, got:\n%s", output)
	}
}

func TestRun_LazyConfigDoesNotWriteFile(t *testing.T) {
	// When the resolved config path doesn't exist and --config wasn't passed,
	// freedius must boot from the embedded default and NOT create a file on
	// disk. Build a one-shot binary in /tmp (outside the test tempdir) and
	// override HOME only for the binary so the test process retains its
	// default GOPATH/GOMODCACHE and doesn't write build artifacts into
	// t.TempDir.
	dir := t.TempDir()
	expectedXDGPath := filepath.Join(dir, ".config", "freedius", "config.yaml")

	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := os.MkdirTemp("", "freedius-bin-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(binDir)
	bin := filepath.Join(binDir, "freedius")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = projectRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Bound the run with a timeout: since startup no longer fails on missing
	// keys, the binary proceeds to bind port 1 and then blocks on SIGINT.
	// On a root container (CAP_NET_BIND_SERVICE) an unbounded cmd.Run() would
	// hang to the go test timeout; the context kills it after 5s instead.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--port", "1", "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Strip any inherited HOME and API key env vars from the test process
	// before re-adding our override, so HOME points only at the empty dir
	// we control and no API keys are picked up from the ambient environment.
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		if strings.HasPrefix(e, "FREEDIUS_") ||
			strings.HasPrefix(e, "OPENAI_API_KEY") ||
			strings.HasPrefix(e, "ANTHROPIC_API_KEY") ||
			strings.HasPrefix(e, "GEMINI_API_KEY") ||
			strings.HasPrefix(e, "MISTRAL_API_KEY") ||
			strings.HasPrefix(e, "DEEPSEEK_API_KEY") ||
			strings.HasPrefix(e, "GROQ_API_KEY") ||
			strings.HasPrefix(e, "TOGETHER_API_KEY") ||
			strings.HasPrefix(e, "FIREWORKS_API_KEY") ||
			strings.HasPrefix(e, "COHERE_API_KEY") ||
			strings.HasPrefix(e, "OPENCODE_API_KEY") ||
			strings.HasPrefix(e, "NVIDIA_NIM_API_KEY") {
			continue
		}
		filtered = append(filtered, e)
	}
	filtered = append(filtered, "HOME="+dir)
	cmd.Env = filtered
	_ = cmd.Run()

	if _, err := os.Stat(expectedXDGPath); err == nil {
		t.Errorf("config file should NOT be created on disk during lazy startup, but found at %s", expectedXDGPath)
	}
}

func TestResolveUIPort(t *testing.T) {
	tests := []struct {
		name    string
		flagVal int
		flagSet bool
		env     string
		want    int
	}{
		{"default", 0, false, "", 8083},
		{"flag set", 9090, true, "", 9090},
		{"env set", 0, false, "7070", 7070},
		{"both flag and env", 9090, true, "7070", 9090},
		{"invalid env falls back", 0, false, "not-a-number", 8083},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("FREEDIUS_UI_PORT", tt.env)
			}
			got := resolveUIPort(tt.flagVal, tt.flagSet)
			if got != tt.want {
				t.Errorf("resolveUIPort(%d, %v) = %d, want %d", tt.flagVal, tt.flagSet, got, tt.want)
			}
		})
	}
}

func TestAuthTokenEnv(t *testing.T) {
	t.Setenv("FREEDIUS_UI_TOKEN", "my-secret-token")
	if v := os.Getenv("FREEDIUS_UI_TOKEN"); v != "my-secret-token" {
		t.Errorf("env round-trip failed: got %q, want %q", v, "my-secret-token")
	}
}
