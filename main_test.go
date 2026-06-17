package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestCheckRequiredEnvVars_PresetEnvVarMissing(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "nim", Model: "x", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing NVIDIA_NIM_API_KEY")
	}
	if !strings.Contains(err.Error(), "NVIDIA_NIM_API_KEY") || !strings.Contains(err.Error(), "nim") {
		t.Errorf("error should mention env var and provider: %v", err)
	}
}

func TestCheckRequiredEnvVars_PerModelOverrideMissing(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "set")
	t.Setenv("OPENAI_API_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestCheckRequiredEnvVars_AllSet(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "nim", Model: "x", APIKeyEnv: "NVIDIA_NIM_API_KEY"},
			"b": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRequiredEnvVars_CustomNoDefault(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "custom", Model: "x", BaseURL: "https://x", APIKeyEnv: "CUSTOM_KEY"},
		},
	}
	t.Setenv("CUSTOM_KEY", "k2")
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error for custom (no preset default): %v", err)
	}
}

func TestCheckRequiredEnvVars_ProviderNotReferenced(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "openai", Model: "gpt-4", BaseURL: "https://x", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error when nim not referenced: %v", err)
	}
}

func TestCheckRequiredEnvVars_MappingMissingEnv(t *testing.T) {
	t.Setenv("NVIDIA_NIM_API_KEY", "k1")
	t.Setenv("MY_MAPPING_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"opus": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "MY_MAPPING_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing mapping env")
	}
	if !strings.Contains(err.Error(), "MY_MAPPING_KEY") {
		t.Errorf("error should mention mapping env: %v", err)
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := newLogger("json", &buf)
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
	logger, err := newLogger("text", &buf)
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
	_, err := newLogger("yaml", io.Discard)
	if err == nil {
		t.Fatal("expected error for invalid log format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention invalid format: %v", err)
	}
}

func TestCheckRequiredEnvVars_UsesOriginalProvider(t *testing.T) {
	// `provider=zen` post-rewrites to `mix`, but the user wrote `zen` — the
	// error must reflect the user's actual provider name.
	t.Setenv("OPENCODE_API_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"haiku": {
				Provider:         "mix",
				OriginalProvider: "zen",
				Model:            "x",
				APIKeyEnv:        "OPENCODE_API_KEY",
			},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENCODE_API_KEY")
	}
	if !strings.Contains(err.Error(), "provider=zen") {
		t.Errorf("error should reference the original provider name (zen), got: %v", err)
	}
	if strings.Contains(err.Error(), "provider=mix") {
		t.Errorf("error must NOT reference the rewritten provider name (mix), got: %v", err)
	}
}

func TestCheckRequiredEnvVars_FallsBackToProvider(t *testing.T) {
	// Backwards compat: Model literals without OriginalProvider should still
	// report the (post-rewrite) Provider name.
	t.Setenv("MY_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"x": {Provider: "custom", Model: "x", APIKeyEnv: "MY_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	// OriginalProvider is empty here so the function should fall back to Provider.
	if !strings.Contains(err.Error(), "provider=custom") {
		t.Errorf("error should reference Provider when OriginalProvider empty, got: %v", err)
	}
}

func TestRun_StartupBanner(t *testing.T) {
	// Manual check 2.10: the "freedius starting" log line must appear before
	// "listening on". Run via `go run` so we capture a fresh binary's stderr.
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte("mappings: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".", "--config", cfgPath, "--port", "0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Dir = "."
	cmd.Run() // expected to fail (port 0 on listener), but banner should be emitted
	output := stderr.String()
	if !strings.Contains(output, "freedius starting") {
		t.Errorf("startup banner 'freedius starting' not found in stderr:\n%s", output)
	}
	// The "listening on" line may or may not appear (port 0 fails to bind).
}

// --- Phase 3: init subcommand + subcommand dispatch ---

func TestDispatch_HelpSubcommand(t *testing.T) {
	code := dispatch([]string{"freedius", "help"})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestDispatch_VersionSubcommand(t *testing.T) {
	code := dispatch([]string{"freedius", "version"})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w

	code := dispatch([]string{"freedius", "nonexistent_sub"})
	w.Close()
	os.Stderr = old
	_, _ = io.Copy(&stderr, r)

	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "nonexistent_sub") {
		t.Errorf("stderr should mention unknown subcommand")
	}
}

func TestDispatch_NoSubcommandRoutesToServe(t *testing.T) {
	// Regression: freedius with no subcommand or flags starting with "-"
	// routes to serve, not help/version or unknown.
	code := dispatch([]string{"freedius", "--help"})
	if code != 0 {
		t.Errorf("expected 0 for --help via serve, got %d", code)
	}
	code = dispatch([]string{"freedius", "-h"})
	if code != 0 {
		t.Errorf("expected 0 for -h via serve, got %d", code)
	}
}

func TestRunInit_WritesDefaultOutput(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	code := runInit([]string{})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if _, err := os.Stat("freedius.yaml"); err != nil {
		t.Fatalf("freedius.yaml not written: %v", err)
	}
	data, _ := os.ReadFile("freedius.yaml")
	if !strings.Contains(string(data), "mappings:") {
		t.Errorf("template should contain mappings: section")
	}
}

func TestRunInit_DryRunPrintsToStdout(t *testing.T) {
	var stdout bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	code := runInit([]string{"--dry-run"})
	w.Close()
	os.Stdout = old
	_, _ = io.Copy(&stdout, r)

	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "mappings:") {
		t.Errorf("dry-run stdout should contain template content")
	}
}

func TestRunInit_CustomOutputPath(t *testing.T) {
	dir := t.TempDir()
	out := dir + "/custom_config.yaml"

	code := runInit([]string{"--output", out})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

func TestRunInit_CustomOutputCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	out := dir + "/subdir/nested/config.yaml"

	code := runInit([]string{"--output", out})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

func TestRunInit_ExistingFileFailsWithoutForce(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)
	os.WriteFile("freedius.yaml", []byte("existing"), 0o644)

	var stderr bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w

	code := runInit([]string{})
	w.Close()
	os.Stderr = old
	_, _ = io.Copy(&stderr, r)

	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("stderr should mention file exists, got: %s", stderr.String())
	}
}

func TestRunInit_ForceOverwritesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)
	os.WriteFile("freedius.yaml", []byte("old content"), 0o644)

	code := runInit([]string{"--force"})
	if code != 0 {
		t.Fatalf("expected 0 with --force, got %d", code)
	}
	data, _ := os.ReadFile("freedius.yaml")
	if strings.Contains(string(data), "old content") {
		t.Errorf("file should be overwritten, not contain old content")
	}
	if !strings.Contains(string(data), "mappings:") {
		t.Errorf("overwritten file should contain template")
	}
	if _, err := os.Stat("freedius.yaml.bak"); err != nil {
		t.Errorf("backup freedius.yaml.bak should exist: %v", err)
	}
}

func TestStarterTemplate_ValidYAML(t *testing.T) {
	// Dry-run instead of writing to disk: parse via config.Load.
	// We validate the template produces a parseable Config.
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte(starterTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("starter template should be valid config: %v", err)
	}
	if len(cfg.Mappings) == 0 {
		t.Errorf("starter template should define at least one mapping")
	}
}

// --- Phase 4: env auto-injection wiring ---

func TestRunInit_WritesSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	code := runInit([]string{})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	// Check ~/.claude/settings.json was written.
	settingsPath := dir + "/.claude/settings.json"
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
}

func TestRunInit_SkipsSettingsJSONWithNoEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	code := runInit([]string{"--no-env"})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	settingsPath := dir + "/.claude/settings.json"
	if _, err := os.Stat(settingsPath); err == nil {
		t.Errorf("settings.json should not be written with --no-env")
	}
}

func TestRunInit_ShellInstallWritesRC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	code := runInit([]string{"--shell-install"})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	rcPath := dir + "/.zshrc"
	if _, err := os.Stat(rcPath); err != nil {
		t.Fatalf(".zshrc not written: %v", err)
	}
}

func TestRunInit_ShellInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	code := runInit([]string{"--shell-install", "--output", "cfg1.yaml"})
	if code != 0 {
		t.Fatalf("first install: expected 0, got %d", code)
	}

	var stderr bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w

	code = runInit([]string{"--shell-install", "--output", "cfg2.yaml"})
	w.Close()
	os.Stderr = old
	_, _ = io.Copy(&stderr, r)

	if code != 1 {
		t.Errorf("second install: expected 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "already installed") {
		t.Errorf("stderr should mention 'already installed', got: %s", stderr.String())
	}
}

func TestRunInit_ShellInstallRefusesUnknownShell(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/usr/bin/tcsh")
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)

	var stderr bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w

	code := runInit([]string{"--shell-install"})
	w.Close()
	os.Stderr = old
	_, _ = io.Copy(&stderr, r)

	if code != 1 {
		t.Errorf("expected 1 for unknown shell, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unsupported shell") {
		t.Errorf("stderr should mention unsupported shell, got: %s", stderr.String())
	}
}

func TestRunServe_EvalSnippetAppears(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte("mappings:\n  opus: {provider: nim, model: test}\n"), 0o644); err != nil {
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

func TestRunServe_EvalSnippetSuppressed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"
	if err := os.WriteFile(cfgPath, []byte("mappings:\n  opus: {provider: nim, model: test}\n"), 0o644); err != nil {
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
