//go:build windows

package main

import "fmt"

func startDaemon(args []string) error {
	return fmt.Errorf("daemon mode not supported on Windows, use --fg with a service manager")
}

func stopDaemon() error {
	return fmt.Errorf("daemon mode not supported on Windows")
}

func daemonStatus() (running bool, pid int, err error) {
	return false, 0, fmt.Errorf("daemon mode not supported on Windows")
}
