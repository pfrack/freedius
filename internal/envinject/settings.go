package envinject

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	settingsFile     = "settings.json"
	backupPrefix     = settingsFile + ".bak."
	prerestorePrefix = settingsFile + ".prerestore."
	backupTimeForm   = "20060102-150405"
)

// resolveConfigDir returns configDir, defaulting to $HOME/.claude when empty.
func resolveConfigDir(configDir string) (string, error) {
	if configDir != "" {
		return configDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("envinject: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// BackupSettingsJSON copies the existing settings.json (if any) to a
// timestamped settings.json.bak.<ts> in the same dir. Returns ("", nil) when
// there is no source file to back up.
func BackupSettingsJSON(configDir string) (string, error) {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, settingsFile)

	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("envinject: read %s: %w", src, err)
	}

	// Preserve the source file's permission bits so a 0600 settings.json (which
	// may hold a real API key) is not widened to world-readable in its backup.
	perm := os.FileMode(0o600)
	if fi, err := os.Stat(src); err == nil {
		perm = fi.Mode().Perm()
	}

	dst, err := uniquePath(dir, backupPrefix, time.Now().Format(backupTimeForm))
	if err != nil {
		return "", err
	}
	// #nosec G306,G703 -- operator-supplied config dir; backup preserves source perms (falls back to 0600)
	if err := os.WriteFile(dst, data, perm); err != nil {
		return "", fmt.Errorf("envinject: write backup %s: %w", dst, err)
	}
	return dst, nil
}

// uniquePath returns a path for the given prefix and timestamp that does not yet
// exist. The timestamp has second granularity, so two files within the same
// second get a zero-padded suffix that keeps the lexicographic (= chronological)
// order within a prefix. The candidate is claimed with O_CREATE|O_EXCL so two
// concurrent callers cannot land on the same path (no TOCTOU), and after 100
// collisions it returns an error rather than clobbering an existing file.
func uniquePath(dir, prefix, ts string) (string, error) {
	base := filepath.Join(dir, prefix+ts)
	if p, ok := claimPath(base); ok {
		return p, nil
	}
	for i := 1; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%02d", base, i)
		if p, ok := claimPath(candidate); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("envinject: could not allocate a unique path for %s (too many collisions)", base)
}

// claimPath attempts to create path exclusively. It returns the path on success,
// or false if the path already exists (or another error occurred).
func claimPath(path string) (string, bool) {
	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", false
	}
	_ = f.Close()
	return path, true
}

// RestoreSettingsJSON restores the newest settings.json.bak.* back to
// settings.json. Returns an error when no backup exists.
func RestoreSettingsJSON(configDir string) (string, error) {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return "", err
	}

	// Back up the current settings.json before overwriting it, so a mistaken
	// --restore is itself reversible (mirrors the care taken in runConfigure).
	// We only do this once we know a real backup exists to restore, so that
	// `--restore` with nothing to restore still fails fast (see below).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("envinject: read directory %s: %w", dir, err)
	}
	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name := e.Name(); len(name) > len(backupPrefix) &&
			name[:len(backupPrefix)] == backupPrefix {
			backups = append(backups, name)
		}
	}
	if len(backups) == 0 {
		return "", fmt.Errorf("envinject: no %s* backup found in %s", backupPrefix, dir)
	}

	// Safety net: snapshot the current settings.json under a separate prefix so a
	// mistaken --restore is recoverable. A distinct prefix keeps it out of the
	// .bak.* selection, so it never shadows the user's real backup (which must
	// remain the newest restore target).
	if err := snapshotPreRestore(dir); err != nil {
		return "", fmt.Errorf("envinject: snapshot before restore: %w", err)
	}

	// Timestamps sort lexicographically, so the max name is the newest backup.
	newest := filepath.Join(dir, slices.Max(backups))
	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	data, err := os.ReadFile(newest)
	if err != nil {
		return "", fmt.Errorf("envinject: read backup %s: %w", newest, err)
	}

	// Preserve the backup's permission bits so the restored settings.json keeps
	// the original file's mode (e.g. 0600) rather than being widened.
	perm := os.FileMode(0o600)
	if fi, err := os.Stat(newest); err == nil {
		perm = fi.Mode().Perm()
	}

	dst := filepath.Join(dir, settingsFile)
	// #nosec G306,G703 -- operator-supplied config dir; restore preserves backup perms (falls back to 0600)
	if err := os.WriteFile(dst, data, perm); err != nil {
		return "", fmt.Errorf("envinject: restore %s: %w", dst, err)
	}
	return newest, nil
}

// snapshotPreRestore copies the live settings.json (if present) to a
// timestamped settings.json.prerestore.<ts>, preserving its permission bits, so
// that a mistaken --restore can be undone. It is a no-op when there is no live
// settings.json to snapshot. The distinct prefix keeps the snapshot out of the
// .bak.* selection used for the actual restore.
func snapshotPreRestore(dir string) error {
	live := filepath.Join(dir, settingsFile)

	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	data, err := os.ReadFile(live)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("envinject: read %s: %w", live, err)
	}

	perm := os.FileMode(0o600)
	if fi, err := os.Stat(live); err == nil {
		perm = fi.Mode().Perm()
	}

	dst, err := uniquePath(dir, prerestorePrefix, time.Now().Format(backupTimeForm))
	if err != nil {
		return err
	}
	// #nosec G306,G703 -- operator-supplied config dir; snapshot preserves source perms (falls back to 0600)
	if err := os.WriteFile(dst, data, perm); err != nil {
		return fmt.Errorf("envinject: write prerestore snapshot %s: %w", dst, err)
	}
	return nil
}

// IsFreediusSettings reports whether the existing settings.json at configDir is
// exactly the freedius-authored env block (no extra keys). The CLI uses this to
// avoid re-backing-up an already-configured file: a second `configure` run would
// otherwise back up the freedius block itself, making the newest backup a copy of
// freedius's content rather than the user's original — which would make
// `--restore` return the wrong file.
func IsFreediusSettings(configDir string, host string, port int) (bool, error) {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, settingsFile)

	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("envinject: read %s: %w", path, err)
	}

	var current map[string]any
	if err := json.Unmarshal(data, &current); err != nil {
		// A malformed file is not the freedius block.
		return false, nil
	}

	want := map[string]any{"env": envBlock(host, port)}
	curBytes, err := json.Marshal(current)
	if err != nil {
		return false, nil
	}
	wantBytes, err := json.Marshal(want)
	if err != nil {
		return false, nil
	}
	return bytes.Equal(curBytes, wantBytes), nil
}

func envBlock(host string, port int) map[string]string {
	addr := fmt.Sprintf("http://%s:%d", host, port)
	return map[string]string{
		"ANTHROPIC_BASE_URL":      addr,
		"ANTHROPIC_API_KEY":       "freedius-dummy",
		"ENABLE_TOOL_SEARCH":      "true",
		"DISABLE_TELEMETRY":       "1",
		"DISABLE_ERROR_REPORTING": "1",
	}
}

// WriteSettingsJSON writes Claude Code's settings.json (at
// $HOME/.claude/settings.json when configDir is empty) as a fresh document
// containing only freedius's env block. Any pre-existing keys are discarded —
// back the file up with BackupSettingsJSON first (see RestoreSettingsJSON).
func WriteSettingsJSON(configDir string, host string, port int, dryRun bool) error {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, settingsFile)

	settings := map[string]any{"env": envBlock(host, port)}

	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("envinject: marshal settings.json: %w", err)
	}
	output = append(output, '\n')

	if dryRun {
		fmt.Println(string(output))
		return nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("envinject: create directory %s: %w", dir, err)
	}

	// Stage through a unique temp file so concurrent runs cannot clobber each
	// other, and clean it up on any failure (a failed rename would otherwise
	// leave an orphan settings.json.*.tmp behind).
	tmp, err := os.CreateTemp(dir, "settings.json.*.tmp")
	if err != nil {
		return fmt.Errorf("envinject: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	// #nosec G306,G703 -- operator-supplied config dir; settings.json must stay tool-readable
	if _, err := tmp.Write(output); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("envinject: write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("envinject: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("envinject: rename %s -> %s: %w", tmpPath, path, err)
	}
	cleanup = false
	return nil
}
