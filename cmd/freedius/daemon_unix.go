//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func pidFilePath() string {
	return filepath.Join(runtimeDir(), "freedius.pid")
}

func lockFilePath() string {
	return filepath.Join(runtimeDir(), "freedius.lock")
}

func startDaemon(args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("freedius: cannot resolve executable path: %w", err)
	}

	// Refuse to start under go run (executable in temp directory).
	if strings.HasPrefix(exePath, os.TempDir()) {
		return fmt.Errorf("freedius: --daemon requires a built binary; run 'go build -o freedius ./cmd/freedius' first")
	}

	// Check if already running.
	lockFile, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lockFile)

	if running, pid, _ := checkDaemonRunning(); running {
		return fmt.Errorf("freedius: already running (PID %d)", pid)
	}

	// Build args for child process: original args + --fg.
	childArgs := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "--daemon" || a == "-d" {
			continue
		}
		childArgs = append(childArgs, a)
	}
	childArgs = append(childArgs, "--fg")

	cmd := exec.Command(exePath, childArgs...) //nolint:gosec // args from user CLI, exe from os.Executable
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("freedius: failed to start daemon: %w", err)
	}

	if err := writePIDFile(cmd.Process.Pid); err != nil {
		// Try to kill the child if we can't write the PID file.
		_ = cmd.Process.Kill()
		return fmt.Errorf("freedius: failed to write PID file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "freedius: daemon started (PID %d)\n", cmd.Process.Pid)
	return nil
}

func stopDaemon() error {
	lockFile, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lockFile)

	pid, _, err := readPIDFile()
	if err != nil {
		return fmt.Errorf("freedius: not running (no PID file at %s)", pidFilePath())
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			removePIDFile()
			return fmt.Errorf("freedius: not running (stale PID file)")
		}
		return fmt.Errorf("freedius: failed to send SIGTERM to PID %d: %w", pid, err)
	}

	// Poll for process exit.
	for i := 0; i < 4; i++ {
		time.Sleep(50 * time.Millisecond)
		if !isProcessAlive(pid) {
			return nil
		}
	}
	return fmt.Errorf("freedius: daemon (PID %d) did not exit within 200ms", pid)
}

func daemonStatus() (running bool, pid int, err error) {
	lockFile, err := acquireLock()
	if err != nil {
		return false, 0, err
	}
	defer releaseLock(lockFile)

	return checkDaemonRunning()
}

func checkDaemonRunning() (running bool, pid int, err error) {
	pid, _, err = readPIDFile()
	if err != nil {
		return false, 0, nil // No PID file = not running.
	}

	if isProcessAlive(pid) {
		return true, pid, nil
	}

	// Stale PID file — clean it up.
	removePIDFile()
	return false, pid, nil
}

func writePIDFile(pid int) error {
	path := pidFilePath()
	data := fmt.Sprintf("%d\t%d\n", pid, time.Now().UnixNano())
	return os.WriteFile(path, []byte(data), 0600) //nolint:gosec // path is from runtimeDir()
}

func readPIDFile() (pid int, startTime int64, err error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, 0, err
	}

	parts := strings.SplitN(strings.TrimSpace(string(data)), "\t", 2)
	if len(parts) < 1 {
		return 0, 0, fmt.Errorf("malformed PID file")
	}

	pid, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("malformed PID file: %w", err)
	}

	if len(parts) >= 2 {
		startTime, _ = strconv.ParseInt(parts[1], 10, 64)
	}

	return pid, startTime, nil
}

func removePIDFile() {
	_ = os.Remove(pidFilePath())
}

func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but we lack permission to signal it.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func acquireLock() (*os.File, error) {
	path := lockFilePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) //nolint:gosec // path is from runtimeDir()
	if err != nil {
		return nil, fmt.Errorf("freedius: cannot open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("freedius: cannot acquire lock (another instance running?)")
	}
	return f, nil
}

func releaseLock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
