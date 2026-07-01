//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func runtimeDir() string {
	return os.TempDir()
}

func socketPath() string {
	return filepath.Join(runtimeDir(), "freedius.sock")
}
