package envinject

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	settingsFile   = "settings.json"
	backupPrefix   = settingsFile + ".bak."
	backupTimeForm = "20060102-150405"
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

	dst := uniqueBackupPath(dir, time.Now().Format(backupTimeForm))
	// #nosec G306,G703 -- operator-supplied config dir; backup mirrors settings.json's permissions
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("envinject: write backup %s: %w", dst, err)
	}
	return dst, nil
}

// uniqueBackupPath returns a backup path for ts that does not yet exist. The
// timestamp has second granularity, so two backups within the same second get
// a zero-padded suffix that keeps the lexicographic (= chronological) order.
func uniqueBackupPath(dir, ts string) string {
	base := filepath.Join(dir, backupPrefix+ts)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	for i := 1; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%02d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return base
}

// RestoreSettingsJSON restores the newest settings.json.bak.* back to
// settings.json. Returns an error when no backup exists.
func RestoreSettingsJSON(configDir string) (string, error) {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return "", err
	}

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

	// Timestamps sort lexicographically, so the max name is the newest backup.
	newest := filepath.Join(dir, slices.Max(backups))
	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	data, err := os.ReadFile(newest)
	if err != nil {
		return "", fmt.Errorf("envinject: read backup %s: %w", newest, err)
	}

	dst := filepath.Join(dir, settingsFile)
	// #nosec G306,G703 -- operator-supplied config dir; settings.json must stay tool-readable
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("envinject: restore %s: %w", dst, err)
	}
	return newest, nil
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

// WriteSettingsJSON writes the env block into Claude Code's settings.json
// (at $HOME/.claude/settings.json when configDir is empty). It preserves any
// existing keys and only sets/updates the "env" object.
func WriteSettingsJSON(configDir string, host string, port int, dryRun bool) error {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("envinject: cannot determine home directory: %w", err)
		}
		configDir = filepath.Join(home, ".claude")
	}
	path := filepath.Join(configDir, "settings.json")

	env := envBlock(host, port)

	existing := make(map[string]any)
	// #nosec G304 -- path is operator-supplied via $HOME/.claude
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			slog.Warn(
				"envinject: malformed existing settings.json, replacing",
				"path",
				path,
				"err",
				err,
			)
			existing = make(map[string]any)
		}
	}

	existing["env"] = env

	output, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("envinject: marshal settings.json: %w", err)
	}
	output = append(output, '\n')

	if dryRun {
		fmt.Println(string(output))
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("envinject: create directory %s: %w", dir, err)
	}

	tmpPath := path + ".tmp"
	// #nosec G306 -- settings.json needs to be readable by external tooling (Claude Code)
	if err := os.WriteFile(tmpPath, output, 0o644); err != nil {
		return fmt.Errorf("envinject: write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("envinject: rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}
