package config

import "os"

// writeFile is a tiny helper to keep tests focused on assertions, not file ops.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
