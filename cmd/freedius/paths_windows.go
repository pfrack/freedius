//go:build windows

package main

import "os"

func runtimeDir() string {
	return os.TempDir()
}
