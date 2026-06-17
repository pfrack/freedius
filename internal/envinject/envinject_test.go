package envinject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnippet_ContainsAllVars(t *testing.T) {
	s := Snippet("127.0.0.1", 8080)
	for _, want := range []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_API_KEY",
		"ENABLE_TOOL_SEARCH",
		"DISABLE_TELEMETRY",
		"DISABLE_ERROR_REPORTING",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("snippet missing %s", want)
		}
	}
	if !strings.Contains(s, "127.0.0.1:8080") {
		t.Errorf("snippet should contain the host:port")
	}
	if !strings.Contains(s, "--no-export-hint") {
		t.Errorf("snippet should mention --no-export-hint")
	}
}

func TestWriteSettingsJSON_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	err := WriteSettingsJSON(dir, "127.0.0.1", 8080, false)
	if err != nil {
		t.Fatalf("WriteSettingsJSON: %v", err)
	}
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if !strings.Contains(string(data), "ANTHROPIC_BASE_URL") {
		t.Errorf("settings.json should contain ANTHROPIC_BASE_URL")
	}
	if !strings.Contains(string(data), "freedius-dummy") {
		t.Errorf("settings.json should contain API key")
	}
}

func TestWriteSettingsJSON_MergePreservesKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	existing := `{"project":"my-app","other":{"nested":true}}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteSettingsJSON(dir, "0.0.0.0", 9090, false)
	if err != nil {
		t.Fatalf("WriteSettingsJSON: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"project": "my-app"`) {
		t.Errorf("existing key 'project' should be preserved")
	}
	if !strings.Contains(string(data), `"other"`) {
		t.Errorf("existing key 'other' should be preserved")
	}
	if !strings.Contains(string(data), "0.0.0.0:9090") {
		t.Errorf("settings.json should contain the provided host:port")
	}
}

func TestWriteSettingsJSON_DryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	err := WriteSettingsJSON(dir, "127.0.0.1", 8080, true)
	if err != nil {
		t.Fatalf("WriteSettingsJSON(dryRun): %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("dry-run should not write the file")
	}
}

func TestWriteSettingsJSON_MalformedEnvReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	existing := `{"env": "not_a_map", "other": true}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteSettingsJSON(dir, "127.0.0.1", 8080, false)
	if err != nil {
		t.Fatalf("WriteSettingsJSON: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"other": true`) {
		t.Errorf("existing key 'other' should survive malformed env replacement")
	}
	if !strings.Contains(string(data), `"env"`) {
		t.Errorf("env key should still exist")
	}
	if strings.Contains(string(data), `"not_a_map"`) {
		t.Errorf("malformed env value should be replaced, not kept")
	}
}

func TestWriteShellRC_AppendsBlock(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	// Simulate existing rc file.
	os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0o644)

	written, err := WriteShellRC(dir, "zsh", "127.0.0.1", 8080, false, false)
	if err != nil {
		t.Fatalf("WriteShellRC: %v", err)
	}
	if written != rcPath {
		t.Errorf("expected rc path %s, got %s", rcPath, written)
	}
	data, _ := os.ReadFile(rcPath)
	content := string(data)
	if !strings.Contains(content, startMarker) {
		t.Errorf("output should contain start marker")
	}
	if !strings.Contains(content, endMarker) {
		t.Errorf("output should contain end marker")
	}
	if !strings.Contains(content, "ANTHROPIC_BASE_URL") {
		t.Errorf("output should contain env vars")
	}
	if !strings.HasPrefix(content, "export FOO=bar") {
		t.Errorf("existing content should precede marker block")
	}
}

func TestWriteShellRC_IdempotentReturn(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteShellRC(dir, "zsh", "127.0.0.1", 8080, false, false)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	_, err = WriteShellRC(dir, "zsh", "127.0.0.1", 8080, false, false)
	if err == nil {
		t.Fatal("expected error for idempotent re-run")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("error should mention 'already installed', got: %v", err)
	}
}

func TestWriteShellRC_ForceReplaces(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("original\n"), 0o644)

	// First install.
	_, err := WriteShellRC(dir, "zsh", "127.0.0.1", 8080, false, false)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Force replace.
	_, err = WriteShellRC(dir, "zsh", "0.0.0.0", 9090, true, false)
	if err != nil {
		t.Fatalf("force replace: %v", err)
	}
	data, _ := os.ReadFile(rcPath)
	content := string(data)
	if strings.Count(content, startMarker) != 1 {
		t.Errorf(
			"expected exactly one start marker after force replace, got %d",
			strings.Count(content, startMarker),
		)
	}
	if !strings.Contains(content, "0.0.0.0:9090") {
		t.Errorf("force replace should write new values (0.0.0.0:9090)")
	}
	// Should start with original content.
	if !strings.HasPrefix(content, "original") {
		t.Errorf("original content should be preserved before marker")
	}
}

func TestWriteShellRC_DryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")

	_, err := WriteShellRC(dir, "zsh", "127.0.0.1", 8080, false, true)
	if err != nil {
		t.Fatalf("WriteShellRC(dryRun): %v", err)
	}
	if _, err := os.Stat(rcPath); err == nil {
		t.Errorf("dry-run should not write the rc file")
	}
}

func TestWriteShellRC_UnknownShellError(t *testing.T) {
	_, err := WriteShellRC(t.TempDir(), "tcsh", "127.0.0.1", 8080, false, false)
	if err == nil {
		t.Fatal("expected error for unknown shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("error should mention unsupported shell, got: %v", err)
	}
}

func TestWriteShellRC_BashPath(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteShellRC(dir, "/usr/bin/bash", "127.0.0.1", 8080, false, false)
	if err != nil {
		t.Fatalf("WriteShellRC: %v", err)
	}
	expected := filepath.Join(dir, ".bashrc")
	if written != expected {
		t.Errorf("expected %s, got %s", expected, written)
	}
}

func TestWriteShellRC_FishSyntax(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteShellRC(dir, "fish", "127.0.0.1", 8080, false, false)
	if err != nil {
		t.Fatalf("WriteShellRC: %v", err)
	}
	expected := filepath.Join(dir, ".config", "fish", "config.fish")
	if written != expected {
		t.Errorf("expected %s, got %s", expected, written)
	}
	data, _ := os.ReadFile(written)
	content := string(data)
	if !strings.Contains(content, "set -gx") {
		t.Errorf("fish block should use set -gx syntax")
	}
}
