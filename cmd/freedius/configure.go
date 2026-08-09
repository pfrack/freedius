package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pfrack/freedius/internal/envinject"
)

// configureStdin is the reader used for the y/N confirmation prompt. It is a
// variable so tests can drive the prompt without a terminal.
var configureStdin io.Reader = os.Stdin

// runConfigure implements the `freedius configure` subcommand: it backs up
// Claude Code's settings.json and overwrites it with freedius's env block, or
// restores the newest backup with --restore.
func runConfigure(args []string) int {
	fs := flag.NewFlagSet("freedius configure", flag.ContinueOnError)
	flagConfigDir := fs.String(
		"config-dir",
		"",
		"Claude Code config directory (default $HOME/.claude)",
	)
	flagRestore := fs.Bool("restore", false, "restore the newest settings.json backup")
	flagDryRun := fs.Bool("dry-run", false, "print what would be written and exit")
	flagYes := fs.Bool("yes", false, "skip the confirmation prompt")
	flagYesShorthand := fs.Bool("y", false, "shorthand for --yes")
	fs.Usage = func() { printConfigureUsage(os.Stderr) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	configDir, err := configureDir(*flagConfigDir)
	if err != nil {
		return failf("freedius configure: %v", err)
	}
	settingsPath := filepath.Join(configDir, "settings.json")

	// Skip the backup when the file is already the freedius env block, so a
	// second `configure` run never backs up the freedius block itself — keeping
	// the user's original as the newest (and thus --restore's) backup.
	already, err := envinject.IsFreediusSettings(configDir, defaultHost, defaultPort)
	if err != nil {
		return failf("freedius configure: %v", err)
	}

	if *flagRestore {
		restored, err := envinject.RestoreSettingsJSON(configDir)
		if err != nil {
			return failf("freedius configure: %v", err)
		}
		fmt.Printf("restored %s -> %s\n", restored, settingsPath)
		return 0
	}

	if *flagDryRun {
		if already {
			fmt.Printf("%s is already configured for freedius — no backup needed\n", settingsPath)
		} else {
			fmt.Printf("would back up %s to %s.bak.<timestamp>\n", settingsPath, settingsPath)
		}
		fmt.Printf("would write %s:\n", settingsPath)
		if err := envinject.WriteSettingsJSON(configDir, defaultHost, defaultPort, true); err != nil {
			return failf("freedius configure: %v", err)
		}
		return 0
	}

	// backup is referenced by the undo hint below; it stays empty when the file
	// is already configured, so the hint is suppressed (an earlier run already
	// made the backup).
	var backup string
	if already {
		fmt.Printf("%s is already configured for freedius — nothing to back up\n", settingsPath)
	} else {
		b, err := envinject.BackupSettingsJSON(configDir)
		if err != nil {
			return failf("freedius configure: %v", err)
		}
		backup = b
		if backup == "" {
			fmt.Printf("no existing %s — nothing to back up\n", settingsPath)
		} else {
			fmt.Printf("backed up %s -> %s\n", settingsPath, backup)
		}
	}

	fmt.Printf("about to overwrite %s with:\n", settingsPath)
	if err := envinject.WriteSettingsJSON(configDir, defaultHost, defaultPort, true); err != nil {
		return failf("freedius configure: %v", err)
	}

	if !*flagYes && !*flagYesShorthand && !confirm(configureStdin, os.Stdout, settingsPath) {
		fmt.Println("aborted — settings.json left unchanged")
		return 0
	}

	if err := envinject.WriteSettingsJSON(configDir, defaultHost, defaultPort, false); err != nil {
		return failf("freedius configure: %v", err)
	}
	fmt.Printf("wrote %s\n", settingsPath)
	if backup != "" {
		fmt.Println("undo with: freedius configure --restore")
	}
	return 0
}

// configureDir resolves the Claude Code config directory, defaulting to
// $HOME/.claude when the flag is empty.
func configureDir(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// confirm asks for y/N on r. Anything other than y/yes (case-insensitive),
// including EOF, declines.
func confirm(r io.Reader, w io.Writer, path string) bool {
	_, _ = fmt.Fprintf(w, "Write freedius env block to %s? [y/N] ", path)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func printConfigureUsage(w io.Writer) {
	usage := `freedius configure — point Claude Code at the local freedius proxy

Usage: freedius configure [--config-dir DIR] [--restore] [--dry-run] [--yes]

Backs up settings.json to settings.json.bak.<timestamp>, then overwrites it
with freedius's env block. --restore puts the newest backup back.

Flags:
`
	if _, err := io.WriteString(w, usage); err != nil {
		return
	}
	fs := flag.NewFlagSet("freedius configure", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.String("config-dir", "", "Claude Code config directory (default $HOME/.claude)")
	fs.Bool("restore", false, "restore the newest settings.json backup")
	fs.Bool("dry-run", false, "print what would be written and exit")
	fs.Bool("yes", false, "skip the confirmation prompt")
	fs.Bool("y", false, "shorthand for --yes")
	fs.PrintDefaults()
}
