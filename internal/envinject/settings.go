package envinject

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func envBlock(host string, port int) map[string]string {
	addr := fmt.Sprintf("http://%s:%d", host, port)
	return map[string]string{
		"ANTHROPIC_BASE_URL":       addr,
		"ANTHROPIC_API_KEY":        "freedius-dummy",
		"ENABLE_TOOL_SEARCH":       "true",
		"DISABLE_TELEMETRY":        "1",
		"DISABLE_ERROR_REPORTING":  "1",
	}
}

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
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			slog.Warn("envinject: malformed existing settings.json, replacing", "path", path, "err", err)
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
	if err := os.WriteFile(tmpPath, output, 0o644); err != nil {
		return fmt.Errorf("envinject: write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("envinject: rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}
