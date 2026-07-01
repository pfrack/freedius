//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// runtimeDir returns the directory for daemon state files (PID,
// socket). On Linux, $XDG_RUNTIME_DIR is set by systemd-logind
// and is per-user + tmpfs (auto-cleaned on reboot). On macOS,
// $XDG_RUNTIME_DIR is not standard, so fall back to os.TempDir()
// which respects $TMPDIR (per-user on macOS).
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

// socketPath returns the path to the IPC Unix socket.
func socketPath() string {
	return filepath.Join(runtimeDir(), "freedius.sock")
}
