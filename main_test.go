package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

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
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing NVIDIA_NIM_API_KEY")
	}
	if !strings.Contains(err.Error(), "NVIDIA_NIM_API_KEY") ||
		!strings.Contains(err.Error(), "nim") {
		t.Errorf("error should mention env var and provider: %v", err)
	}
}

func TestCheckRequiredEnvVars_PerProviderOverrideMissing(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "set")
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENCODE_API_KEY")
	}
	if !strings.Contains(err.Error(), "OPENCODE_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
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
	}
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRequiredEnvVars_CustomNoDefault(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"custom": {
				Behavior:         "mix",
				DefaultBaseURL:   "https://x",
				DefaultAPIKeyEnv: "CUSTOM_KEY",
			},
		},
	}
	t.Setenv("CUSTOM_KEY", "k2")
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error for custom (no preset default): %v", err)
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
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error when nim not referenced: %v", err)
	}
}

func TestCheckRequiredEnvVars_MappingDoesNotTriggerCheck(t *testing.T) {
	// Env var checks are provider-level now; mappings referencing a provider
	// with a missing env should still surface the error via the provider, not
	// independently per-mapping.
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
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENCODE_API_KEY")
	}
	if !strings.Contains(err.Error(), "OPENCODE_API_KEY") {
		t.Errorf("error should mention provider env: %v", err)
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

func TestCheckRequiredEnvVars_ProviderNameInError(t *testing.T) {
	// Under the new schema, the env-var check is provider-level. The error
	// must reference the provider's user-defined name.
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"zen": {Behavior: "mix", DefaultAPIKeyEnv: "OPENCODE_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENCODE_API_KEY")
	}
	if !strings.Contains(err.Error(), "zen") {
		t.Errorf("error should reference the provider name (zen), got: %v", err)
	}
	if !strings.Contains(err.Error(), "OPENCODE_API_KEY") {
		t.Errorf("error should reference the env var, got: %v", err)
	}
}

func TestCheckRequiredEnvVars_ReferencesConfiguredProvider(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "provider \"nim\"") {
		t.Errorf("error should reference the configured provider (nim), got: %v", err)
	}
}

func TestRun_StartupBanner(t *testing.T) {
	// Manual check 2.10: the "freedius starting" log line must appear before
	// "listening on". Run via `go run` so we capture a fresh binary's stderr.
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run() // expected to fail (port 1 is privileged), but banner should be emitted
	output := stderr.String()
	if !strings.Contains(output, "freedius listening on") {
		t.Errorf("startup banner 'freedius listening on' not found in stderr:\n%s", output)
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
	for _, want := range []string{"Usage: freedius", "config", "port", "verbose-errors", "-c"} {
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
	if !strings.Contains(output, "freedius listening on") {
		t.Errorf("expected startup banner with no subcommand, got:\n%s", output)
	}
	if strings.Contains(output, "unknown subcommand") {
		t.Errorf("should not print 'unknown subcommand' error, got:\n%s", output)
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

func TestRun_BindFailureSurfacesBeforeTUI(t *testing.T) {
	// Regression for F3: when the bind fails (e.g., port already in use),
	// the error must be surfaced immediately rather than hidden until the
	// user quits the TUI. Use a port we hold from a side listener.
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

	cmd := exec.Command(bin, "--port", "1", "--no-export-hint")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Strip any inherited HOME from the test process before re-adding our
	// override, so HOME points only at the empty dir we control.
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		filtered = append(filtered, e)
	}
	filtered = append(filtered, "HOME="+dir)
	cmd.Env = filtered
	cmd.Run()

	if _, err := os.Stat(expectedXDGPath); err == nil {
		t.Errorf("config file should NOT be created on disk during lazy startup, but found at %s", expectedXDGPath)
	}

	output := stderr.String()
	if !strings.Contains(output, "freedius listening on") &&
		!strings.Contains(output, "permission denied") &&
		!strings.Contains(output, "address already in use") {
		t.Errorf("expected startup or bind error in stderr, got:\n%s", output)
	}
}
