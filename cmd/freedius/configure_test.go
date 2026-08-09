package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func countBackups(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak.") {
			n++
		}
	}
	return n
}

func readSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json unreadable: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	return got
}

func TestRunConfigure_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()

	if code := runConfigure([]string{"--dry-run", "--config-dir", dir}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run should write nothing, found %d entries", len(entries))
	}
}

func TestRunConfigure_FirstRunWritesNoBackup(t *testing.T) {
	dir := t.TempDir()

	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := readSettings(t, dir)
	env, ok := got["env"].(map[string]any)
	if !ok {
		t.Fatalf("env block missing: %v", got)
	}
	if env["ANTHROPIC_BASE_URL"] == "" || env["ANTHROPIC_BASE_URL"] == nil {
		t.Errorf("ANTHROPIC_BASE_URL should be set, got %v", env["ANTHROPIC_BASE_URL"])
	}
	if n := countBackups(t, dir); n != 0 {
		t.Errorf("first run should make no backup, found %d", n)
	}
}

func TestRunConfigure_SecondRunDoesNotRebackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"project":"mine"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("first run exit code = %d, want 0", code)
	}
	if n := countBackups(t, dir); n != 1 {
		t.Fatalf("expected 1 backup after the first run, found %d", n)
	}
	// The second run finds settings.json already equals the freedius block, so
	// it must NOT back up the freedius block itself (otherwise --restore would
	// return the wrong file). It still overwrites with the freedius block.
	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("second run exit code = %d, want 0", code)
	}
	if n := countBackups(t, dir); n != 1 {
		t.Errorf("second run must not create a new backup (already configured), found %d", n)
	}
	got := readSettings(t, dir)
	if _, ok := got["project"]; ok {
		t.Errorf("overwrite should discard the pre-existing 'project' key")
	}
}

// TestRunConfigure_RestoreAfterSecondRunReturnsOriginal is the regression guard
// for the F1 bug: a second `configure` run must not shadow the user's original
// backup, so --restore still returns exactly the original settings.json.
func TestRunConfigure_RestoreAfterSecondRunReturnsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{"project":"mine"}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("first run exit code = %d, want 0", code)
	}
	// A second run while already configured must not create a freedius-block backup.
	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("second run exit code = %d, want 0", code)
	}
	if n := countBackups(t, dir); n != 1 {
		t.Fatalf("expected exactly 1 backup (the original) before restore, found %d", n)
	}
	if code := runConfigure([]string{"--restore", "--config-dir", dir}); code != 0 {
		t.Fatalf("restore exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("restore after a second configure run = %q, want original %q", string(data), original)
	}
}

func TestRunConfigure_RestoreReturnsNewestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{"project":"mine"}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("configure exit code = %d, want 0", code)
	}
	if code := runConfigure([]string{"--restore", "--config-dir", dir}); code != 0 {
		t.Fatalf("restore exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("restored settings.json = %q, want %q", string(data), original)
	}
}

func TestRunConfigure_RestoreWithoutBackupFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runConfigure([]string{"--restore", "--config-dir", dir}); code != 1 {
		t.Errorf("exit code = %d, want 1 when no backup exists", code)
	}
}

func TestRunConfigure_YesSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	// An empty stdin would decline the prompt; --yes must never read it.
	restore := configureStdin
	configureStdin = strings.NewReader("")
	t.Cleanup(func() { configureStdin = restore })

	if code := runConfigure([]string{"--config-dir", dir, "--yes"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Errorf("--yes should write settings.json without a prompt: %v", err)
	}
}

func TestRunConfigure_PromptDeclineLeavesFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	restore := configureStdin
	configureStdin = strings.NewReader("n\n")
	t.Cleanup(func() { configureStdin = restore })

	if code := runConfigure([]string{"--config-dir", dir}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("declining the prompt should write nothing, stat err = %v", err)
	}
}

func TestRunConfigure_PromptAcceptWrites(t *testing.T) {
	dir := t.TempDir()
	restore := configureStdin
	configureStdin = strings.NewReader("y\n")
	t.Cleanup(func() { configureStdin = restore })

	if code := runConfigure([]string{"--config-dir", dir}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Errorf("confirming the prompt should write settings.json: %v", err)
	}
}

func TestRun_ConfigureDispatchedBeforeFlagParse(t *testing.T) {
	dir := t.TempDir()

	// --config-dir is not a server flag: reaching the flat parse would exit 2.
	if code := run([]string{"configure", "--config-dir", dir, "--dry-run"}); code != 0 {
		t.Fatalf("run(configure ...) exit code = %d, want 0", code)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run via run() should write nothing, found %d entries", len(entries))
	}
}

func TestPrintUsage_MentionsConfigure(t *testing.T) {
	var sb strings.Builder
	printUsage(&sb)
	if !strings.Contains(sb.String(), "freedius configure") {
		t.Errorf("usage should mention the configure subcommand:\n%s", sb.String())
	}
}
